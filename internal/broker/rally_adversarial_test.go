package broker

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/stretchr/testify/require"
)

// TestRallyDoneOnInterruptedSession verifies that a done request on an
// already-interrupted session is rejected and cannot resurrect the session.
func TestRallyDoneOnInterruptedSession(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})
	sessionID := sess.ID

	// Alice joins and receives the baton.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, awaitBaton(ctx, ts.URL, sessionID, "alice"))

	// Daemon shuts down the rally.
	CloseAllRallies()

	// Alice tries to hand off — must be rejected.
	doneResp := postJSON(t, ts.URL+"/rally/sessions/"+sessionID+"/done?as=alice", `{}`)
	defer func() { _ = doneResp.Body.Close() }()
	require.Equal(t, http.StatusConflict, doneResp.StatusCode)

	body, _ := io.ReadAll(doneResp.Body)
	require.Contains(t, string(body), contract.ErrSessionInterrupted.Error())

	// Session must remain interrupted, not bob_turn.
	statusResp, err := http.Get(ts.URL + "/rally/sessions/" + sessionID)
	require.NoError(t, err)
	defer func() { _ = statusResp.Body.Close() }()
	require.Equal(t, http.StatusOK, statusResp.StatusCode)

	var status contract.RallySession
	require.NoError(t, json.NewDecoder(statusResp.Body).Decode(&status))
	require.Equal(t, contract.RallyStateInterrupted, status.Status)
	require.Empty(t, status.Holder)
	require.Empty(t, status.History)
}

// TestRallyJoinInterruptedSession verifies that joining an interrupted session
// receives the close sentinel immediately instead of hanging.
func TestRallyJoinInterruptedSession(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})
	sessionID := sess.ID

	// Interrupt before anyone joins.
	CloseAllRallies()

	// Alice tries to join.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan []string, 1)
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			ts.URL+"/rally/sessions/"+sessionID+"/baton?as=alice", nil)
		if err != nil {
			done <- nil
			return
		}
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			done <- nil
			return
		}
		defer func() { _ = resp.Body.Close() }()

		var lines []string
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		done <- lines
	}()

	select {
	case lines := <-done:
		found := false
		for _, line := range lines {
			if strings.Contains(line, `"closed":true`) {
				found = true
				break
			}
		}
		require.True(t, found, "expected closed:true in SSE output; got %v", lines)
	case <-time.After(2 * time.Second):
		t.Fatal("join on interrupted session did not return close sentinel")
	}
}

// TestRallyHandoffToNonMember verifies that handoff_to naming a non-member is
// rejected rather than silently falling back to round-robin.
func TestRallyHandoffToNonMember(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})
	sessionID := sess.ID

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, awaitBaton(ctx, ts.URL, sessionID, "alice"))

	doneResp := postJSON(t, ts.URL+"/rally/sessions/"+sessionID+"/done?as=alice",
		`{"handoff_to":"eve"}`)
	defer func() { _ = doneResp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, doneResp.StatusCode)

	body, _ := io.ReadAll(doneResp.Body)
	require.Contains(t, string(body), contract.ErrHandoffToNotMember.Error())

	// Holder must still be alice; no history entry written.
	statusResp, err := http.Get(ts.URL + "/rally/sessions/" + sessionID)
	require.NoError(t, err)
	defer func() { _ = statusResp.Body.Close() }()

	var status contract.RallySession
	require.NoError(t, json.NewDecoder(statusResp.Body).Decode(&status))
	require.Equal(t, "alice", status.Holder)
	require.Empty(t, status.History)
}

// TestRallyDuplicateJoinRejected verifies that a second concurrent baton stream
// for the same participant is rejected so the first stream is not orphaned.
func TestRallyDuplicateJoinRejected(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})
	sessionID := sess.ID

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First join: hold the stream open until context cancelled.
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
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
		}
	}()

	// Wait for the first stream to be registered.
	sid, err := contract.NewSessionID(sessionID)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		rallies.mu.Lock()
		defer rallies.mu.Unlock()
		return rallies.sessions[sid].streams["alice"] != nil
	}, 2*time.Second, 10*time.Millisecond)

	// Second join attempt must be rejected.
	resp, err := http.Get(ts.URL + "/rally/sessions/" + sessionID + "/baton?as=alice")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusConflict, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), contract.ErrAlreadyConnected.Error())

	cancel()
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first join stream did not close")
	}
}

// TestRallyConcurrentDoneCloseAllRallies stress-tests the send/close channel
// boundary. Before the hardening this interleaving could panic with
// "send on closed channel"; now it must complete without panicking.
func TestRallyConcurrentDoneCloseAllRallies(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})
	sessionID := sess.ID

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Keep bob's stream registered and alive for the duration.
	bobCtx, bobCancel := context.WithCancel(ctx)
	defer bobCancel()
	go func() {
		req, _ := http.NewRequestWithContext(bobCtx, http.MethodGet,
			ts.URL+"/rally/sessions/"+sessionID+"/baton?as=bob", nil)
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			return
		}
		defer func() { _ = resp.Body.Close() }()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
		}
	}()

	// Make alice the holder.
	require.NoError(t, awaitBaton(ctx, ts.URL, sessionID, "alice"))

	var wg sync.WaitGroup
	// Repeatedly race done against CloseAllRallies. If the fix regresses,
	// this will panic with "send on closed channel".
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			resp, _ := http.Post(
				ts.URL+"/rally/sessions/"+sessionID+"/done?as=alice",
				"application/json",
				strings.NewReader(`{}`),
			)
			if resp != nil {
				_ = resp.Body.Close()
			}
		}()
		go func() {
			defer wg.Done()
			CloseAllRallies()
		}()
	}
	wg.Wait()
	bobCancel()
}

// TestRallyConcurrentJoinCloseAllRallies stress-tests the join path against
// session interruption. It must not panic or corrupt the stream map.
func TestRallyConcurrentJoinCloseAllRallies(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})
	sessionID := sess.ID

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	// Goroutine A: repeatedly join as alice.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
				ts.URL+"/rally/sessions/"+sessionID+"/baton?as=alice", nil)
			resp, err := (&http.Client{}).Do(req)
			if err != nil {
				continue
			}
			_ = resp.Body.Close()
		}
	}()

	// Goroutine B: repeatedly interrupt the session.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			CloseAllRallies()
		}
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()
	wg.Wait()
}

// TestRallyDoneBeforeAnyJoinRejectsIdleSession verifies that calling done on an
// idle session (no holder yet) returns 409, not 200.
func TestRallyDoneBeforeAnyJoinRejectsIdleSession(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	sess := createSession(t, ts, []string{"alice", "bob"})

	doneResp := postJSON(t, ts.URL+"/rally/sessions/"+sess.ID+"/done?as=alice", `{}`)
	defer func() { _ = doneResp.Body.Close() }()
	require.Equal(t, http.StatusConflict, doneResp.StatusCode)
}
