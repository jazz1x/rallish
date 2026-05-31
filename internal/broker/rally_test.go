package broker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jazz1x/rallish/internal/budget"
	"github.com/jazz1x/rallish/internal/session"
	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/stretchr/testify/require"
)

// resetRallies clears the global rally store between tests.
func resetRallies() {
	rallies.mu.Lock()
	rallies.sessions = make(map[contract.SessionID]*rallySession)
	rallies.mu.Unlock()
}

// newRallyServer builds a broker Server backed by a temp session store.
func newRallyServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		ts.Close()
		resetRallies()
	})
	return ts, srv
}

// postJSON is a convenience wrapper that sends a JSON POST and returns the response.
func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body)) //nolint:gosec // httptest URL
	require.NoError(t, err)
	return resp
}

// createSession creates a rally session and returns the parsed RallySession.
func createSession(t *testing.T, ts *httptest.Server, participants []string) contract.RallySession {
	t.Helper()
	req := contract.NewRallyRequest{Participants: participants}
	body, err := json.Marshal(req)
	require.NoError(t, err)
	resp := postJSON(t, ts.URL+"/rally/sessions", string(body))
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var sess contract.RallySession
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sess))
	return sess
}

// ---- Tests ----

func TestRallyCreate(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})

	require.NotEmpty(t, sess.ID)
	require.True(t, strings.HasPrefix(sess.ID, "rly_"), "id must start with rly_")
	require.Equal(t, []string{"alice", "bob"}, sess.Participants)
	require.Equal(t, contract.RallyStateIdle, sess.Status)
	require.Equal(t, "", sess.Holder)
	require.Equal(t, 0, sess.TurnN)
}

func TestRallyCreateAndStatus(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})

	// GET status immediately after create → idle.
	resp, err := http.Get(ts.URL + "/rally/sessions/" + sess.ID)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got contract.RallySession
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, sess.ID, got.ID)
	require.Equal(t, contract.RallyStateIdle, got.Status)
	require.Empty(t, got.History)
}

func TestRallyContentType(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	// POST without application/json → 415.
	resp, err := http.Post(ts.URL+"/rally/sessions", "text/plain", strings.NewReader(`{"participants":["a","b"]}`))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode)
}

func TestRallyParticipantNameValidation(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	cases := []struct {
		name         string
		participants []string
		wantStatus   int
	}{
		{"bad space", []string{"bad name", "bob"}, http.StatusBadRequest},
		{"too long", []string{"thisnameiswaytolong1", "bob"}, http.StatusBadRequest},
		{"special chars", []string{"bad!name", "bob"}, http.StatusBadRequest},
		{"empty name", []string{"", "bob"}, http.StatusBadRequest},
		{"valid underscore", []string{"alice_1", "bob-2"}, http.StatusCreated},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := contract.NewRallyRequest{Participants: tc.participants}
			body, err := json.Marshal(req)
			require.NoError(t, err)
			resp := postJSON(t, ts.URL+"/rally/sessions", string(body))
			defer func() { _ = resp.Body.Close() }()
			require.Equal(t, tc.wantStatus, resp.StatusCode)
		})
	}
}

