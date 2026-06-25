package broker

import (
	"bufio"
	"context"
	"encoding/json"
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

func TestA2A_AgentCard(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// v1.0 conformance path.
	resp, err := http.Get(ts.URL + "/.well-known/agent-card.json")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var card contract.AgentCard
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
	_ = resp.Body.Close()
	require.Equal(t, "rallish", card.Name)
	require.Equal(t, contract.ProtocolVersion, card.ProtocolVersion)
	require.True(t, card.Capabilities.Streaming)
	require.NotEmpty(t, card.Skills)

	// Legacy draft path remains served as a back-compat alias.
	legacy, err := http.Get(ts.URL + "/.well-known/agent.json")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, legacy.StatusCode)
	var legacyCard contract.AgentCard
	require.NoError(t, json.NewDecoder(legacy.Body).Decode(&legacyCard))
	_ = legacy.Body.Close()
	require.Equal(t, contract.ProtocolVersion, legacyCard.ProtocolVersion)
}

func TestA2A_TasksSend(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"parts":[{"text":"hello"}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	_ = resp.Body.Close()
	require.Nil(t, result.Error)
	require.NotNil(t, result.Result)
	m, ok := result.Result["task"].(map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, m["id"])
	require.Equal(t, m["id"], m["sessionId"], "A2ATask.sessionId must equal task id")
}

func TestA2A_TasksSendMultipleParts(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"parts":[{"text":"hello"},{"text":"world"}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	_ = resp.Body.Close()
	require.Nil(t, result.Error)

	taskID := result.Result["task"].(map[string]any)["id"].(string)
	sess, err := store.Get(context.Background(), taskID)
	require.NoError(t, err)
	require.Equal(t, "hello world", sess.Task.Body)
}

func TestA2A_TasksGet(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Create via SendMessage
	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"parts":[{"text":"hello"}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sendResult contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sendResult))
	_ = resp.Body.Close()
	task := sendResult.Result["task"].(map[string]any)
	taskID := task["id"].(string)

	// Get via GetTask
	getBody := `{"jsonrpc":"2.0","id":2,"method":"GetTask","params":{"id":"` + taskID + `"}}`
	resp, err = http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(getBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var getResult contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&getResult))
	_ = resp.Body.Close()
	require.Nil(t, getResult.Error)
	gotTask := getResult.Result["task"].(map[string]any)
	require.Equal(t, taskID, gotTask["id"])
}

func TestA2A_TasksCancel(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Create
	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"parts":[{"text":"hello"}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sendResult contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sendResult))
	_ = resp.Body.Close()
	taskID := sendResult.Result["task"].(map[string]any)["id"].(string)

	// Cancel
	cancelBody := `{"jsonrpc":"2.0","id":3,"method":"CancelTask","params":{"id":"` + taskID + `"}}`
	resp, err = http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(cancelBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var cancelResult contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cancelResult))
	_ = resp.Body.Close()
	require.Nil(t, cancelResult.Error)
	gotTask := cancelResult.Result["task"].(map[string]any)
	status := gotTask["status"].(map[string]any)
	require.Equal(t, "canceled", status["state"])
}

func TestA2A_TasksCancelAlreadyTerminated(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Create
	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"parts":[{"text":"hello"}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sendResult contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sendResult))
	_ = resp.Body.Close()
	taskID := sendResult.Result["task"].(map[string]any)["id"].(string)

	// Cancel first time
	cancelBody := `{"jsonrpc":"2.0","id":2,"method":"CancelTask","params":{"id":"` + taskID + `"}}`
	resp, err = http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(cancelBody))
	require.NoError(t, err)
	var first contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&first))
	_ = resp.Body.Close()
	require.Nil(t, first.Error)

	// Cancel second time — should error
	resp, err = http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(cancelBody))
	require.NoError(t, err)
	var second contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&second))
	_ = resp.Body.Close()
	require.NotNil(t, second.Error)
	require.Equal(t, -32602, second.Error.Code)
}

func TestA2A_UnknownMethod(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/unknown","params":{}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	_ = resp.Body.Close()
	require.NotNil(t, result.Error)
	require.Equal(t, -32601, result.Error.Code)
}

func TestA2A_EmptyBody(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(""))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	_ = resp.Body.Close()
	require.NotNil(t, result.Error)
	require.Equal(t, -32700, result.Error.Code)
}

