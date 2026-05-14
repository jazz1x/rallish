package internal_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jazz1x/hocketty/internal/adapter/fake"
	"github.com/jazz1x/hocketty/internal/broker"
	"github.com/jazz1x/hocketty/internal/budget"
	"github.com/jazz1x/hocketty/internal/preset"
	"github.com/jazz1x/hocketty/internal/runner"
	"github.com/jazz1x/hocketty/internal/session"
	"github.com/jazz1x/hocketty/pkg/contract"
	"github.com/stretchr/testify/require"
)

type fakeClock struct {
	t time.Time
}

func (f *fakeClock) Now() time.Time { return f.t }

func TestIntegration_FakeAdapterPingPong(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	clock := &fakeClock{t: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}

	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)

	budgeter := budget.NewBudgeter(clock)
	srv := broker.NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Create session
	createBody := `{
		"preset": {
			"name":"pair-review",
			"routing":"round_robin",
			"roles":[
				{"id":"planner","runtime":"claude","model":"opus"},
				{"id":"executor","runtime":"kimi","model":"k2"}
			],
			"budget":{"tokens_left":10000,"turns_left":10}
		},
		"task": {"title":"implement foo","body":"add foo feature","repo_root":"/tmp"}
	}`
	resp, err := http.Post(ts.URL+"/sessions", "application/json", strings.NewReader(createBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var sess session.Session
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sess))
	_ = resp.Body.Close()
	require.NotEmpty(t, sess.ID)

	// Simulate 3 turns
	for i := 1; i <= 3; i++ {
		// GET /next
		req, err := http.NewRequestWithContext(ctx, "GET", ts.URL+"/sessions/"+sess.ID+"/next", nil)
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		bodyBytes, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		_ = resp.Body.Close()

		// Parse SSE data
		bodyStr := string(bodyBytes)
		var turnReq contract.TurnRequest
		found := false
		for _, line := range strings.Split(bodyStr, "\n") {
			if strings.HasPrefix(line, "data: ") {
				require.NoError(t, json.Unmarshal([]byte(line[len("data: "):]), &turnReq))
				found = true
				break
			}
		}
		require.True(t, found, "expected SSE data event")
		require.Equal(t, i, turnReq.Turn)

		// POST /turn
		turnResp := contract.TurnResponse{
			Done:    i == 3,
			Summary: fmt.Sprintf("turn %d", i),
			Usage: &contract.Usage{
				TokensIn:  10,
				TokensOut: 20,
				Ms:        100,
			},
		}
		turnRespJSON, err := json.Marshal(turnResp)
		require.NoError(t, err)

		resp, err = http.Post(ts.URL+"/sessions/"+sess.ID+"/turn", "application/json", bytes.NewReader(turnRespJSON))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var updated session.Session
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&updated))
		_ = resp.Body.Close()
		require.Equal(t, i, updated.TurnCount)
	}

	// Verify log.jsonl
	logPath := filepath.Join(dir, sess.ID, "log.jsonl")
	logData, err := os.ReadFile(logPath) //nolint:gosec // test file
	require.NoError(t, err)

	lines := 0
	scanner := bufio.NewScanner(bytes.NewReader(logData))
	for scanner.Scan() {
		lines++
		var rec session.TurnRecord
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &rec))
		require.Equal(t, lines, rec.Turn)
	}
	require.NoError(t, scanner.Err())
	require.Equal(t, 3, lines)
}

func TestIntegration_RunnerLoop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dir := t.TempDir()
	clock := &fakeClock{t: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)

	presetsMap, err := preset.LoadEmbedded()
	require.NoError(t, err)
	p, ok := presetsMap["solo-ralph"]
	require.True(t, ok)

	budgeter := budget.NewBudgeter(clock)
	srv := broker.NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	body, _ := json.Marshal(map[string]any{
		"preset": p,
		"task":   contract.Task{Title: "test", Body: "test body", RepoRoot: "/tmp"},
	})
	resp, err := http.Post(ts.URL+"/sessions", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	var sess session.Session
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sess))
	_ = resp.Body.Close()

	callCount := 0
	fa := fake.New(func(turn int) contract.TurnResponse {
		callCount++
		return contract.TurnResponse{
			Done:    turn == 3,
			Summary: fmt.Sprintf("turn %d", turn),
			Usage: &contract.Usage{
				TokensIn:  10,
				TokensOut: 10,
				Ms:        100,
			},
		}
	})

	loop := runner.NewLoop(fa, "ralph", ts.URL, sess.ID)
	err = loop.Run(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, callCount)

	// Verify session state
	got, err := store.Get(ctx, sess.ID)
	require.NoError(t, err)
	require.Equal(t, 3, got.TurnCount)

	// Verify log.jsonl has 3 lines
	logPath := filepath.Join(dir, sess.ID, "log.jsonl")
	logData, err := os.ReadFile(logPath) //nolint:gosec // test file
	require.NoError(t, err)

	lines := 0
	scanner := bufio.NewScanner(bytes.NewReader(logData))
	for scanner.Scan() {
		lines++
		var rec session.TurnRecord
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &rec))
		require.Equal(t, lines, rec.Turn)
	}
	require.NoError(t, scanner.Err())
	require.Equal(t, 3, lines)
}