func TestRallyDoneByNonHolder(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})

	// Join as alice so she gets the baton.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	aliceJoined := make(chan struct{})
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			ts.URL+"/rally/sessions/"+sess.ID+"/baton?as=alice", nil)
		if err != nil {
			return
		}
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		defer func() { _ = resp.Body.Close() }()
		close(aliceJoined)
		// Block until context cancelled.
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
		}
	}()

	// Give alice time to join and receive baton.
	select {
	case <-aliceJoined:
	case <-time.After(2 * time.Second):
		t.Fatal("alice did not join in time")
	}
	time.Sleep(50 * time.Millisecond)

	// Bob tries to call done — should 409 because alice holds the baton.
	doneBody := `{"note":"bob done"}`
	resp := postJSON(t, ts.URL+"/rally/sessions/"+sess.ID+"/done?as=bob", doneBody)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestRallyBatonRoundTrip(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})
	sessionID := sess.ID

	type batonResult struct {
		events []contract.BatonEvent
		err    error
	}

	// Helper: join as a participant, collect up to `n` baton events, then return.
	joinAndCollect := func(ctx context.Context, name string, n int) chan batonResult {
		ch := make(chan batonResult, 1)
		go func() {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet,
				ts.URL+"/rally/sessions/"+sessionID+"/baton?as="+name, nil)
			if err != nil {
				ch <- batonResult{err: err}
				return
			}
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				ch <- batonResult{err: err}
				return
			}
			defer func() { _ = resp.Body.Close() }()

			var events []contract.BatonEvent
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "data: ") {
					data := strings.TrimPrefix(line, "data: ")
					var evt contract.BatonEvent
					if jsonErr := json.Unmarshal([]byte(data), &evt); jsonErr == nil {
						events = append(events, evt)
						if len(events) >= n {
							ch <- batonResult{events: events}
							return
						}
					}
				}
			}
			ch <- batonResult{events: events, err: scanner.Err()}
		}()
		return ch
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Both participants join and wait for their baton events.
	// alice expects 2 events (turn 1 and turn 3), bob expects 2 events (turn 2 and... wait).
	// For this test we do 2 full rounds (alice→bob→alice→bob) and check 4 handoffs.
	aliceCh := joinAndCollect(ctx, "alice", 1) // alice gets turn 1 immediately
	time.Sleep(80 * time.Millisecond)          // let alice join and get baton

	bobCh := joinAndCollect(ctx, "bob", 1) // bob waits for alice to pass

	// Verify alice received turn 1.
	select {
	case res := <-aliceCh:
		require.NoError(t, res.err)
		require.Len(t, res.events, 1)
		require.Equal(t, 1, res.events[0].TurnN)
	case <-time.After(3 * time.Second):
		t.Fatal("alice did not receive turn 1")
	}

	// Alice calls done → bob should get baton.
	doneResp := postJSON(t, ts.URL+"/rally/sessions/"+sessionID+"/done?as=alice",
		`{"note":"alice done"}`)
	defer func() { _ = doneResp.Body.Close() }()
	require.Equal(t, http.StatusOK, doneResp.StatusCode)

	// Bob should receive turn 2.
	select {
	case res := <-bobCh:
		require.NoError(t, res.err)
		require.Len(t, res.events, 1)
		require.Equal(t, 2, res.events[0].TurnN)
		require.Equal(t, "alice", res.events[0].From)
		require.Equal(t, "alice done", res.events[0].Note)
	case <-time.After(3 * time.Second):
		t.Fatal("bob did not receive turn 2")
	}

	// Verify status shows bob_turn and 1 history entry.
	statusResp, err := http.Get(ts.URL + "/rally/sessions/" + sessionID)
	require.NoError(t, err)
	defer func() { _ = statusResp.Body.Close() }()
	require.Equal(t, http.StatusOK, statusResp.StatusCode)
	var status contract.RallySession
	require.NoError(t, json.NewDecoder(statusResp.Body).Decode(&status))
	require.Equal(t, contract.RallyTurnState("bob"), status.Status)
	require.Equal(t, "bob", status.Holder)
	require.Len(t, status.History, 1)
	require.Equal(t, "alice", status.History[0].From)
	require.Equal(t, "bob", status.History[0].To)
}