func TestA2A_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader("not-json"))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	_ = resp.Body.Close()
	require.NotNil(t, result.Error)
	require.Equal(t, -32700, result.Error.Code)
}

func TestA2A_TasksSendMissingParams(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	_ = resp.Body.Close()
	require.NotNil(t, result.Error)
	require.Equal(t, -32602, result.Error.Code)
}

func TestA2A_TasksGetMissingID(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"GetTask","params":{}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	_ = resp.Body.Close()
	require.NotNil(t, result.Error)
	require.Equal(t, -32602, result.Error.Code)
}

func TestA2A_TasksCancelMissingID(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"CancelTask","params":{}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	_ = resp.Body.Close()
	require.NotNil(t, result.Error)
	require.Equal(t, -32602, result.Error.Code)
}

func TestA2A_TasksSendSubscribeTerminal(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Create
	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"parts":[{"text":"hello"}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sendResult contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sendResult))
	_ = resp.Body.Close()
	taskID := sendResult.Result["task"].(map[string]any)["id"].(string)

	// Cancel to make terminal
	cancelBody := `{"jsonrpc":"2.0","id":2,"method":"CancelTask","params":{"id":"` + taskID + `"}}`
	resp, err = http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(cancelBody))
	require.NoError(t, err)
	_ = resp.Body.Close()

	// Subscribe should immediately return the terminal event without leaking a channel
	subBody := `{"jsonrpc":"2.0","id":3,"method":"SubscribeToTask","params":{"id":"` + taskID + `"}}`
	resp, err = http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(subBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	scanner := bufio.NewScanner(resp.Body)
	var events []string
	var eventTypes []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventTypes = append(eventTypes, strings.TrimPrefix(line, "event: "))
		}
		if strings.HasPrefix(line, "data: ") {
			events = append(events, strings.TrimPrefix(line, "data: "))
		}
	}
	_ = resp.Body.Close()
	require.NoError(t, scanner.Err())
	require.Len(t, events, 1)
	require.Len(t, eventTypes, 1)
	require.Equal(t, "TaskStatusUpdateEvent", eventTypes[0])

	var evt contract.A2ATaskUpdateEvent
	require.NoError(t, json.Unmarshal([]byte(events[0]), &evt))
	require.Equal(t, taskID, evt.ID)
	require.True(t, evt.Final)
	require.Equal(t, contract.TaskStateCanceled, evt.Status.State)
}

