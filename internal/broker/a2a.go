// Package broker provides the HTTP/SSE server and route handlers for the rallish broker.
//
// This file implements the Google A2A (Agent2Agent) protocol endpoints layered on
// top of the internal turn-taking contract.
package broker

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jazz1x/rallish/internal/buildinfo"
	"github.com/jazz1x/rallish/internal/preset"
	"github.com/jazz1x/rallish/internal/router"
	"github.com/jazz1x/rallish/internal/session"
	"github.com/jazz1x/rallish/pkg/contract"
)

func (s *Server) registerA2ARoutes() {
	s.mux.HandleFunc("GET /.well-known/agent.json", s.handleAgentCard)
	s.mux.HandleFunc("POST /a2a", s.handleA2A)
}

func (s *Server) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	card := contract.AgentCard{
		Name:             "rallish",
		Description:      "A local broker for multi-agent turn-taking",
		Version:          buildinfo.Version(),
		URL:              fmt.Sprintf("http://%s", r.Host),
		DocumentationURL: "https://github.com/jazz1x/rallish",
		Capabilities: contract.AgentCapability{
			Streaming: true,
		},
		DefaultInputModes:  []string{"text/plain", "application/json"},
		DefaultOutputModes: []string{"text/plain", "application/json"},
		Skills: []contract.AgentSkill{
			{
				ID:          "pair-review",
				Name:        "Pair Review",
				Description: "Planner / executor / reviewer turn-taking with structured output",
				Tags:        []string{"turn-taking", "review", "code"},
			},
			{
				ID:          "solo-ralph",
				Name:        "Solo Ralph",
				Description: "Single-agent execution with budget and exit guardrails",
				Tags:        []string{"solo", "budget", "execution"},
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(card); err != nil {
		slog.Error("encode agent card", "error", err)
	}
}

func (s *Server) handleA2A(w http.ResponseWriter, r *http.Request) {
	var req contract.JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPCError(w, req.ID, -32700, "Parse error", err.Error())
		return
	}

	switch req.Method {
	case "tasks/send":
		s.handleA2ASend(w, r, req)
	case "tasks/sendSubscribe":
		s.handleA2ASendSubscribe(w, r, req)
	case "tasks/get":
		s.handleA2AGet(w, r, req)
	case "tasks/cancel":
		s.handleA2ACancel(w, r, req)
	default:
		writeJSONRPCError(w, req.ID, -32601, "Method not found", nil)
	}
}

func writeJSONRPCError(w http.ResponseWriter, id any, code int, message string, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := contract.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &contract.JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func writeJSONRPCResult(w http.ResponseWriter, id any, result map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	resp := contract.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleA2ASend(w http.ResponseWriter, r *http.Request, req contract.JSONRPCRequest) {
	ctx := r.Context()
	message, ok := extractTextFromParts(req.Params, "message")
	if !ok {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params: missing message", nil)
		return
	}

	presets, err := preset.LoadEmbedded()
	if err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error: cannot load presets", err.Error())
		return
	}
	p, ok := presets["solo-ralph"]
	if !ok {
		writeJSONRPCError(w, req.ID, -32603, "Internal error: default preset not found", nil)
		return
	}

	task := contract.Task{
		Title: "A2A task",
		Body:  message,
	}

	sess, err := s.store.Create(ctx, p, task)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error: cannot create session", err.Error())
		return
	}

	s.mu.Lock()
	s.sessionStates[sess.ID] = sessionState{
		turnCount: 0,
		preset:    p,
		router:    router.NewRouter(p),
		createdAt: sess.CreatedAt,
		notify:    make(chan struct{}),
	}
	s.mu.Unlock()

	a2aTask := mapSessionToA2ATask(sess)
	result := map[string]any{"task": a2aTask}
	writeJSONRPCResult(w, req.ID, result)
}

func (s *Server) handleA2AGet(w http.ResponseWriter, r *http.Request, req contract.JSONRPCRequest) {
	ctx := r.Context()
	taskID, ok := req.Params["id"].(string)
	if !ok || taskID == "" {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params: missing id", nil)
		return
	}

	sess, err := s.store.Get(ctx, taskID)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32602, "Task not found", err.Error())
		return
	}

	s.mu.Lock()
	state, hasState := s.sessionStates[taskID]
	s.mu.Unlock()

	a2aTask := mapSessionToA2ATask(sess)
	if hasState {
		a2aTask.Status = contract.A2ATaskStatus{
			State:  terminalReasonToTaskState(state.terminal, state.terminalReason),
			Reason: state.terminalReason,
		}
	}
	result := map[string]any{"task": a2aTask}
	writeJSONRPCResult(w, req.ID, result)
}

