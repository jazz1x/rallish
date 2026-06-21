package broker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/stretchr/testify/require"
)

// mcpConnection holds an active MCP SSE transport and a channel that receives
// every JSON-RPC response sent by the server over that transport.
type mcpConnection struct {
	endpoint string
	respCh   chan contract.MCPJSONRPCResponse
	cancel   func()
	nextID   int
}

// connectMCP opens an SSE connection to /mcp/sse, reads the endpoint event, and
// starts a goroutine that forwards all server messages to respCh.
func connectMCP(t *testing.T, ts *httptest.Server) *mcpConnection {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/mcp/sse", nil)
	require.NoError(t, err)

	resp, err := (&http.Client{}).Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	ctx, cancel := context.WithCancel(context.Background())
	conn := &mcpConnection{
		respCh: make(chan contract.MCPJSONRPCResponse, 16),
		cancel: func() {
			cancel()
			_ = resp.Body.Close()
		},
		nextID: 1,
	}
	t.Cleanup(conn.cancel)

	scanner := bufio.NewScanner(resp.Body)
	require.True(t, scanner.Scan(), "expected endpoint event")
	line := scanner.Text()
	require.True(t, strings.HasPrefix(line, "event: endpoint"), "expected endpoint event, got %q", line)
	require.True(t, scanner.Scan(), "expected endpoint data")
	dataLine := scanner.Text()
	prefix := "data: "
	require.True(t, strings.HasPrefix(dataLine, prefix), "expected endpoint data, got %q", dataLine)
	conn.endpoint = ts.URL + dataLine[len(prefix):]

	go func() {
		var eventData string
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if eventData != "" {
					var msg contract.MCPJSONRPCResponse
					if err := json.Unmarshal([]byte(eventData), &msg); err == nil {
						select {
						case conn.respCh <- msg:
						case <-ctx.Done():
							return
						}
					}
					eventData = ""
				}
				continue
			}
			if strings.HasPrefix(line, "data: ") {
				eventData = line[len("data: "):]
			}
		}
	}()

	return conn
}

// sendMCPMessage posts a JSON-RPC request to the connection endpoint.
func sendMCPMessage(t *testing.T, conn *mcpConnection, req contract.MCPJSONRPCRequest) {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	resp, err := http.Post(conn.endpoint, "application/json", bytes.NewReader(body)) //nolint:gosec // httptest URL
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
}

// expectMCPResponse waits for a JSON-RPC response on the connection channel.
func expectMCPResponse(t *testing.T, conn *mcpConnection, timeout time.Duration) contract.MCPJSONRPCResponse {
	t.Helper()
	select {
	case resp := <-conn.respCh:
		return resp
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for MCP response")
		return contract.MCPJSONRPCResponse{}
	}
}

// callMCPTool sends a tools/call request and returns the parsed result value.
// If the tool returns an error, the returned error contains the tool message.
func callMCPTool(t *testing.T, conn *mcpConnection, name string, args any) (json.RawMessage, error) {
	t.Helper()
	argBytes, err := json.Marshal(args)
	require.NoError(t, err)

	conn.nextID++
	reqID := conn.nextID
	req := contract.MCPJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  "tools/call",
		Params:  mustJSON(t, contract.MCPToolCallRequest{Name: name, Arguments: argBytes}),
	}
	sendMCPMessage(t, conn, req)

	var resp contract.MCPJSONRPCResponse
	for {
		resp = expectMCPResponse(t, conn, 5*time.Second)
		if mcpIDEqual(resp.ID, reqID) {
			break
		}
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	content, ok := resp.Result["content"].([]any)
	require.True(t, ok, "expected content array in tool result")
	require.NotEmpty(t, content, "expected at least one content item")

	first := content[0].(map[string]any)
	require.Equal(t, "text", first["type"])
	text := first["text"].(string)

	if isErr, _ := resp.Result["isError"].(bool); isErr {
		return nil, fmt.Errorf("tool error: %s", text)
	}

	return json.RawMessage(text), nil
}

