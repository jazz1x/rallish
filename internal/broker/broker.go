// Package broker provides the HTTP/SSE server and route handlers for the hocketty broker.
package broker

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/jazz1x/hocketty/internal/budget"
	"github.com/jazz1x/hocketty/internal/router"
	"github.com/jazz1x/hocketty/internal/session"
	"github.com/jazz1x/hocketty/pkg/contract"
)

// Server is the HTTP/SSE broker.
type Server struct {
	mux      *http.ServeMux
	store    *session.Store
	budgeter *budget.Budgeter

	mu            sync.Mutex
	pendingReqs   map[string]contract.TurnRequest
	sessionStates map[string]sessionState
}

type sessionState struct {
	turnCount  int
	totalUsage contract.Usage
	lastTurn   *contract.LastTurn
	lastResp   *contract.TurnResponse
	preset     contract.Preset
	router     *router.Router
}

// NewServer creates a new broker Server.
func NewServer(store *session.Store, budgeter *budget.Budgeter) *Server {
	s := &Server{
		mux:           http.NewServeMux(),
		store:         store,
		budgeter:      budgeter,
		pendingReqs:   make(map[string]contract.TurnRequest),
		sessionStates: make(map[string]sessionState),
	}
	s.mux.HandleFunc("POST /sessions", s.handleCreateSession)
	s.mux.HandleFunc("GET /sessions/{id}", s.handleGetSession)
	s.mux.HandleFunc("GET /sessions/{id}/next", s.handleNextTurn)
	s.mux.HandleFunc("POST /sessions/{id}/turn", s.handlePostTurn)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "create_session")

	var req struct {
		Preset contract.Preset `json:"preset"`
		Task   contract.Task   `json:"task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.ErrorContext(ctx, "decode request", "error", err)
		http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
		return
	}

	sess, err := s.store.Create(ctx, req.Preset, req.Task)
	if err != nil {
		logger.ErrorContext(ctx, "create session", "error", err)
		http.Error(w, fmt.Sprintf("create session: %v", err), http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.sessionStates[sess.ID] = sessionState{
		turnCount: 0,
		preset:    req.Preset,
		router:    router.NewRouter(req.Preset),
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(sess); err != nil {
		logger.ErrorContext(ctx, "encode response", "error", err)
	}
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "get_session")

	id := r.PathValue("id")
	sess, err := s.store.Get(ctx, id)
	if err != nil {
		logger.ErrorContext(ctx, "get session", "error", err)
		http.Error(w, fmt.Sprintf("session not found: %v", err), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(sess); err != nil {
		logger.ErrorContext(ctx, "encode response", "error", err)
	}
}

func (s *Server) handleNextTurn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "next_turn")

	id := r.PathValue("id")

	s.mu.Lock()
	state, ok := s.sessionStates[id]
	if !ok {
		s.mu.Unlock()
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	sess, err := s.store.Get(ctx, id)
	if err != nil {
		s.mu.Unlock()
		logger.ErrorContext(ctx, "get session", "error", err)
		http.Error(w, fmt.Sprintf("get session: %v", err), http.StatusInternalServerError)
		return
	}

	turn := state.turnCount + 1
	roleID, err := state.router.Next(ctx, state.lastResp, turn)
	if err != nil {
		s.mu.Unlock()
		logger.ErrorContext(ctx, "router next", "error", err)
		http.Error(w, fmt.Sprintf("router error: %v", err), http.StatusInternalServerError)
		return
	}

	var role contract.Role
	for _, rl := range state.preset.Roles {
		if rl.ID == roleID {
			role = rl
			break
		}
	}

	remaining := s.budgeter.Remaining(state.preset.Budget, state.totalUsage, state.turnCount)

	req := contract.TurnRequest{
		Session:     id,
		Turn:        turn,
		Role:        roleID,
		RuntimeHint: role.Runtime,
		ModelHint:   role.Model,
		Budget:      remaining,
		LastTurn:    state.lastTurn,
		Task:        sess.Task,
		ExitWhen:    state.preset.ExitWhen,
	}

	s.pendingReqs[id] = req
	s.mu.Unlock()

	select {
	case <-ctx.Done():
		logger.InfoContext(ctx, "client disconnected", "session", id)
		return
	default:
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	data, err := json.Marshal(req)
	if err != nil {
		logger.ErrorContext(ctx, "marshal turn request", "error", err)
		return
	}

	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		logger.ErrorContext(ctx, "write sse", "error", err)
		return
	}

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *Server) handlePostTurn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "post_turn")

	id := r.PathValue("id")

	var resp contract.TurnResponse
	if err := json.NewDecoder(r.Body).Decode(&resp); err != nil {
		logger.ErrorContext(ctx, "decode request", "error", err)
		http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	req, ok := s.pendingReqs[id]
	if !ok {
		s.mu.Unlock()
		http.Error(w, "no pending turn for session", http.StatusBadRequest)
		return
	}
	delete(s.pendingReqs, id)

	state := s.sessionStates[id]
	state.turnCount++
	if resp.Usage != nil {
		state.totalUsage.TokensIn += resp.Usage.TokensIn
		state.totalUsage.TokensOut += resp.Usage.TokensOut
		state.totalUsage.Ms += resp.Usage.Ms
	}
	state.lastTurn = &contract.LastTurn{
		From:      req.Role,
		Runtime:   req.RuntimeHint,
		Summary:   resp.Summary,
		Artifacts: resp.Artifacts,
		SelfEval:  resp.SelfEval,
	}
	state.lastResp = &resp
	s.sessionStates[id] = state
	s.mu.Unlock()

	if err := s.store.Append(ctx, id, req, resp); err != nil {
		logger.ErrorContext(ctx, "append turn", "error", err)
		http.Error(w, fmt.Sprintf("append turn: %v", err), http.StatusInternalServerError)
		return
	}

	sess, err := s.store.Get(ctx, id)
	if err != nil {
		logger.ErrorContext(ctx, "get session", "error", err)
		http.Error(w, fmt.Sprintf("get session: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(sess); err != nil {
		logger.ErrorContext(ctx, "encode response", "error", err)
	}
}
