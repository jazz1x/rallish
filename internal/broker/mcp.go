// Package broker provides the HTTP/SSE server and route handlers for the rallish broker.
//
// This file implements the MCP (Model Context Protocol) 2025-03-26 server surface
// for rally baton-passing sessions.
package broker

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jazz1x/rallish/internal/buildinfo"
	"github.com/jazz1x/rallish/pkg/contract"
)

// mcpTransportSession is a single client connection to the MCP SSE endpoint.
type mcpTransportSession struct {
	id      string
	respCh  chan contract.MCPJSONRPCResponse
	created time.Time
}

// mcpTransportManager holds active MCP SSE transport sessions.
type mcpTransportManager struct {
	mu       sync.Mutex
	sessions map[string]*mcpTransportSession
}

func newMCPTransportManager() *mcpTransportManager {
	return &mcpTransportManager{
		sessions: make(map[string]*mcpTransportSession),
	}
}

// create registers a new transport session and returns its session ID.
func (m *mcpTransportManager) create() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fall back to a time-based ID if crypto/rand fails.
		return fmt.Sprintf("mcpts_%d_fallback", time.Now().UnixNano())
	}
	id := "mcpts_" + hex.EncodeToString(b)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = &mcpTransportSession{
		id:      id,
		respCh:  make(chan contract.MCPJSONRPCResponse, 16),
		created: time.Now(),
	}
	return id
}

// get returns the response channel for a transport session.
func (m *mcpTransportManager) get(id string) (chan contract.MCPJSONRPCResponse, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, false
	}
	return s.respCh, true
}

// remove deletes a transport session.
func (m *mcpTransportManager) remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// registerMCPRoutes wires the MCP endpoints into the broker mux.
func (s *Server) registerMCPRoutes() {
	s.mux.HandleFunc("GET /mcp/sse", s.handleMCPSSE)
	s.mux.HandleFunc("POST /mcp/message", s.handleMCPMessage)
}

// handleMCPSSE handles GET /mcp/sse — the MCP SSE transport endpoint.
func (s *Server) handleMCPSSE(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "mcp_sse")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		logger.ErrorContext(ctx, "response writer does not support flushing")
		return
	}

	id := s.mcpTransport.create()
	defer s.mcpTransport.remove(id)

	logger.InfoContext(ctx, "mcp_transport_open", "session_id", id)

	endpoint := fmt.Sprintf("/mcp/message?session_id=%s", id)
	if _, err := fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpoint); err != nil {
		logger.ErrorContext(ctx, "write endpoint event", "error", err)
		return
	}
	flusher.Flush()

	respCh, ok := s.mcpTransport.get(id)
	if !ok {
		logger.ErrorContext(ctx, "transport session disappeared after create", "session_id", id)
		return
	}

	// Heartbeat to keep the TCP connection alive.
	ticker := time.NewTicker(sseHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.InfoContext(ctx, "mcp_transport_close", "session_id", id, "reason", "client_disconnect")
			return
		case resp := <-respCh:
			data, err := json.Marshal(resp)
			if err != nil {
				logger.ErrorContext(ctx, "marshal mcp response", "error", err)
				continue
			}
			if err := writeSSEMessage(w, flusher, data); err != nil {
				logger.ErrorContext(ctx, "write mcp response", "error", err)
				return
			}
		case <-ticker.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				logger.ErrorContext(ctx, "write mcp heartbeat", "error", err)
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSEMessage(w http.ResponseWriter, flusher http.Flusher, data []byte) error {
	if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// handleMCPMessage handles POST /mcp/message?session_id=<id>.
func (s *Server) handleMCPMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "mcp_message")

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}

	respCh, ok := s.mcpTransport.get(sessionID)
	if !ok {
		http.Error(w, "unknown session_id", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.ErrorContext(ctx, "read mcp request body", "error", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	var req contract.MCPJSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		resp := contract.MCPJSONRPCResponse{
			JSONRPC: "2.0",
			ID:      nil,
			Error:   &contract.MCPJSONRPCError{Code: -32700, Message: "Parse error", Data: err.Error()},
		}
		respCh <- resp
		w.WriteHeader(http.StatusAccepted)
		return
	}

	resp := s.dispatchMCP(ctx, req)
	respCh <- resp

	w.WriteHeader(http.StatusAccepted)
}

// dispatchMCP routes an MCP JSON-RPC request to the appropriate handler.
func (s *Server) dispatchMCP(ctx context.Context, req contract.MCPJSONRPCRequest) contract.MCPJSONRPCResponse {
	logger := slog.With("handler", "mcp_dispatch", "method", req.Method)

	switch req.Method {
	case "initialize":
		return mcpInitialize(req)
	case "notifications/initialized":
		// No response required for this notification.
		return contract.MCPJSONRPCResponse{JSONRPC: "2.0", ID: req.ID}
	case "tools/list":
		return mcpToolsList(req)
	case "tools/call":
		return s.mcpToolCall(ctx, req)
	default:
		logger.InfoContext(ctx, "mcp_method_not_found")
		return mcpErrorResponse(req.ID, -32601, "Method not found", nil)
	}
}

func mcpErrorResponse(id any, code int, message string, data any) contract.MCPJSONRPCResponse {
	return contract.MCPJSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &contract.MCPJSONRPCError{Code: code, Message: message, Data: data},
	}
}