func mcpIDEqual(a, b any) bool {
	toFloat := func(v any) (float64, bool) {
		switch x := v.(type) {
		case int:
			return float64(x), true
		case float64:
			return x, true
		}
		return 0, false
	}
	if af, ok := toFloat(a); ok {
		if bf, ok := toFloat(b); ok {
			return af == bf
		}
	}
	if as, ok := a.(string); ok {
		bs, ok := b.(string)
		return ok && as == bs
	}
	return false
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestMCPInitialize(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)
	conn := connectMCP(t, ts)

	req := contract.MCPJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      0,
		Method:  "initialize",
		Params: mustJSON(t, contract.MCPInitializeRequest{
			ProtocolVersion: contract.MCPProtocolVersion,
			ClientInfo:      contract.MCPClientInfo{Name: "test", Version: "0.1"},
		}),
	}
	sendMCPMessage(t, conn, req)
	resp := expectMCPResponse(t, conn, 5*time.Second)
	require.Nil(t, resp.Error)
	require.Equal(t, "2025-03-26", resp.Result["protocolVersion"])
}

func TestMCPToolsList(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)
	conn := connectMCP(t, ts)

	// initialize first
	req := contract.MCPJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      0,
		Method:  "initialize",
		Params: mustJSON(t, contract.MCPInitializeRequest{
			ProtocolVersion: contract.MCPProtocolVersion,
			ClientInfo:      contract.MCPClientInfo{Name: "test", Version: "0.1"},
		}),
	}
	sendMCPMessage(t, conn, req)
	_ = expectMCPResponse(t, conn, 5*time.Second)

	req = contract.MCPJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}
	sendMCPMessage(t, conn, req)
	resp := expectMCPResponse(t, conn, 5*time.Second)
	require.Nil(t, resp.Error)

	tools, ok := resp.Result["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 5)
}

func TestMCPCreateRally(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)
	conn := connectMCP(t, ts)

	raw, err := callMCPTool(t, conn, "rally_create", map[string]any{
		"participants": []string{"alice", "bob"},
		"task":         "refactor auth",
	})
	require.NoError(t, err)

	var sess contract.RallySession
	require.NoError(t, json.Unmarshal(raw, &sess))
	require.NotEmpty(t, sess.ID)
	require.Equal(t, []string{"alice", "bob"}, sess.Participants)
	require.Equal(t, contract.RallyStateIdle, sess.Status)
}

func TestMCPJoinAndDonePingPong(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)
	connAlice := connectMCP(t, ts)
	connBob := connectMCP(t, ts)

	// Create the session over HTTP so the MCP connections are free for concurrent
	// join/done calls.
	sess := createSession(t, ts, []string{"alice", "bob"})

	// alice joins via MCP and gets the baton immediately (first participant).
	raw, err := callMCPTool(t, connAlice, "rally_join", map[string]any{
		"session_id": sess.ID,
		"as":         "alice",
		"timeout_ms": 5000,
	})
	require.NoError(t, err)
	var aliceEvt contract.BatonEvent
	require.NoError(t, json.Unmarshal(raw, &aliceEvt))
	require.Equal(t, 1, aliceEvt.TurnN)

	// bob joins via MCP and waits for the baton.
	var wg sync.WaitGroup
	wg.Add(1)
	var bobEvt contract.BatonEvent
	go func() {
		defer wg.Done()
		raw, err := callMCPTool(t, connBob, "rally_join", map[string]any{
			"session_id": sess.ID,
			"as":         "bob",
			"timeout_ms": 5000,
		})
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &bobEvt))
	}()

	// Wait until bob's join has registered its channel.
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

	// alice passes the baton to bob.
	_, err = callMCPTool(t, connAlice, "rally_done", map[string]any{
		"session_id": sess.ID,
		"as":         "alice",
		"note":       "done with turn 1",
	})
	require.NoError(t, err)
	wg.Wait()
	require.Equal(t, 2, bobEvt.TurnN)
	require.Equal(t, "alice", bobEvt.From)
	require.Equal(t, "done with turn 1", bobEvt.Note)
}

func TestMCPJoinTimeout(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)
	conn := connectMCP(t, ts)

	raw, err := callMCPTool(t, conn, "rally_create", map[string]any{
		"participants": []string{"alice", "bob"},
	})
	require.NoError(t, err)
	var sess contract.RallySession
	require.NoError(t, json.Unmarshal(raw, &sess))

	raw, err = callMCPTool(t, conn, "rally_join", map[string]any{
		"session_id": sess.ID,
		"as":         "bob",
		"timeout_ms": 50,
	})
	require.NoError(t, err)

	var timeout contract.MCPTimeoutResult
	require.NoError(t, json.Unmarshal(raw, &timeout))
	require.True(t, timeout.Timeout)

	// bob should be able to join again after timeout cleanup.
	raw, err = callMCPTool(t, conn, "rally_join", map[string]any{
		"session_id": sess.ID,
		"as":         "bob",
		"timeout_ms": 50,
	})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &timeout))
	require.True(t, timeout.Timeout)
}

