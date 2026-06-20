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
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/internal/cycle/gates"
	"github.com/jazz1x/rallish/pkg/contract"
)

// defaultCyclesDir is the relative fallback used when no cycles directory is
// injected (tests, and any caller that never calls SetCyclesDir). The daemon
// injects an absolute ~/.rallish/cycles path so that cycle state lives OUTSIDE
// the worked-on repo — otherwise the broker would dirty the very working tree
// PreflightGate requires clean, and the path would be coupled to the daemon's
// CWD (a cron `cycle run --once` started elsewhere could not find it).
const defaultCyclesDir = "tmp"

// cycleStore holds active cycle sessions in memory, backed by file sync.
type cycleStore struct {
	mu            sync.Mutex
	baseDir       string // directory holding cycle-<id>.json / -ledger.jsonl (SSOT for cycle paths)
	states        map[string]cycle.State
	syncs         map[string]*cycle.StateFileSync
	events        map[string][]chan contract.CycleEvent
	orchestrating map[string]bool // guards against duplicate orchestrate goroutines
}

func newCycleStore() *cycleStore {
	return &cycleStore{
		baseDir:       defaultCyclesDir,
		states:        make(map[string]cycle.State),
		syncs:         make(map[string]*cycle.StateFileSync),
		events:        make(map[string][]chan contract.CycleEvent),
		orchestrating: make(map[string]bool),
	}
}

// statePath returns the on-disk path of a cycle's state file. SSOT — every
// reader/writer of cycle state goes through this so the directory is configured
// in exactly one place.
func (cs *cycleStore) statePath(id string) string {
	return filepath.Join(cs.dir(), fmt.Sprintf("cycle-%s.json", id))
}

// ledgerPath returns the on-disk path of a cycle's append-only ledger.
func (cs *cycleStore) ledgerPath(id string) string {
	return filepath.Join(cs.dir(), fmt.Sprintf("cycle-%s-ledger.jsonl", id))
}

// dir returns the configured cycles directory, falling back to the relative
// default when unset (zero-value store in tests).
func (cs *cycleStore) dir() string {
	if cs.baseDir == "" {
		return defaultCyclesDir
	}
	return cs.baseDir
}

// get returns the cycle state from memory, falling back to the file system.
// It also registers the sync in the store on successful file fallback.
func (cs *cycleStore) get(id string) (cycle.State, bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	s, ok := cs.states[id]
	if ok {
		return s, true
	}
	// Memory miss — try filesystem.
	fs, err := loadCycleFromFile(cs.statePath(id))
	if err != nil || fs.ID == "" {
		return cycle.State{}, false
	}
	cs.states[id] = fs
	if cs.syncs[id] == nil {
		cs.syncs[id] = cycle.NewStateFileSync(cs.statePath(id))
	}
	return fs, true
}

func (cs *cycleStore) put(id string, state cycle.State) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.states[id] = state
}

// getSync returns the registered StateFileSync for a cycle, if any. Locked
// because the syncs map is also mutated under cs.mu by get/refreshFromFile/closeAll.
func (cs *cycleStore) getSync(id string) (*cycle.StateFileSync, bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	sync, ok := cs.syncs[id]
	return sync, ok
}

// putSync registers a StateFileSync for a cycle. Mirrors put(): every syncs
// map access goes through cs.mu so the daemon's per-request goroutines never
// write the map concurrently.
func (cs *cycleStore) putSync(id string, sync *cycle.StateFileSync) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.syncs[id] = sync
}