func TestRallyBatonTwoRound(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})
	sessionID := sess.ID

	type turnRecord struct {
		name  string
		turnN int
		from  string
	}

	// We'll collect 4 baton events total across both participants.
	var (
		mu      sync.Mutex
		records []turnRecord
		allDone = make(chan struct{})
	)

	addRecord := func(name string, evt contract.BatonEvent) {
		mu.Lock()
		records = append(records, turnRecord{name: name, turnN: evt.TurnN, from: evt.From})
		n := len(records)
		mu.Unlock()
		if n >= 4 {
			select {
			case allDone <- struct{}{}:
			default:
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// runParticipant connects to the SSE stream and calls done after receiving each baton.
	runParticipant := func(name string) {
		go func() {
			for {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet,
					ts.URL+"/rally/sessions/"+sessionID+"/baton?as="+name, nil)
				if err != nil {
					return
				}
				client := &http.Client{}
				resp, err := client.Do(req)
				if err != nil {
					return
				}

				scanner := bufio.NewScanner(resp.Body)
				for scanner.Scan() {
					line := scanner.Text()
					if strings.HasPrefix(line, "data: ") {
						data := strings.TrimPrefix(line, "data: ")
						var evt contract.BatonEvent
						if jsonErr := json.Unmarshal([]byte(data), &evt); jsonErr == nil {
							addRecord(name, evt)
							// Call done immediately.
							doneBody := `{"note":"` + name + ` done turn ` + fmt.Sprint(evt.TurnN) + `"}`
							doneResp, doneErr := http.Post(
								ts.URL+"/rally/sessions/"+sessionID+"/done?as="+name,
								"application/json",
								strings.NewReader(doneBody),
							)
							if doneErr == nil {
								_ = doneResp.Body.Close()
							}
						}
					}
				}
				_ = resp.Body.Close()

				select {
				case <-ctx.Done():
					return
				default:
				}
			}
		}()
	}

	runParticipant("alice")
	time.Sleep(50 * time.Millisecond)
	runParticipant("bob")

	select {
	case <-allDone:
	case <-time.After(12 * time.Second):
		mu.Lock()
		t.Fatalf("did not collect 4 baton events in time; got %d", len(records))
		mu.Unlock()
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, records, 4)

	// Expected sequence: alice(1), bob(2), alice(3), bob(4).
	expected := []turnRecord{
		{name: "alice", turnN: 1},
		{name: "bob", turnN: 2, from: "alice"},
		{name: "alice", turnN: 3, from: "bob"},
		{name: "bob", turnN: 4, from: "alice"},
	}
	for i, exp := range expected {
		require.Equal(t, exp.name, records[i].name, "record %d name", i)
		require.Equal(t, exp.turnN, records[i].turnN, "record %d turnN", i)
		if exp.from != "" {
			require.Equal(t, exp.from, records[i].from, "record %d from", i)
		}
	}

	// Status should have 4 history entries (history records each handoff, not initial grant).
	// alice→bob, bob→alice, alice→bob, bob→alice = 4 done calls = 4 handoffs but
	// the 4th done happens after we collect 4 events (so possibly 3 or 4 history entries).
	// Accept 3 or 4 depending on timing.
	statusResp, err := http.Get(ts.URL + "/rally/sessions/" + sessionID)
	require.NoError(t, err)
	defer func() { _ = statusResp.Body.Close() }()
	var status contract.RallySession
	require.NoError(t, json.NewDecoder(statusResp.Body).Decode(&status))
	require.GreaterOrEqual(t, len(status.History), 3)
}

func TestRallyDoneNonHolder409(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})
	sessionID := sess.ID

	// Join as alice to give her the baton.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ready := make(chan struct{})
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			ts.URL+"/rally/sessions/"+sessionID+"/baton?as=alice", nil)
		if err != nil {
			return
		}
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		defer func() { _ = resp.Body.Close() }()
		// Signal after first data line read.
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				close(ready)
				// Block.
				break
			}
		}
		for scanner.Scan() {
		}
	}()

	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("alice baton not received")
	}

	// Bob calls done while alice holds → 409.
	resp := postJSON(t, ts.URL+"/rally/sessions/"+sessionID+"/done?as=bob", `{}`)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestRallyStaleParticipant(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})
	sid, err := contract.NewSessionID(sess.ID)
	require.NoError(t, err)
	alice, err := contract.NewParticipantName("alice")
	require.NoError(t, err)

	// Set stale threshold to 100 ms for this test.
	setRallyStaleThreshold(sid, 100)

	// Record alice's last_seen manually.
	rallies.mu.Lock()
	s := rallies.sessions[sid]
	s.lastSeen[alice] = time.Now().UnixMilli()
	rallies.mu.Unlock()

	// Not stale yet.
	require.False(t, isParticipantStale(sid, alice, time.Now().UnixMilli()))

	// Wait past threshold.
	time.Sleep(150 * time.Millisecond)

	// Now stale.
	require.True(t, isParticipantStale(sid, alice, time.Now().UnixMilli()))
}

