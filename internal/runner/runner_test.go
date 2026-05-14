package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jazz1x/hocketty/internal/adapter/fake"
	"github.com/jazz1x/hocketty/internal/broker"
	"github.com/jazz1x/hocketty/internal/budget"
	"github.com/jazz1x/hocketty/internal/session"
	"github.com/jazz1x/hocketty/pkg/contract"
	"github.com/stretchr/testify/require"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }

func TestLoop_ThreeTurns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dir := t.TempDir()
	clock := &fakeClock{t: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)

	preset := contract.Preset{
		Name:    "test",
		Routing: "round_robin",
		Roles:   []contract.Role{{ID: "ralph", Runtime: "fake"}},
		Budget:  contract.Budget{TokensLeft: 100, TurnsLeft: 10},
	}
	budgeter := budget.NewBudgeter(clock)
	srv := broker.NewServer(store, budgeter)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	body, _ := json.Marshal(map[string]any{
		"preset": preset,
		"task":   contract.Task{Title: "t", Body: "b", RepoRoot: "/tmp"},
	})
	resp, err := http.Post(ts.URL+"/sessions", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	var sess session.Session
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sess))
	_ = resp.Body.Close()

	callCount := 0
	fa := fake.New(func(_ int) contract.TurnResponse {
		callCount++
		return contract.TurnResponse{
			Done:    callCount == 3,
			Summary: "turn",
		}
	})

	loop := NewLoop(fa, "ralph", ts.URL, sess.ID)
	err = loop.Run(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, callCount)

	records, err := store.Replay(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, records, 3)
}

func TestLoop_SessionGone_ReturnsNil(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Broker that returns 410 on /next after one turn is posted
	var turnPosted bool
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions/{id}/turn", func(w http.ResponseWriter, r *http.Request) {
		turnPosted = true
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id")})
	})
	mux.HandleFunc("GET /sessions/{id}/next", func(w http.ResponseWriter, r *http.Request) {
		if turnPosted {
			http.Error(w, "session terminated", http.StatusGone)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		//nolint:gosec // test-only mock server with fully controlled input
		_, _ = w.Write([]byte("data: {\"session\":\"" + r.PathValue("id") + "\",\"turn\":1,\"role\":\"ralph\"}\n\n"))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	callCount := 0
	fa := fake.New(func(_ int) contract.TurnResponse {
		callCount++
		return contract.TurnResponse{Done: false, Summary: "turn"}
	})

	loop := NewLoop(fa, "ralph", ts.URL, "test-session")
	err := loop.Run(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, callCount)
	require.True(t, turnPosted)
}

func TestLoop_204_RePolls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var nextCount int
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions/{id}/turn", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": r.PathValue("id")})
	})
	mux.HandleFunc("GET /sessions/{id}/next", func(w http.ResponseWriter, r *http.Request) {
		nextCount++
		if nextCount <= 2 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		//nolint:gosec // test-only mock server with fully controlled input
		_, _ = w.Write([]byte("data: {\"session\":\"" + r.PathValue("id") + "\",\"turn\":1,\"role\":\"ralph\"}\n\n"))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	callCount := 0
	fa := fake.New(func(_ int) contract.TurnResponse {
		callCount++
		return contract.TurnResponse{Done: true, Summary: "turn"}
	})

	loop := NewLoop(fa, "ralph", ts.URL, "test-session")
	err := loop.Run(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, callCount)
	require.Equal(t, 3, nextCount)
}