func TestA2A_TasksSendSubscribeClientDisconnect(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Create
	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"parts":[{"text":"hello"}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sendResult contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sendResult))
	_ = resp.Body.Close()
	taskID := sendResult.Result["task"].(map[string]any)["id"].(string)

	// Subscribe with cancelable context
	ctx, cancel := context.WithCancel(context.Background())
	subBody := `{"jsonrpc":"2.0","id":2,"method":"SubscribeToTask","params":{"id":"` + taskID + `"}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/a2a", strings.NewReader(subBody))
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		defer func() { _ = resp.Body.Close() }()
		// Read until disconnect
		_, _ = io.Copy(io.Discard, resp.Body)
	}()

	// Give handler time to enter the select loop
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Handler returned after context cancellation
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after client disconnect")
	}
}

func TestA2A_TasksSendSubscribeRaceNoLeak(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Create
	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"parts":[{"text":"hello"}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sendResult contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sendResult))
	_ = resp.Body.Close()
	taskID := sendResult.Result["task"].(map[string]any)["id"].(string)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			subBody := `{"jsonrpc":"2.0","id":2,"method":"SubscribeToTask","params":{"id":"` + taskID + `"}}`
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/a2a", strings.NewReader(subBody))
			client := &http.Client{Timeout: 2 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer func() { _ = resp.Body.Close() }()
			_, _ = io.Copy(io.Discard, resp.Body)
		}()
	}

	// Let subscribers register
	time.Sleep(50 * time.Millisecond)

	// Cancel should notify all subscribers without races or panics
	cancelBody := `{"jsonrpc":"2.0","id":3,"method":"CancelTask","params":{"id":"` + taskID + `"}}`
	resp2, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(cancelBody))
	require.NoError(t, err)
	_ = resp2.Body.Close()

	wg.Wait()
}

// TestA2A_StrictIntake_UnknownParamFieldRejected is the strict-intake negative
// control: a v1.0 request whose params carry an unknown/extra field is an ERROR
// (-32602), not silently dropped (parse-don't-validate; RFC 9413 / Postel).
func TestA2A_StrictIntake_UnknownParamFieldRejected(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Well-formed message plus a bogus extra param field.
	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"parts":[{"text":"hello"}]},"bogus":true}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	_ = resp.Body.Close()
	require.NotNil(t, result.Error, "unknown param field must be rejected, not dropped")
	require.Equal(t, -32602, result.Error.Code)
}

// TestA2A_StrictIntake_UnknownEnvelopeFieldRejected asserts an unknown top-level
// JSON-RPC envelope field is a parse error (-32700) under DisallowUnknownFields.
func TestA2A_StrictIntake_UnknownEnvelopeFieldRejected(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"parts":[{"text":"hi"}]}},"extra":1}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	_ = resp.Body.Close()
	require.NotNil(t, result.Error, "unknown envelope field must be rejected")
	require.Equal(t, -32700, result.Error.Code)
}

// TestA2A_LegacyMethodAlias is the back-compat false-positive guard: the legacy
// lowercase tasks/* method names still dispatch (a legacy client is not broken).
func TestA2A_LegacyMethodAlias(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"parts":[{"text":"hello"}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	_ = resp.Body.Close()
	require.Nil(t, result.Error, "legacy tasks/send alias must still dispatch")
	require.NotNil(t, result.Result)
}

// TestA2A_TasksSendSubscribe_ArtifactEvent asserts that an A2A SSE update carrying
// an artifact is emitted as a "TaskArtifactUpdateEvent" and that a status-only
// update remains "TaskStatusUpdateEvent".
func TestA2A_TasksSendSubscribe_ArtifactEvent(t *testing.T) {
	dir := t.TempDir()
	clock := &fakeClock{t: time.Now()}
	store, err := session.NewStore(dir, clock)
	require.NoError(t, err)
	budgeter := budget.NewBudgeter(clock)
	srv := NewServer(store, budgeter)

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Create a non-terminal task.
	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"parts":[{"text":"hello"}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sendResult contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sendResult))
	_ = resp.Body.Close()
	taskID := sendResult.Result["task"].(map[string]any)["id"].(string)

	// Subscribe with a long timeout so the handler stays alive until we close it.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	subBody := `{"jsonrpc":"2.0","id":2,"method":"SubscribeToTask","params":{"id":"` + taskID + `"}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/a2a", strings.NewReader(subBody))
	require.NoError(t, err)

	type sseResult struct {
		eventTypes []string
		events     []string
		err        error
	}
	resCh := make(chan sseResult, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			resCh <- sseResult{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		scanner := bufio.NewScanner(resp.Body)
		var eventTypes, events []string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				eventTypes = append(eventTypes, strings.TrimPrefix(line, "event: "))
			}
			if strings.HasPrefix(line, "data: ") {
				events = append(events, strings.TrimPrefix(line, "data: "))
			}
		}
		resCh <- sseResult{eventTypes: eventTypes, events: events, err: scanner.Err()}
	}()

	// Give the handler time to register the subscriber.
	time.Sleep(100 * time.Millisecond)
	srv.mu.Lock()
	state := srv.sessionStates[taskID]
	require.NotEmpty(t, state.a2aSubs, "subscriber channel must be registered")
	ch := state.a2aSubs[0]
	srv.mu.Unlock()

	// Push an artifact-bearing final event through the broker's private channel.
	ch <- contract.A2ATaskUpdateEvent{
		ID: taskID,
		Artifact: &contract.A2AArtifact{
			Name:  "test-artifact",
			Parts: []contract.A2APart{{Kind: "text", Text: "artifact body"}},
		},
		Final: true,
	}

	res := <-resCh
	require.NoError(t, res.err)
	require.GreaterOrEqual(t, len(res.events), 2, "expected initial status event plus artifact event")
	require.GreaterOrEqual(t, len(res.eventTypes), 2)
	require.Equal(t, "TaskStatusUpdateEvent", res.eventTypes[0])
	require.Equal(t, "TaskArtifactUpdateEvent", res.eventTypes[len(res.eventTypes)-1])

	var last contract.A2ATaskUpdateEvent
	require.NoError(t, json.Unmarshal([]byte(res.events[len(res.events)-1]), &last))
	require.Equal(t, taskID, last.ID)
	require.NotNil(t, last.Artifact)
	require.Equal(t, "test-artifact", last.Artifact.Name)
	require.True(t, last.Final)
}