func TestRallySessionNotFound(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	// rly_0_0000 is a validly-formatted session id that won't exist in the store.
	resp, err := http.Get(ts.URL + "/rally/sessions/rly_0_0000")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestRallyDoneContentType(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})

	resp, err := http.Post(ts.URL+"/rally/sessions/"+sess.ID+"/done?as=alice",
		"text/plain", strings.NewReader(`{}`))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode)
}

func TestRallyBatonInvalidName(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})

	resp, err := http.Get(ts.URL + "/rally/sessions/" + sess.ID + "/baton?as=bad name!")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestRallyCreateMinParticipants(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	// Only 1 participant → 400.
	req := contract.NewRallyRequest{Participants: []string{"alice"}}
	body, err := json.Marshal(req)
	require.NoError(t, err)
	resp := postJSON(t, ts.URL+"/rally/sessions", string(body))
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestRallyBatonNonMember(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})

	resp, err := http.Get(ts.URL + "/rally/sessions/" + sess.ID + "/baton?as=charlie")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestRallyCreateWithFirstHolder verifies that a session created with
// first_holder="server" immediately has status=server_turn, holder="server",
// and turnN=1.
func TestRallyCreateWithFirstHolder(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	req := contract.NewRallyRequest{
		Participants: []string{"server", "returner"},
		FirstHolder:  "server",
	}
	body, err := json.Marshal(req)
	require.NoError(t, err)
	resp := postJSON(t, ts.URL+"/rally/sessions", string(body))
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var sess contract.RallySession
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sess))
	require.Equal(t, contract.RallyTurnState("server"), sess.Status)
	require.Equal(t, "server", sess.Holder)
	require.Equal(t, 1, sess.TurnN)
}

// TestRallyCreateInvalidFirstHolder verifies that supplying a first_holder
// that is not in participants returns 400 with a message mentioning first_holder.
func TestRallyCreateInvalidFirstHolder(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	req := contract.NewRallyRequest{
		Participants: []string{"server", "returner"},
		FirstHolder:  "ghost",
	}
	body, err := json.Marshal(req)
	require.NoError(t, err)
	resp := postJSON(t, ts.URL+"/rally/sessions", string(body))
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	raw, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(raw), "first_holder")
}

// TestRallyCreateWithoutFirstHolder verifies backwards compatibility: omitting
// first_holder still produces an idle session with turnN=0 and no holder.
func TestRallyCreateWithoutFirstHolder(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"server", "returner"})

	require.Equal(t, contract.RallyStateIdle, sess.Status)
	require.Equal(t, "", sess.Holder)
	require.Equal(t, 0, sess.TurnN)
}

