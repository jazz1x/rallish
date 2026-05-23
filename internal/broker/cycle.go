// Package broker provides the HTTP/SSE server and route handlers for the rallish broker.
//
// This file implements the autonomous-cycle subsystem endpoints.
package broker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/internal/cycle/gates"
	"github.com/jazz1x/rallish/pkg/contract"
)

// cycleStore holds active cycle sessions in memory, backed by file sync.
type cycleStore struct {
	mu     sync.Mutex
	states map[string]cycle.State
	syncs  map[string]*cycle.StateFileSync
	events map[string][]chan contract.CycleEvent
}

func newCycleStore() *cycleStore {
	return &cycleStore{
		states: make(map[string]cycle.State),
		syncs:  make(map[string]*cycle.StateFileSync),
		events: make(map[string][]chan contract.CycleEvent),
	}
}

func (cs *cycleStore) get(id string) (cycle.State, bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	s, ok := cs.states[id]
	return s, ok
}

func (cs *cycleStore) put(id string, state cycle.State) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.states[id] = state
}

func (cs *cycleStore) subscribe(id string) chan contract.CycleEvent {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	ch := make(chan contract.CycleEvent, 4)
	cs.events[id] = append(cs.events[id], ch)
	return ch
}

func (cs *cycleStore) unsubscribe(id string, ch chan contract.CycleEvent) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	var filtered []chan contract.CycleEvent
	for _, c := range cs.events[id] {
		if c != ch {
			filtered = append(filtered, c)
		}
	}
	cs.events[id] = filtered
	close(ch)
}

func (cs *cycleStore) broadcast(id string, evt contract.CycleEvent) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, ch := range cs.events[id] {
		select {
		case ch <- evt:
		default:
		}
	}
}

func (cs *cycleStore) closeAll(id string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, ch := range cs.events[id] {
		close(ch)
	}
	delete(cs.events, id)
	delete(cs.states, id)
	delete(cs.syncs, id)
}

func (s *Server) registerCycleRoutes() {
	s.cycleStore = newCycleStore()
	s.mux.HandleFunc("POST /cycles", s.handleCreateCycle)
	s.mux.HandleFunc("GET /cycles/{id}", s.handleGetCycle)
	s.mux.HandleFunc("POST /cycles/{id}/step", s.handleStepCycle)
	s.mux.HandleFunc("POST /cycles/{id}/halt", s.handleHaltCycle)
	s.mux.HandleFunc("GET /cycles/{id}/events", s.handleCycleEvents)
	s.mux.HandleFunc("POST /cycles/{id}/orchestrate", s.handleOrchestrate)
}

func (s *Server) handleCreateCycle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "create_cycle")

	var req contract.NewCycleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.ErrorContext(ctx, "decode request", "error", err)
		http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
		return
	}

	id := fmt.Sprintf("cyc_%d_%s", time.Now().UnixMilli(), randomHex(4))
	state, err := cycle.NewState(req, id)
	if err != nil {
		logger.InfoContext(ctx, "invalid cycle request", "error", err)
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	sync := cycle.NewStateFileSync(fmt.Sprintf("tmp/cycle-%s.json", id))
	if err := sync.Write(state); err != nil {
		logger.ErrorContext(ctx, "write initial state", "error", err)
		http.Error(w, fmt.Sprintf("write state: %v", err), http.StatusInternalServerError)
		return
	}

	s.cycleStore.put(id, state)
	s.cycleStore.syncs[id] = sync

	logger.InfoContext(ctx, "cycle_created", "cycle_id", id, "branch", state.Branch, "max_cycles", state.MaxCycles)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(state.CycleState); err != nil {
		logger.ErrorContext(ctx, "encode response", "error", err)
	}
}

func (s *Server) handleGetCycle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "get_cycle")

	id := r.PathValue("id")
	state, ok := s.cycleStore.get(id)
	if !ok {
		if fs, err := loadCycleFromFile(id); err == nil {
			state = fs
			s.cycleStore.put(id, state)
			s.cycleStore.syncs[id] = cycle.NewStateFileSync(fmt.Sprintf("tmp/cycle-%s.json", id))
			ok = true
		}
	}
	if !ok {
		logger.InfoContext(ctx, "cycle not found", "cycle_id", id)
		http.Error(w, "cycle not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(state.CycleState); err != nil {
		logger.ErrorContext(ctx, "encode response", "error", err)
	}
}

