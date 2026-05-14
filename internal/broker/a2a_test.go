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

	resp, err := http.Get(ts.URL + "/.well-known/agent.json")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var card contract.AgentCard
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
	_ = resp.Body.Close()
	require.Equal(t, "rallish", card.Name)
	require.True(t, card.Capabilities.Streaming)
	require.NotEmpty(t, card.Skills)
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

	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"parts":[{"text":"hello"}]}}}`
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

	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"parts":[{"text":"hello"},{"text":"world"}]}}}`
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

	// Create via tasks/send
	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"parts":[{"text":"hello"}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sendResult contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sendResult))
	_ = resp.Body.Close()
	task := sendResult.Result["task"].(map[string]any)
	taskID := task["id"].(string)

	// Get via tasks/get
	getBody := `{"jsonrpc":"2.0","id":2,"method":"tasks/get","params":{"id":"` + taskID + `"}}`
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
	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"parts":[{"text":"hello"}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sendResult contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sendResult))
	_ = resp.Body.Close()
	taskID := sendResult.Result["task"].(map[string]any)["id"].(string)

	// Cancel
	cancelBody := `{"jsonrpc":"2.0","id":3,"method":"tasks/cancel","params":{"id":"` + taskID + `"}}`
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
	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"parts":[{"text":"hello"}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sendResult contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sendResult))
	_ = resp.Body.Close()
	taskID := sendResult.Result["task"].(map[string]any)["id"].(string)

	// Cancel first time
	cancelBody := `{"jsonrpc":"2.0","id":2,"method":"tasks/cancel","params":{"id":"` + taskID + `"}}`
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

	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{}}`
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

	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/get","params":{}}`
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

	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/cancel","params":{}}`
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
	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"parts":[{"text":"hello"}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sendResult contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sendResult))
	_ = resp.Body.Close()
	taskID := sendResult.Result["task"].(map[string]any)["id"].(string)

	// Cancel to make terminal
	cancelBody := `{"jsonrpc":"2.0","id":2,"method":"tasks/cancel","params":{"id":"` + taskID + `"}}`
	resp, err = http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(cancelBody))
	require.NoError(t, err)
	_ = resp.Body.Close()

	// Subscribe should immediately return the terminal event without leaking a channel
	subBody := `{"jsonrpc":"2.0","id":3,"method":"tasks/sendSubscribe","params":{"id":"` + taskID + `"}}`
	resp, err = http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(subBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	scanner := bufio.NewScanner(resp.Body)
	var events []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			events = append(events, strings.TrimPrefix(line, "data: "))
		}
	}
	_ = resp.Body.Close()
	require.NoError(t, scanner.Err())
	require.Len(t, events, 1)

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
	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"parts":[{"text":"hello"}]}}}`
	resp, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	var sendResult contract.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sendResult))
	_ = resp.Body.Close()
	taskID := sendResult.Result["task"].(map[string]any)["id"].(string)

	// Subscribe with cancelable context
	ctx, cancel := context.WithCancel(context.Background())
	subBody := `{"jsonrpc":"2.0","id":2,"method":"tasks/sendSubscribe","params":{"id":"` + taskID + `"}}`
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
	body := `{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"parts":[{"text":"hello"}]}}}`
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
			subBody := `{"jsonrpc":"2.0","id":2,"method":"tasks/sendSubscribe","params":{"id":"` + taskID + `"}}`
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
	cancelBody := `{"jsonrpc":"2.0","id":3,"method":"tasks/cancel","params":{"id":"` + taskID + `"}}`
	resp2, err := http.Post(ts.URL+"/a2a", "application/json", strings.NewReader(cancelBody))
	require.NoError(t, err)
	_ = resp2.Body.Close()

	wg.Wait()
}
