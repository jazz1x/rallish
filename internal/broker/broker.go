// Package broker provides the HTTP/SSE server and route handlers for the rallish broker.
package broker

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jazz1x/rallish/internal/budget"
	"github.com/jazz1x/rallish/internal/exit"
	"github.com/jazz1x/rallish/internal/router"
	"github.com/jazz1x/rallish/internal/session"
	"github.com/jazz1x/rallish/pkg/contract"
)

// Server is the HTTP/SSE broker.
type Server struct {
	mux         *http.ServeMux
	store       *session.Store
	budgeter    *budget.Budgeter
	pollTimeout time.Duration

	mu            sync.Mutex
	pendingReqs   map[string]contract.TurnRequest
	sessionStates map[string]sessionState
}

type sessionState struct {
	turnCount      int
	totalUsage     contract.Usage
	lastTurn       *contract.LastTurn
	lastResp       *contract.TurnResponse
	preset         contract.Preset
	router         *router.Router
	createdAt      time.Time
	terminal       bool
	terminalReason string
	notify         chan struct{}
	a2aSubs        []chan contract.A2ATaskUpdateEvent
}

// NewServer creates a new broker Server.
func NewServer(store *session.Store, budgeter *budget.Budgeter) *Server {
	s := &Server{
		mux:           http.NewServeMux(),
		store:         store,
		budgeter:      budgeter,
		pollTimeout:   30 * time.Second,
		pendingReqs:   make(map[string]contract.TurnRequest),
		sessionStates: make(map[string]sessionState),
	}
	s.mux.HandleFunc("POST /sessions", s.handleCreateSession)
	s.mux.HandleFunc("GET /sessions/{id}", s.handleGetSession)
	s.mux.HandleFunc("GET /sessions/{id}/next", s.handleNextTurn)
	s.mux.HandleFunc("POST /sessions/{id}/turn", s.handlePostTurn)
	s.registerA2ARoutes()
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
		createdAt: sess.CreatedAt,
		notify:    make(chan struct{}),
	}
	s.mu.Unlock()

	logger.InfoContext(ctx, "session_created",
		"session_id", sess.ID,
		"preset_name", req.Preset.Name,
		"role_count", len(req.Preset.Roles),
		"repo_root", req.Task.RepoRoot,
	)

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
		logger.InfoContext(ctx, "session not found", "session_id", id)
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
	as := r.URL.Query().Get("as")

	timer := time.NewTimer(s.pollTimeout)
	defer timer.Stop()

	for {
		s.mu.Lock()
		state, ok := s.sessionStates[id]
		if !ok {
			s.mu.Unlock()
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		if state.terminal {
			s.mu.Unlock()
			http.Error(w, fmt.Sprintf("session terminated: %s", state.terminalReason), http.StatusGone)
			return
		}

		remaining := s.budgeter.Remaining(state.preset.Budget, state.totalUsage, state.turnCount)
		if s.budgeter.IsExhausted(remaining) {
			state.terminal = true
			state.terminalReason = "budget exhausted"
			s.sessionStates[id] = state
			s.mu.Unlock()
			http.Error(w, "session terminated: budget exhausted", http.StatusGone)
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

		if as == "" || as == roleID {
			var role contract.Role
			for _, rl := range state.preset.Roles {
				if rl.ID == roleID {
					role = rl
					break
				}
			}

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

			if !timer.Stop() {
				<-timer.C
			}

			logger.InfoContext(ctx, "next_turn",
				"session_id", id,
				"turn", turn,
				"role", roleID,
				"runtime_hint", role.Runtime,
			)

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
			return
		}

		// as does not match — long-poll.
		notify := state.notify
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			logger.InfoContext(ctx, "client disconnected", "session", id)
			return
		case <-notify:
			// State changed (turn posted or terminal set). Re-check.
		case <-timer.C:
			w.WriteHeader(http.StatusNoContent)
			return
		}
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
		logger.InfoContext(ctx, "no pending turn for session", "session_id", id)
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

	logger.InfoContext(ctx, "turn_recorded",
		"session_id", id,
		"turn", req.Turn,
		"role", req.Role,
		"done", resp.Done,
		"handoff_to", resp.HandoffTo,
		"self_eval", resp.SelfEval,
	)

	// Evaluate non-shell exit predicates.
	s.mu.Lock()
	state = s.sessionStates[id]
	remaining := s.budgeter.Remaining(state.preset.Budget, state.totalUsage, state.turnCount)
	evalState := exit.State{
		TurnCount:    state.turnCount,
		Usage:        state.totalUsage,
		Budget:       remaining,
		StartTime:    state.createdAt,
		LastResponse: state.lastResp,
		DeadlineMS:   state.preset.Budget.DeadlineMS,
	}
	matched, reason, evalErr := exit.NewEvaluator(false).Evaluate(ctx, evalState, state.preset.ExitWhen)
	if matched {
		state.terminal = true
		state.terminalReason = reason
	}
	oldNotify := state.notify
	state.notify = make(chan struct{})

	// Notify A2A subscribers under lock to prevent races and double-close.
	if len(state.a2aSubs) > 0 {
		event := contract.A2ATaskUpdateEvent{
			ID: id,
			Status: contract.A2ATaskStatus{
				State:  terminalReasonToTaskState(state.terminal, state.terminalReason),
				Reason: state.terminalReason,
			},
			Final: state.terminal,
		}
		for _, ch := range state.a2aSubs {
			select {
			case ch <- event:
			default:
			}
		}
		if state.terminal {
			for _, ch := range state.a2aSubs {
				close(ch)
			}
			state.a2aSubs = nil
		}
	}
	s.sessionStates[id] = state
	s.mu.Unlock()

	if oldNotify != nil {
		close(oldNotify)
	}

	if evalErr != nil {
		logger.InfoContext(ctx, "exit_predicate_shell_skipped", "session_id", id, "error", evalErr)
	} else if matched {
		logger.InfoContext(ctx, "session_terminal", "session_id", id, "reason", reason, "turn", state.turnCount)
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