func TestMCPJoinInterruptedSession(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)
	conn := connectMCP(t, ts)

	raw, err := callMCPTool(t, conn, "rally_create", map[string]any{
		"participants": []string{"alice", "bob"},
	})
	require.NoError(t, err)
	var sess contract.RallySession
	require.NoError(t, json.Unmarshal(raw, &sess))

	_, err = callMCPTool(t, conn, "rally_interrupt", map[string]any{
		"session_id": sess.ID,
	})
	require.NoError(t, err)

	raw, err = callMCPTool(t, conn, "rally_join", map[string]any{
		"session_id": sess.ID,
		"as":         "alice",
		"timeout_ms": 1000,
	})
	require.NoError(t, err)

	var evt contract.BatonEvent
	require.NoError(t, json.Unmarshal(raw, &evt))
	require.True(t, evt.Closed)
}

func TestMCPDoneNotHolder(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)
	conn := connectMCP(t, ts)

	raw, err := callMCPTool(t, conn, "rally_create", map[string]any{
		"participants": []string{"alice", "bob"},
		"first_holder": "alice",
	})
	require.NoError(t, err)
	var sess contract.RallySession
	require.NoError(t, json.Unmarshal(raw, &sess))

	_, err = callMCPTool(t, conn, "rally_done", map[string]any{
		"session_id": sess.ID,
		"as":         "bob",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), contract.ErrNotHolder.Error())
}

func TestMCPInterruptSession(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)
	connAdmin := connectMCP(t, ts)
	connBob := connectMCP(t, ts)

	raw, err := callMCPTool(t, connAdmin, "rally_create", map[string]any{
		"participants": []string{"alice", "bob"},
	})
	require.NoError(t, err)
	var sess contract.RallySession
	require.NoError(t, json.Unmarshal(raw, &sess))

	var wg sync.WaitGroup
	wg.Add(1)
	var joinErr error
	go func() {
		defer wg.Done()
		_, joinErr = callMCPTool(t, connBob, "rally_join", map[string]any{
			"session_id": sess.ID,
			"as":         "bob",
			"timeout_ms": 5000,
		})
	}()

	time.Sleep(50 * time.Millisecond)
	_, err = callMCPTool(t, connAdmin, "rally_interrupt", map[string]any{
		"session_id": sess.ID,
	})
	require.NoError(t, err)
	wg.Wait()
	require.NoError(t, joinErr)
}

func TestMCPCoexistWithHTTPRally(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)
	conn := connectMCP(t, ts)

	raw, err := callMCPTool(t, conn, "rally_create", map[string]any{
		"participants": []string{"alice", "bob"},
	})
	require.NoError(t, err)
	var sess contract.RallySession
	require.NoError(t, json.Unmarshal(raw, &sess))

	// alice joins via HTTP SSE.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, awaitBaton(ctx, ts.URL, sess.ID, "alice"))

	// bob joins via MCP and waits.
	var wg sync.WaitGroup
	wg.Add(1)
	var bobEvt contract.BatonEvent
	go func() {
		defer wg.Done()
		raw, err := callMCPTool(t, conn, "rally_join", map[string]any{
			"session_id": sess.ID,
			"as":         "bob",
			"timeout_ms": 5000,
		})
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &bobEvt))
	}()

	time.Sleep(50 * time.Millisecond)

	// alice passes baton via HTTP.
	resp := postJSON(t, ts.URL+"/rally/sessions/"+sess.ID+"/done?as=alice", `{"note":"from http"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	wg.Wait()
	require.Equal(t, 2, bobEvt.TurnN)
	require.Equal(t, "alice", bobEvt.From)
	require.Equal(t, "from http", bobEvt.Note)
}

func TestMCPTransportSessionRequired(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)

	resp, err := http.Post(ts.URL+"/mcp/message", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), "session_id")
}

func TestMCPUnknownTool(t *testing.T) {
	resetRallies()
	ts, _ := newRallyServer(t)
	conn := connectMCP(t, ts)

	_, err := callMCPTool(t, conn, "rally_nonexistent", map[string]any{})
	require.Error(t, err)
}
