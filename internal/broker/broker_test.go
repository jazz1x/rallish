package broker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jazz1x/hocketty/internal/budget"
	"github.com/jazz1x/hocketty/internal/session"
	"github.com/jazz1x/hocketty/pkg/contract"
	"github.com/stretchr/testify/require"
)

func parseSSEReq(resp *http.Response) (contract.TurnRequest, error) {
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := line[len("data: "):]
			var req contract.TurnRequest
			if err := json.Unmarshal([]byte(data), &req); err != nil {
				return contract.TurnRequest{}, err
			}
			return req, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return contract.TurnRequest{}, err
	}
	return contract.TurnRequest{}, errors.New("no data event in sse")
}

type fakeClock struct {
	t time.Time
}

func (f *fakeClock) Now() time.Time { return f.t }

func TestBroker_CreateAndGetSession(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}

	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)

	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Create session
	body := `{"preset": {"name":"test","routing":"round_robin","roles":[{"id":"a","runtime":"x"}],"budget":{"tokens_left":100,"turns_left":10}}, "task": {"title":"t","body":"b","repo_root":"/tmp"}}`
	resp, err := http.Post(ts.URL+"/sessions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var sess session.Session
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sess))
	_ = resp.Body.Close()
	require.NotEmpty(t, sess.ID)

	// Get session
	resp, err = http.Get(ts.URL + "/sessions/" + sess.ID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got session.Session
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	_ = resp.Body.Close()
	require.Equal(t, sess.ID, got.ID)
}

func TestBroker_NextTurnSSE(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}

	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)

	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Create session
	body := `{"preset": {"name":"test","routing":"round_robin","roles":[{"id":"planner","runtime":"claude"}],"budget":{"tokens_left":100,"turns_left":10}}, "task": {"title":"t","body":"b","repo_root":"/tmp"}}`
	resp, err := http.Post(ts.URL+"/sessions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sess session.Session
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sess))
	_ = resp.Body.Close()

	// GET /next
	req, err := http.NewRequestWithContext(context.Background(), "GET", ts.URL+"/sessions/"+sess.ID+"/next", nil)
	require.NoError(t, err)

	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	bodyBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()

	bodyStr := string(bodyBytes)
	require.Contains(t, bodyStr, "data:")
	require.Contains(t, bodyStr, `"turn":1`)
}