func (s *Server) handleStepCycle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "step_cycle")

	id := r.PathValue("id")
	state, ok := s.cycleStore.get(id)
	if !ok {
		if fs, err := loadCycleFromFile(id); err == nil {
			state = fs
			s.cycleStore.put(id, state)
			ok = true
		}
	}
	if !ok {
		http.Error(w, "cycle not found", http.StatusNotFound)
		return
	}

	var req contract.StepRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Goal != "" {
		state.NextCycleGoal = req.Goal
	}
	if strings.TrimSpace(state.NextCycleGoal) == "" {
		http.Error(w, "next_cycle_goal is empty", http.StatusBadRequest)
		return
	}

	pipeline := buildStandardPipeline()
	result := pipeline.Execute(ctx, state)

	if result.IsSuccess() {
		st := result.Value()
		completed := st.CompleteCycle()
		state = completed.Value()
		state.NextCycleGoal = ""
	} else {
		state = result.Value()
	}

	if sync, ok := s.cycleStore.syncs[id]; ok {
		_ = sync.Write(state)
	}
	s.cycleStore.put(id, state)
	s.cycleStore.broadcast(id, contract.NewCycleEvent(&state.CycleState))

	if result.IsFailure() {
		logger.InfoContext(ctx, "cycle_step_failed", "cycle_id", id, "error", result.Err())
	} else {
		logger.InfoContext(ctx, "cycle_step_completed", "cycle_id", id, "completed_cycles", state.CompletedCycles)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(state.CycleState); err != nil {
		logger.ErrorContext(ctx, "encode response", "error", err)
	}
}

func (s *Server) handleHaltCycle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "halt_cycle")

	id := r.PathValue("id")
	state, ok := s.cycleStore.get(id)
	if !ok {
		if fs, err := loadCycleFromFile(id); err == nil {
			state = fs
			s.cycleStore.put(id, state)
			ok = true
		}
	}
	if !ok {
		http.Error(w, "cycle not found", http.StatusNotFound)
		return
	}

	var req contract.HaltRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	reason := contract.HaltUserRequested
	if req.Reason != "" {
		if parsed, err := contract.ParseHaltReason(req.Reason); err == nil {
			reason = parsed
		}
	}

	halted := state.Halt(reason).Value()
	if sync, ok := s.cycleStore.syncs[id]; ok {
		_ = sync.Write(halted)
	}
	s.cycleStore.put(id, halted)
	s.cycleStore.broadcast(id, contract.NewCycleEvent(&halted.CycleState))
	s.cycleStore.closeAll(id)

	logger.InfoContext(ctx, "cycle_halted", "cycle_id", id, "reason", reason)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(halted.CycleState); err != nil {
		logger.ErrorContext(ctx, "encode response", "error", err)
	}
}

func (s *Server) handleCycleEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "cycle_events")

	id := r.PathValue("id")
	state, ok := s.cycleStore.get(id)
	if !ok {
		http.Error(w, "cycle not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// Send current state immediately.
	evt := contract.NewCycleEvent(&state.CycleState)
	data, _ := json.Marshal(evt)
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		logger.ErrorContext(ctx, "write sse", "error", err)
		return
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	ch := s.cycleStore.subscribe(id)
	defer s.cycleStore.unsubscribe(id, ch)

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(evt)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				logger.ErrorContext(ctx, "write sse", "error", err)
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			if evt.Halted {
				return
			}
		}
	}
}

func (s *Server) handleOrchestrate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "orchestrate")

	id := r.PathValue("id")
	state, ok := s.cycleStore.get(id)
	if !ok {
		http.Error(w, "cycle not found", http.StatusNotFound)
		return
	}

	var cfg contract.OrchestratorConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		logger.ErrorContext(ctx, "decode request", "error", err)
		http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
		return
	}

	sync, ok := s.cycleStore.syncs[id]
	if !ok {
		sync = cycle.NewStateFileSync(fmt.Sprintf("tmp/cycle-%s.json", id))
		s.cycleStore.syncs[id] = sync
	}

	if s.adapterRegistry == nil {
		logger.ErrorContext(ctx, "orchestrate refused: no adapter registry configured")
		http.Error(w, "orchestration unavailable: adapter registry not configured", http.StatusServiceUnavailable)
		return
	}
	orch := cycle.NewMultiAgentOrchestrator(s.adapterRegistry, sync)
	orch.SetDriverPipeline(buildStandardPipeline())

	// Run orchestration asynchronously so the HTTP request returns immediately.
	go func() {
		if err := orch.Run(context.WithoutCancel(ctx), cfg); err != nil {
			logger.ErrorContext(ctx, "orchestration failed", "cycle_id", id, "error", err)
		}
	}()

	logger.InfoContext(ctx, "orchestration_started", "cycle_id", id, "agents", cfg.Agents)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(state.CycleState); err != nil {
		logger.ErrorContext(ctx, "encode response", "error", err)
	}
}

func loadCycleFromFile(id string) (cycle.State, error) {
	sync := cycle.NewStateFileSync(fmt.Sprintf("tmp/cycle-%s.json", id))
	return sync.Read()
}

func buildStandardPipeline() cycle.Pipeline {
	return cycle.Pipeline{
		gates.PreflightGate{},
		gates.AuditGate{},
		gates.PhilosophyGate{},
		gates.PolishGate{},
		gates.CommitGate{},
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based hex on crypto failure (extremely unlikely).
		return fmt.Sprintf("%x", time.Now().UnixNano())[:n*2]
	}
	return hex.EncodeToString(b)
}
