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