func TestBroker_PostTurn(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}

	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)

	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Create session
	body := `{"preset": {"name":"test","routing":"round_robin","roles":[{"id":"planner","runtime":"claude"}],"budget":{"tokens_left":100,"turns_left":10}}, "task": {"title":"t","body":"b","repo_root":"/tmp"}}`
	resp, err := http.Post(ts.URL+"/sessions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sess session.Session
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sess))
	_ = resp.Body.Close()

	// First get next turn to establish pending request
	_, err = http.Get(ts.URL + "/sessions/" + sess.ID + "/next")
	require.NoError(t, err)

	// Post turn
	turnBody := `{"done":false,"summary":"did work"}`
	resp, err = http.Post(ts.URL+"/sessions/"+sess.ID+"/turn", "application/json", strings.NewReader(turnBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got session.Session
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	_ = resp.Body.Close()
	require.Equal(t, 1, got.TurnCount)
}

func TestBroker_TerminalSession_410Gone(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}

	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)

	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Create session with only 1 turn budget
	body := `{"preset": {"name":"test","routing":"round_robin","roles":[{"id":"planner","runtime":"claude"}],"budget":{"tokens_left":100,"turns_left":1},"exit_when":["turns_exhausted"]}, "task": {"title":"t","body":"b","repo_root":"/tmp"}}`
	resp, err := http.Post(ts.URL+"/sessions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sess session.Session
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sess))
	_ = resp.Body.Close()

	// First get next turn to establish pending request
	_, err = http.Get(ts.URL + "/sessions/" + sess.ID + "/next")
	require.NoError(t, err)

	// Post turn — this exhausts the turn budget
	turnBody := `{"done":false,"summary":"did work"}`
	resp, err = http.Post(ts.URL+"/sessions/"+sess.ID+"/turn", "application/json", strings.NewReader(turnBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	// Next /next should return 410 Gone
	resp, err = http.Get(ts.URL + "/sessions/" + sess.ID + "/next")
	require.NoError(t, err)
	require.Equal(t, http.StatusGone, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestNext_AsRoleGatesDispatch_HappyPath(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}

	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)

	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Create session with 2 roles
	body := `{"preset": {"name":"test","routing":"round_robin","roles":[{"id":"alpha","runtime":"claude"},{"id":"beta","runtime":"claude"}],"budget":{"tokens_left":100,"turns_left":10}}, "task": {"title":"t","body":"b","repo_root":"/tmp"}}`
	resp, err := http.Post(ts.URL+"/sessions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sess session.Session
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sess))
	_ = resp.Body.Close()

	type result struct {
		req contract.TurnRequest
		ok  bool
	}
	alphaRecv := make(chan result, 1)
	betaRecv := make(chan result, 1)

	// Start both pollers concurrently.
	go func() {
		resp, err := http.Get(ts.URL + "/sessions/" + sess.ID + "/next?as=alpha")
		if err != nil {
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return
		}
		req, err := parseSSEReq(resp)
		if err != nil {
			return
		}
		alphaRecv <- result{req: req, ok: true}
	}()

	go func() {
		resp, err := http.Get(ts.URL + "/sessions/" + sess.ID + "/next?as=beta")
		if err != nil {
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return
		}
		req, err := parseSSEReq(resp)
		if err != nil {
			return
		}
		betaRecv <- result{req: req, ok: true}
	}()

	// Alpha should dispatch immediately (turn 1 is alpha's).
	select {
	case r := <-alphaRecv:
		require.True(t, r.ok)
		require.Equal(t, "alpha", r.req.Role)
		require.Equal(t, 1, r.req.Turn)
	case <-time.After(2 * time.Second):
		t.Fatal("alpha did not receive turn 1")
	}

	// Beta should still be blocked.
	select {
	case <-betaRecv:
		t.Fatal("beta should not have received a turn yet")
	case <-time.After(200 * time.Millisecond):
		// expected — beta is long-polling
	}

	// Post turn for alpha.
	turnBody := `{"done":false,"summary":"alpha did work"}`
	resp, err = http.Post(ts.URL+"/sessions/"+sess.ID+"/turn", "application/json", strings.NewReader(turnBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	// Beta should now wake up and receive turn 2.
	select {
	case r := <-betaRecv:
		require.True(t, r.ok)
		require.Equal(t, "beta", r.req.Role)
		require.Equal(t, 2, r.req.Turn)
	case <-time.After(2 * time.Second):
		t.Fatal("beta did not receive turn 2 after alpha posted")
	}
}

func TestNext_AsRoleTimeoutReturns204(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}

	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)

	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)
	srv.pollTimeout = 100 * time.Millisecond

	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := `{"preset": {"name":"test","routing":"round_robin","roles":[{"id":"alpha","runtime":"claude"}],"budget":{"tokens_left":100,"turns_left":10}}, "task": {"title":"t","body":"b","repo_root":"/tmp"}}`
	resp, err := http.Post(ts.URL+"/sessions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sess session.Session
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sess))
	_ = resp.Body.Close()

	// Poll with a non-matching role — should 204 after timeout.
	resp, err = http.Get(ts.URL + "/sessions/" + sess.ID + "/next?as=nonexistent")
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestNext_AsRoleTerminalDuringWait(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}

	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)

	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// 2-role session with 1 turn budget — turns_exhausted after first post.
	body := `{"preset": {"name":"test","routing":"round_robin","roles":[{"id":"alpha","runtime":"claude"},{"id":"beta","runtime":"claude"}],"budget":{"tokens_left":100,"turns_left":1},"exit_when":["turns_exhausted"]}, "task": {"title":"t","body":"b","repo_root":"/tmp"}}`
	resp, err := http.Post(ts.URL+"/sessions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sess session.Session
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sess))
	_ = resp.Body.Close()

	// Alpha gets turn 1.
	resp, err = http.Get(ts.URL + "/sessions/" + sess.ID + "/next?as=alpha")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	// Beta starts long-polling before the turn is posted.
	betaDone := make(chan int, 1)
	go func() {
		resp, err := http.Get(ts.URL + "/sessions/" + sess.ID + "/next?as=beta")
		if err != nil {
			betaDone <- 0
			return
		}
		defer func() { _ = resp.Body.Close() }()
		betaDone <- resp.StatusCode
	}()

	// Give beta time to enter the long-poll.
	time.Sleep(50 * time.Millisecond)

	// Post alpha's turn — exhausts budget, marks terminal.
	turnBody := `{"done":false,"summary":"alpha did work"}`
	resp, err = http.Post(ts.URL+"/sessions/"+sess.ID+"/turn", "application/json", strings.NewReader(turnBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	// Beta should wake up and get 410.
	select {
	case status := <-betaDone:
		require.Equal(t, http.StatusGone, status)
	case <-time.After(2 * time.Second):
		t.Fatal("beta did not receive 410 after session became terminal")
	}
}