func mcpInitialize(req contract.MCPJSONRPCRequest) contract.MCPJSONRPCResponse {
	result := contract.MCPInitializeResult{
		ProtocolVersion: contract.MCPProtocolVersion,
		Capabilities:    contract.MCPCapabilities{Tools: map[string]any{}},
		ServerInfo: contract.MCPServerInfo{
			Name:    "rallish",
			Version: buildinfo.Version(),
		},
	}
	return contract.MCPJSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": result.ProtocolVersion,
			"capabilities":    result.Capabilities,
			"serverInfo":      result.ServerInfo,
		},
	}
}

func mcpToolsList(req contract.MCPJSONRPCRequest) contract.MCPJSONRPCResponse {
	tools := []contract.MCPTool{
		{
			Name:        "rally_create",
			Description: "Create a new rally baton-passing session.",
			InputSchema: contract.MCPToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"participants": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"repo":         map[string]any{"type": "string"},
					"task":         map[string]any{"type": "string"},
					"first_holder": map[string]any{"type": "string"},
				},
				Required: []string{"participants"},
			},
		},
		{
			Name:        "rally_join",
			Description: "Wait until the baton is passed to the named participant. Returns a BatonEvent or a timeout marker.",
			InputSchema: contract.MCPToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"session_id": map[string]any{"type": "string"},
					"as":         map[string]any{"type": "string"},
					"timeout_ms": map[string]any{"type": "integer", "default": 30000},
				},
				Required: []string{"session_id", "as"},
			},
		},
		{
			Name:        "rally_done",
			Description: "Pass the baton to the next participant.",
			InputSchema: contract.MCPToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"session_id": map[string]any{"type": "string"},
					"as":         map[string]any{"type": "string"},
					"note":       map[string]any{"type": "string"},
					"handoff_to": map[string]any{"type": "string"},
				},
				Required: []string{"session_id", "as"},
			},
		},
		{
			Name:        "rally_status",
			Description: "Get the current snapshot of a rally session.",
			InputSchema: contract.MCPToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"session_id": map[string]any{"type": "string"},
				},
				Required: []string{"session_id"},
			},
		},
		{
			Name:        "rally_interrupt",
			Description: "Forcibly interrupt a rally session and close all streams.",
			InputSchema: contract.MCPToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"session_id": map[string]any{"type": "string"},
				},
				Required: []string{"session_id"},
			},
		},
	}

	return contract.MCPJSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": tools,
		},
	}
}

func (s *Server) mcpToolCall(ctx context.Context, req contract.MCPJSONRPCRequest) contract.MCPJSONRPCResponse {
	logger := slog.With("handler", "mcp_tool_call")

	var params contract.MCPToolCallRequest
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return mcpErrorResponse(req.ID, -32602, "Invalid params", err.Error())
	}

	var result any
	var err error
	switch params.Name {
	case "rally_create":
		result, err = mcpToolRallyCreate(params.Arguments)
	case "rally_join":
		result, err = mcpToolRallyJoin(ctx, params.Arguments)
	case "rally_done":
		result, err = mcpToolRallyDone(params.Arguments)
	case "rally_status":
		result, err = mcpToolRallyStatus(params.Arguments)
	case "rally_interrupt":
		result, err = mcpToolRallyInterrupt(params.Arguments)
	default:
		logger.InfoContext(ctx, "mcp_tool_not_found", "tool", params.Name)
		return mcpErrorResponse(req.ID, -32602, "Unknown tool", params.Name)
	}

	if err != nil {
		return mcpToolErrorResponse(req.ID, err)
	}

	return mcpToolSuccessResponse(req.ID, result)
}

func mcpToolSuccessResponse(id any, value any) contract.MCPJSONRPCResponse {
	text, err := json.Marshal(value)
	if err != nil {
		return mcpErrorResponse(id, -32603, "Internal error: failed to marshal result", err.Error())
	}
	return contract.MCPJSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: map[string]any{
			"content": []contract.MCPContent{{Type: contract.MCPContentTypeText, Text: string(text)}},
		},
	}
}

