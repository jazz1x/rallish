package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jazz1x/rallish/internal/broker"
	"github.com/jazz1x/rallish/internal/budget"
	"github.com/jazz1x/rallish/internal/session"
	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/stretchr/testify/require"
)

func setupMCPAgentTest(t *testing.T) (string, *httptest.Server) {
	t.Helper()
	homeDir := t.TempDir()
	sessionsDir := filepath.Join(homeDir, ".rallish", "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o700))
	clock := &realClock{}
	store, err := session.NewStore(sessionsDir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := broker.NewServer(store, budgeter)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	rallishDir := filepath.Join(homeDir, ".rallish")
	require.NoError(t, os.MkdirAll(rallishDir, 0o700))
	port := strings.TrimPrefix(ts.URL, "http://127.0.0.1:")
	require.NoError(t, os.WriteFile(filepath.Join(rallishDir, "port"), []byte(port), 0o600))

	return homeDir, ts
}

func runMCPAgent(t *testing.T, homeDir string, opts mcpAgentOptions) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	err := runRallyMCPAgent(context.Background(), homeDir, opts, &buf)
	return strings.TrimSpace(buf.String()), err
}

func TestMCPAgentCreate(t *testing.T) {
	homeDir, _ := setupMCPAgentTest(t)
	out, err := runMCPAgent(t, homeDir, mcpAgentOptions{
		Mode:         "create",
		Participants: "alice,bob",
		Task:         "refactor auth",
	})
	require.NoError(t, err)

	var sess contract.RallySession
	require.NoError(t, json.Unmarshal([]byte(out), &sess))
	require.NotEmpty(t, sess.ID)
	require.Equal(t, []string{"alice", "bob"}, sess.Participants)
}

func TestMCPAgentCreateTooFewParticipants(t *testing.T) {
	homeDir, _ := setupMCPAgentTest(t)
	_, err := runMCPAgent(t, homeDir, mcpAgentOptions{
		Mode:         "create",
		Participants: "alice",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, contract.ErrTooFewParticipants)
}

func TestMCPAgentJoinAndDone(t *testing.T) {
	homeDir, ts := setupMCPAgentTest(t)

	out, err := runMCPAgent(t, homeDir, mcpAgentOptions{
		Mode:         "create",
		Participants: "alice,bob",
		FirstHolder:  "alice",
	})
	require.NoError(t, err)
	var sess contract.RallySession
	require.NoError(t, json.Unmarshal([]byte(out), &sess))

	var wg sync.WaitGroup
	wg.Add(1)
	var joinOut string
	var joinErr error
	go func() {
		defer wg.Done()
		joinOut, joinErr = runMCPAgent(t, homeDir, mcpAgentOptions{
			Mode:      "join",
			SessionID: sess.ID,
			As:        "bob",
			Timeout:   5 * time.Second,
		})
	}()

	require.Eventually(t, func() bool {
		resp, err := http.Get(ts.URL + "/rally/sessions/" + sess.ID) //nolint:gosec // httptest URL
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return false
		}
		var status contract.RallySession
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			return false
		}
		_, ok := status.LastSeen["bob"]
		return ok
	}, 5*time.Second, 10*time.Millisecond, "bob did not register in time")

	doneOut, err := runMCPAgent(t, homeDir, mcpAgentOptions{
		Mode:      "done",
		SessionID: sess.ID,
		As:        "alice",
		Note:      "from mcp-agent",
	})
	require.NoError(t, err)
	var doneSess contract.RallySession
	require.NoError(t, json.Unmarshal([]byte(doneOut), &doneSess))
	require.Equal(t, "bob", doneSess.Holder)

	wg.Wait()
	require.NoError(t, joinErr)

	var evt contract.BatonEvent
	require.NoError(t, json.Unmarshal([]byte(joinOut), &evt))
	require.Equal(t, 2, evt.TurnN)
	require.Equal(t, "alice", evt.From)
	require.Equal(t, "from mcp-agent", evt.Note)
}

func TestMCPAgentStatus(t *testing.T) {
	homeDir, _ := setupMCPAgentTest(t)

	out, err := runMCPAgent(t, homeDir, mcpAgentOptions{
		Mode:         "create",
		Participants: "alice,bob",
	})
	require.NoError(t, err)
	var sess contract.RallySession
	require.NoError(t, json.Unmarshal([]byte(out), &sess))

	statusOut, err := runMCPAgent(t, homeDir, mcpAgentOptions{
		Mode:      "status",
		SessionID: sess.ID,
	})
	require.NoError(t, err)
	var status contract.RallySession
	require.NoError(t, json.Unmarshal([]byte(statusOut), &status))
	require.Equal(t, sess.ID, status.ID)
}

func TestMCPAgentJoinTimeout(t *testing.T) {
	homeDir, _ := setupMCPAgentTest(t)

	out, err := runMCPAgent(t, homeDir, mcpAgentOptions{
		Mode:         "create",
		Participants: "alice,bob",
		FirstHolder:  "alice",
	})
	require.NoError(t, err)
	var sess contract.RallySession
	require.NoError(t, json.Unmarshal([]byte(out), &sess))

	joinOut, err := runMCPAgent(t, homeDir, mcpAgentOptions{
		Mode:      "join",
		SessionID: sess.ID,
		As:        "bob",
		Timeout:   50 * time.Millisecond,
	})
	require.ErrorIs(t, err, ErrTimeoutWaitingForBaton)

	var timeout contract.MCPTimeoutResult
	require.NoError(t, json.Unmarshal([]byte(joinOut), &timeout))
	require.True(t, timeout.Timeout)
}

func TestMCPAgentInterrupt(t *testing.T) {
	homeDir, _ := setupMCPAgentTest(t)

	out, err := runMCPAgent(t, homeDir, mcpAgentOptions{
		Mode:         "create",
		Participants: "alice,bob",
		FirstHolder:  "alice",
	})
	require.NoError(t, err)
	var sess contract.RallySession
	require.NoError(t, json.Unmarshal([]byte(out), &sess))

	interruptOut, err := runMCPAgent(t, homeDir, mcpAgentOptions{
		Mode:      "interrupt",
		SessionID: sess.ID,
	})
	require.NoError(t, err)
	var status contract.RallySession
	require.NoError(t, json.Unmarshal([]byte(interruptOut), &status))
	require.Equal(t, sess.ID, status.ID)
	require.Equal(t, contract.RallyStateInterrupted, status.Status)
}

func TestMCPAgentUnknownMode(t *testing.T) {
	homeDir, _ := setupMCPAgentTest(t)
	_, err := runMCPAgent(t, homeDir, mcpAgentOptions{Mode: "fly"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown mode")
}
