package broker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jazz1x/hocketty/internal/budget"
	"github.com/jazz1x/hocketty/internal/session"
	"github.com/stretchr/testify/require"
)

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