// refreshFromFile reconciles the in-memory cache with the on-disk state when an
// EXTERNAL writer (a separate `cycle run --once` process) has advanced or halted
// the cycle. Without this, get() serves whatever the daemon last cached, so a
// cycle halted out-of-band keeps reporting halted=false forever (a stale-status
// bug). It overwrites the cache only when the disk copy is at least as fresh
// (UpdatedAt >= cached), so the daemon's own newer in-process writes (handleStep
// -> put) are never clobbered by a slightly older disk snapshot.
func (cs *cycleStore) refreshFromFile(id string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	fs, err := loadCycleFromFile(cs.statePath(id))
	if err != nil || fs.ID == "" {
		return
	}
	if cached, ok := cs.states[id]; ok && fs.UpdatedAt < cached.UpdatedAt {
		return // cache is newer (in-process write) — do not regress to a stale disk copy
	}
	cs.states[id] = fs
	if cs.syncs[id] == nil {
		cs.syncs[id] = cycle.NewStateFileSync(cs.statePath(id))
	}
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
	if s.cyclePipeline == nil {
		s.cyclePipeline = gates.StandardPipeline("", "", nil)
	}
	s.mux.HandleFunc("POST /cycles", s.handleCreateCycle)
	s.mux.HandleFunc("GET /cycles/{id}", s.handleGetCycle)
	s.mux.HandleFunc("GET /cycles/{id}/ledger", s.handleCycleLedger)
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

	sync := cycle.NewStateFileSync(s.cycleStore.statePath(id))
	if err := sync.Write(state); err != nil {
		logger.ErrorContext(ctx, "write initial state", "error", err)
		http.Error(w, fmt.Sprintf("write state: %v", err), http.StatusInternalServerError)
		return
	}

	s.cycleStore.put(id, state)
	s.cycleStore.putSync(id, sync)
	s.appendLedger(ctx, id, contract.NewHarnessLedgerEntry(
		time.Now().UnixMilli(),
		id,
		contract.LedgerEventCycleCreated,
		"cycle created",
		state.PendingFiles,
	))

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
	// Reconcile with disk first: a separate `cycle run --once` process may have
	// advanced or halted this cycle out-of-band, and get() would otherwise serve
	// a stale cached state (e.g. halted=false on an already-halted cycle).
	s.cycleStore.refreshFromFile(id)
	state, ok := s.cycleStore.get(id)
	if !ok {
		logger.InfoContext(ctx, "cycle not found", "cycle_id", id)
		http.Error(w, "cycle not found", http.StatusNotFound)
		return
	}

	// Limit response bloat: cap history to the most recent 50 reports.
	respState := state.CycleState
	if len(respState.History) > 50 {
		respState.History = respState.History[len(respState.History)-50:]
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(respState); err != nil {
		logger.ErrorContext(ctx, "encode response", "error", err)
	}
}

func (s *Server) handleCycleLedger(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "cycle_ledger")

	id := r.PathValue("id")
	entries, err := cycle.NewLedgerFileSync(s.cycleStore.ledgerPath(id)).ReadAll()
	if err != nil {
		logger.ErrorContext(ctx, "read ledger", "cycle_id", id, "error", err)
		http.Error(w, fmt.Sprintf("read ledger: %v", err), http.StatusInternalServerError)
		return
	}
	if len(entries) == 0 {
		if _, ok := s.cycleStore.get(id); !ok {
			http.Error(w, "cycle ledger not found", http.StatusNotFound)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(entries); err != nil {
		logger.ErrorContext(ctx, "encode response", "error", err)
	}
}

func (s *Server) handleStepCycle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := slog.With("handler", "step_cycle")

	id := r.PathValue("id")
	// Reconcile with disk first: a separate `cycle run --once` may have halted or
	// advanced this cycle out-of-band. Without this, step runs on a stale cached
	// state and writes it back — overwriting an on-disk halt, and (worse) emitting
	// validation_green below, the very signal LedgerSealsResume keys on, which
	// would lift the sticky seal. Mirrors handleGetCycle.
	s.cycleStore.refreshFromFile(id)
	state, ok := s.cycleStore.get(id)
	if !ok {
		http.Error(w, "cycle not found", http.StatusNotFound)
		return
	}

	// Reviver guard (G5): refuse to advance a cycle the ledger has sealed with a
	// sticky halt — the same 409 handleOrchestrate and `cycle run --once` return.
	// Without it, a step with no preceding GET would resurrect a sealed cycle.
	ledgerEntries, lerr := cycle.NewLedgerFileSync(s.cycleStore.ledgerPath(id)).ReadAll()
	if lerr != nil {
		logger.ErrorContext(ctx, "step read ledger", "cycle_id", id, "error", lerr)
		http.Error(w, fmt.Sprintf("read ledger: %v", lerr), http.StatusInternalServerError)
		return
	}
	if reason, sealed := cycle.LedgerSealsResume(ledgerEntries); sealed {
		logger.InfoContext(ctx, "step refused: cycle sealed by halt", "cycle_id", id, "halt_reason", reason)
		http.Error(w, fmt.Sprintf("cycle halted (%s); resume refused", reason), http.StatusConflict)
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

	result := s.pipelineForState(state).Execute(ctx, state)

	if result.IsSuccess() {
		st := result.Value()
		completed := st.CompleteCycle()
		state = completed.Value()
		state.NextCycleGoal = ""
		// VERIFIER-produced progress signal (B1): the gate pipeline passed, so
		// record validation_green — the un-gameable signal the reviver keys on to
		// lift a sticky cycle_halted seal (LedgerSealsResume) — alongside the
		// cycle_completed terminal marker. The per-gate gate_passed events are
		// appended from result.Reports() below.
		s.appendLedger(ctx, id, contract.NewHarnessLedgerEntry(
			time.Now().UnixMilli(),
			id,
			contract.LedgerEventValidationGreen,
			"gate pipeline passed",
			nil,
		))
		s.appendLedger(ctx, id, contract.NewHarnessLedgerEntry(
			time.Now().UnixMilli(),
			id,
			contract.LedgerEventCycleCompleted,
			"cycle step completed",
			nil,
		))
	} else {
		state = result.Value()
	}
	for _, report := range result.Reports() {
		s.appendLedger(ctx, id, contract.NewGateLedgerEntry(time.Now().UnixMilli(), id, report))
	}

	if sync, ok := s.cycleStore.getSync(id); ok {
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
	if sync, ok := s.cycleStore.getSync(id); ok {
		_ = sync.Write(halted)
		_ = sync.Remove() // prevent zombie resurrection after halt
	}
	s.appendLedger(ctx, id, contract.NewHarnessLedgerEntry(
		time.Now().UnixMilli(),
		id,
		contract.LedgerEventCycleHalted,
		string(reason),
		nil,
	))
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

	sync, ok := s.cycleStore.getSync(id)
	if !ok {
		sync = cycle.NewStateFileSync(s.cycleStore.statePath(id))
		s.cycleStore.putSync(id, sync)
	}

	if s.adapterRegistry == nil {
		logger.ErrorContext(ctx, "orchestrate refused: no adapter registry configured")
		http.Error(w, "orchestration unavailable: adapter registry not configured", http.StatusServiceUnavailable)
		return
	}

	// Reviver guard (G5): refuse to (re)start a cycle the ledger has already
	// sealed with a sticky halt. The orchestrator enforces the same rule, but
	// returning 409 here avoids spawning a goroutine that would only halt.
	ledgerEntries, lerr := cycle.NewLedgerFileSync(s.cycleStore.ledgerPath(id)).ReadAll()
	if lerr != nil {
		logger.ErrorContext(ctx, "orchestrate read ledger", "cycle_id", id, "error", lerr)
		http.Error(w, fmt.Sprintf("read ledger: %v", lerr), http.StatusInternalServerError)
		return
	}
	if reason, sealed := cycle.LedgerSealsResume(ledgerEntries); sealed {
		logger.InfoContext(ctx, "orchestrate refused: cycle sealed by halt", "cycle_id", id, "halt_reason", reason)
		http.Error(w, fmt.Sprintf("cycle halted (%s); resume refused", reason), http.StatusConflict)
		return
	}

	s.cycleStore.mu.Lock()
	if s.cycleStore.orchestrating[id] {
		s.cycleStore.mu.Unlock()
		http.Error(w, "orchestration already running for this cycle", http.StatusConflict)
		return
	}
	s.cycleStore.orchestrating[id] = true
	s.cycleStore.mu.Unlock()

	orch := cycle.NewMultiAgentOrchestrator(s.adapterRegistry, sync)
	orch.SetLedger(cycle.NewLedgerFileSync(s.cycleStore.ledgerPath(id)))
	orch.SetPipelineFactory(s.pipelineForState)
	if s.cycleSleeper != nil {
		orch.SetDriverSleeper(s.cycleSleeper)
	}

	// Run orchestration asynchronously so the HTTP request returns immediately.
	go func() {
		defer func() {
			s.cycleStore.mu.Lock()
			delete(s.cycleStore.orchestrating, id)
			s.cycleStore.mu.Unlock()
		}()
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

// loadCycleFromFile reads a cycle state from an explicit path (the cycleStore
// owns path construction so the directory is configured in one place).
func loadCycleFromFile(statePath string) (cycle.State, error) {
	sync := cycle.NewStateFileSync(statePath)
	return sync.Read()
}

func (s *Server) appendLedger(ctx context.Context, id string, entry contract.HarnessLedgerEntry) {
	ledger := cycle.NewLedgerFileSync(s.cycleStore.ledgerPath(id))
	if err := ledger.Append(entry); err != nil {
		slog.With("component", "cycle_ledger").WarnContext(ctx, "append ledger", "cycle_id", id, "error", err)
	}
}

func (s *Server) pipelineForState(state cycle.State) cycle.Pipeline {
	// Always build a fresh base incorporating the state's AuditCmd and PolishTestCmd
	// so that per-cycle overrides are respected even when s.cyclePipeline is pre-set
	// with empty overrides (the default path). If the server has a custom
	// cyclePipeline injected (tests / future extension), honour it but only when the
	// state carries no overrides. The canonical order and local-gate insertion are
	// owned by gates.StandardPipeline (shared with the CLI one-shot) — do not
	// re-implement them here.
	if state.AuditCmd != "" || state.PolishTestCmd != "" || s.cyclePipeline == nil {
		return gates.StandardPipeline(state.AuditCmd, state.PolishTestCmd, state.LocalGates)
	}

	base := s.cyclePipeline
	if len(state.LocalGates) == 0 {
		return base
	}

	pipeline := make(cycle.Pipeline, 0, len(base)+len(state.LocalGates))
	for _, gate := range base {
		pipeline = append(pipeline, gate)
		if gate.Name() != "audit" {
			continue
		}
		for _, rawCmd := range state.LocalGates {
			pipeline = append(pipeline, gates.CommandGate{RawCmd: rawCmd})
		}
	}
	return pipeline
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based hex on crypto failure (extremely unlikely).
		return fmt.Sprintf("%x", time.Now().UnixNano())[:n*2]
	}
	return hex.EncodeToString(b)
}
