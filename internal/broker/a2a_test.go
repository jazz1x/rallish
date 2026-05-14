package broker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