func mcpToolErrorResponse(id any, err error) contract.MCPJSONRPCResponse {
	return contract.MCPJSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: map[string]any{
			"content": []contract.MCPContent{{Type: contract.MCPContentTypeText, Text: err.Error()}},
			"isError": true,
		},
	}
}

func mcpToolRallyCreate(args json.RawMessage) (any, error) {
	var req contract.NewRallyRequest
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	return createRallySession(req)
}

func mcpToolRallyDone(args json.RawMessage) (any, error) {
	var p contract.RallyDoneToolArgs
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	id, err := contract.NewSessionID(p.SessionID)
	if err != nil {
		return nil, err
	}
	as, err := contract.NewParticipantName(p.As)
	if err != nil {
		return nil, err
	}
	return advanceRallyBaton(id, as, contract.DoneRequest{Note: p.Note, HandoffTo: p.HandoffTo})
}

func mcpToolRallyStatus(args json.RawMessage) (any, error) {
	var p contract.RallyStatusToolArgs
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	id, err := contract.NewSessionID(p.SessionID)
	if err != nil {
		return nil, err
	}
	return getRallySession(id)
}

func mcpToolRallyInterrupt(args json.RawMessage) (any, error) {
	var p contract.RallyInterruptToolArgs
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	id, err := contract.NewSessionID(p.SessionID)
	if err != nil {
		return nil, err
	}
	return interruptRallySession(id)
}

func mcpToolRallyJoin(ctx context.Context, args json.RawMessage) (any, error) {
	var p contract.RallyJoinToolArgs
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	id, err := contract.NewSessionID(p.SessionID)
	if err != nil {
		return nil, err
	}
	as, err := contract.NewParticipantName(p.As)
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(p.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if timeout > 5*time.Minute {
		timeout = 5 * time.Minute
	}

	rallies.mu.Lock()
	sess, ok := rallies.sessions[id]
	if !ok {
		rallies.mu.Unlock()
		return nil, fmt.Errorf("session %q: %w", id, contract.ErrSessionNotFound)
	}

	// Validate participant membership.
	isMember := false
	for _, pName := range sess.participants {
		if pName == as {
			isMember = true
			break
		}
	}
	if !isMember {
		rallies.mu.Unlock()
		return nil, fmt.Errorf("participant %q is not in this session", as)
	}

	// Interrupted sessions return the close sentinel immediately.
	if sess.status == contract.RallyStateInterrupted {
		rallies.mu.Unlock()
		return contract.BatonEvent{Closed: true}, nil
	}

	// Reject duplicate connections on the same transport or across transports.
	if existingCh := sess.streams[as]; existingCh != nil {
		rallies.mu.Unlock()
		return nil, fmt.Errorf("participant %q: %w", as, contract.ErrAlreadyConnected)
	}

	ch := make(chan contract.BatonEvent, 1)
	sess.streams[as] = ch
	nowMS := time.Now().UnixMilli()
	sess.lastSeen[as] = nowMS

	var immediateEvent *contract.BatonEvent
	if sess.status == contract.RallyStateIdle && sess.participants[0] == as {
		sess.turnN++
		sess.holder = as
		sess.status = contract.RallyTurnState(as.String())
		immediateEvent = &contract.BatonEvent{TurnN: sess.turnN, From: ""}
	} else if sess.status == contract.RallyTurnState(as.String()) {
		var from, note string
		if n := len(sess.history); n > 0 {
			from = sess.history[n-1].From
			note = sess.history[n-1].Note
		}
		immediateEvent = &contract.BatonEvent{TurnN: sess.turnN, From: from, Note: note}
	}

	if immediateEvent != nil {
		ch <- *immediateEvent
	}
	rallies.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		cleanupStream(id, as)
		return nil, ctx.Err()
	case evt, more := <-ch:
		if !more {
			return contract.BatonEvent{Closed: true}, nil
		}
		cleanupStream(id, as)
		return evt, nil
	case <-timer.C:
		cleanupStream(id, as)
		// Drain a just-arrived event to avoid leaking it.
		select {
		case <-ch:
		default:
		}
		return contract.MCPTimeoutResult{Timeout: true}, nil
	}
}

// cleanupStream is defined in rally.go; it removes a participant's SSE channel
// from the session. We rely on it here for MCP join cleanup as well.
//
// It is intentionally not duplicated so HTTP and MCP transports share the same
// stream lifecycle helper.