// TestCloseAllRallies verifies that an active SSE baton stream receives a
// terminal data: {"closed":true} event and the connection closes within 1 s
// after CloseAllRallies is called.
func TestCloseAllRallies(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})
	sessionID := sess.ID

	// Channel to receive lines from alice's SSE stream.
	type sseResult struct {
		lines []string
		err   error
	}
	done := make(chan sseResult, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Join as alice — she gets the baton immediately (first participant).
	joined := make(chan struct{})
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			ts.URL+"/rally/sessions/"+sessionID+"/baton?as=alice", nil)
		if err != nil {
			done <- sseResult{err: err}
			return
		}
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			done <- sseResult{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()

		var lines []string
		scanner := bufio.NewScanner(resp.Body)
		firstData := false
		for scanner.Scan() {
			line := scanner.Text()
			lines = append(lines, line)
			if !firstData && strings.HasPrefix(line, "data: ") {
				firstData = true
				select {
				case joined <- struct{}{}:
				default:
				}
			}
		}
		done <- sseResult{lines: lines, err: scanner.Err()}
	}()

	// Wait for alice to receive her first data event (baton grant).
	select {
	case <-joined:
	case <-time.After(3 * time.Second):
		t.Fatal("alice did not receive initial baton event in time")
	}

	// Now call CloseAllRallies — alice's stream should receive closed:true.
	CloseAllRallies()

	// Stream should close within 1 second.
	select {
	case res := <-done:
		require.NoError(t, res.err)
		// Find the closed:true data line.
		found := false
		for _, line := range res.lines {
			if strings.Contains(line, `"closed":true`) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected closed:true line in SSE output; got lines: %v", res.lines)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("SSE stream did not close within 1 s after CloseAllRallies")
	}

	// Verify session status is interrupted.
	sid2, err2 := contract.NewSessionID(sessionID)
	require.NoError(t, err2)
	rallies.mu.Lock()
	status := rallies.sessions[sid2].status
	rallies.mu.Unlock()
	require.Equal(t, contract.RallyStateInterrupted, status)
}

// TestRallySelfHandoffRejected verifies that a participant cannot hand the baton
// to themselves via handoff_to — the broker must return 400 (ErrSelfHandoff).
func TestRallySelfHandoffRejected(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})
	sessionID := sess.ID

	// Join as alice so she receives the baton (first participant).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ready := make(chan struct{})
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			ts.URL+"/rally/sessions/"+sessionID+"/baton?as=alice", nil)
		if err != nil {
			return
		}
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			return
		}
		defer func() { _ = resp.Body.Close() }()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "data: ") {
				select {
				case ready <- struct{}{}:
				default:
				}
				break
			}
		}
		for scanner.Scan() {
		}
	}()

	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("alice did not receive baton in time")
	}

	// Alice tries to pass the baton to herself — must be 400.
	doneBody := `{"handoff_to":"alice"}`
	resp := postJSON(t, ts.URL+"/rally/sessions/"+sessionID+"/done?as=alice", doneBody)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	raw, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(raw), "cannot hand off the baton to yourself")
}

// TestRallyExplicitHandoffToDifferentParticipant verifies that a valid explicit
// handoff_to a DIFFERENT participant succeeds (false-positive guard for F4).
func TestRallyExplicitHandoffToDifferentParticipant(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	// 3-participant session: alice, bob, charlie.
	sess := createSession(t, ts, []string{"alice", "bob", "charlie"})
	sessionID := sess.ID

	// Join as alice so she receives the baton.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ready := make(chan struct{})
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			ts.URL+"/rally/sessions/"+sessionID+"/baton?as=alice", nil)
		if err != nil {
			return
		}
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			return
		}
		defer func() { _ = resp.Body.Close() }()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "data: ") {
				select {
				case ready <- struct{}{}:
				default:
				}
				break
			}
		}
		for scanner.Scan() {
		}
	}()

	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("alice did not receive baton in time")
	}

	// Alice explicitly passes to charlie (skipping bob) — must succeed with 200.
	doneBody := `{"handoff_to":"charlie"}`
	doneResp := postJSON(t, ts.URL+"/rally/sessions/"+sessionID+"/done?as=alice", doneBody)
	defer func() { _ = doneResp.Body.Close() }()
	require.Equal(t, http.StatusOK, doneResp.StatusCode)

	var updated contract.RallySession
	require.NoError(t, json.NewDecoder(doneResp.Body).Decode(&updated))
	require.Equal(t, "charlie", updated.Holder)
	require.Equal(t, contract.RallyTurnState("charlie"), updated.Status)
}