func (s *Server) handleA2ACancel(w http.ResponseWriter, r *http.Request, req contract.JSONRPCRequest) {
	ctx := r.Context()
	taskID, ok := req.Params["id"].(string)
	if !ok || taskID == "" {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params: missing id", nil)
		return
	}

	s.mu.Lock()
	state, ok := s.sessionStates[taskID]
	if !ok {
		s.mu.Unlock()
		writeJSONRPCError(w, req.ID, -32602, "Task not found", nil)
		return
	}
	state.terminal = true
	state.terminalReason = "canceled"
	oldNotify := state.notify
	state.notify = make(chan struct{})
	s.sessionStates[taskID] = state
	s.mu.Unlock()

	if oldNotify != nil {
		close(oldNotify)
	}

	sess, err := s.store.Get(ctx, taskID)
	if err != nil {
		writeJSONRPCError(w, req.ID, -32603, "Internal error", err.Error())
		return
	}
	a2aTask := mapSessionToA2ATask(sess)
	a2aTask.Status = contract.A2ATaskStatus{State: contract.TaskStateCanceled, Reason: "canceled"}
	result := map[string]any{"task": a2aTask}
	writeJSONRPCResult(w, req.ID, result)
}

func (s *Server) handleA2ASendSubscribe(w http.ResponseWriter, r *http.Request, req contract.JSONRPCRequest) {
	ctx := r.Context()
	taskID, ok := req.Params["id"].(string)
	if !ok || taskID == "" {
		writeJSONRPCError(w, req.ID, -32602, "Invalid params: missing id", nil)
		return
	}

	s.mu.Lock()
	state, ok := s.sessionStates[taskID]
	if !ok {
		s.mu.Unlock()
		writeJSONRPCError(w, req.ID, -32602, "Task not found", nil)
		return
	}

	ch := make(chan contract.A2ATaskUpdateEvent, 4)
	state.a2aSubs = append(state.a2aSubs, ch)
	s.sessionStates[taskID] = state
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if ok {
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
	}

	event := contract.A2ATaskUpdateEvent{
		ID: taskID,
		Status: contract.A2ATaskStatus{
			State:  terminalReasonToTaskState(state.terminal, state.terminalReason),
			Reason: state.terminalReason,
		},
		Final: state.terminal,
	}
	data, _ := json.Marshal(event)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	if ok {
		flusher.Flush()
	}

	if state.terminal {
		return
	}

	for {
		select {
		case evt, more := <-ch:
			if !more {
				return
			}
			data, _ := json.Marshal(evt)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			if ok {
				flusher.Flush()
			}
			if evt.Final {
				return
			}
		case <-ctx.Done():
			s.mu.Lock()
			state = s.sessionStates[taskID]
			filtered := make([]chan contract.A2ATaskUpdateEvent, 0, len(state.a2aSubs))
			for _, c := range state.a2aSubs {
				if c != ch {
					filtered = append(filtered, c)
				}
			}
			state.a2aSubs = filtered
			s.sessionStates[taskID] = state
			s.mu.Unlock()
			return
		}
	}
}

func extractTextFromParts(params map[string]any, key string) (string, bool) {
	msg, ok := params[key].(map[string]any)
	if !ok {
		return "", false
	}
	parts, ok := msg["parts"].([]any)
	if !ok || len(parts) == 0 {
		return "", false
	}
	part0, ok := parts[0].(map[string]any)
	if !ok {
		return "", false
	}
	text, ok := part0["text"].(string)
	return text, ok
}

func mapSessionToA2ATask(sess session.Session) contract.A2ATask {
	return contract.A2ATask{
		ID:     sess.ID,
		Status: contract.A2ATaskStatus{State: contract.TaskStateSubmitted},
	}
}

func terminalReasonToTaskState(terminal bool, reason string) contract.TaskState {
	if !terminal {
		return contract.TaskStateWorking
	}
	if reason == "canceled" {
		return contract.TaskStateCanceled
	}
	return contract.TaskStateCompleted
}
