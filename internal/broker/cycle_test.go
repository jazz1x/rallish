package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jazz1x/rallish/internal/adapter"
	"github.com/jazz1x/rallish/internal/adapter/fake"
	"github.com/jazz1x/rallish/internal/budget"
	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/internal/session"
	"github.com/jazz1x/rallish/pkg/contract"
)

func newTestBroker(t *testing.T) *Server {
	t.Helper()
	store, err := session.NewStore(t.TempDir(), &realClock{})
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	b := budget.NewBudgeter(&realClock{})
	return NewServer(store, b)
}

// TestRefreshFromFilePicksUpExternalHalt is the regression guard for the stale
// cycle-status bug: a cycle the daemon cached, then halted out-of-band by a
// separate `cycle run --once` process (a disk write the daemon never saw), must
// be reflected after refreshFromFile — get() alone would serve the stale cache.
func TestRefreshFromFilePicksUpExternalHalt(t *testing.T) {
	dir := t.TempDir()
	cs := newCycleStore()
	cs.baseDir = dir

	id := "cyc_stale_1"
	live, err := cycle.NewState(contract.NewCycleRequest{Goal: "feat: x", MaxCycles: 5}, id)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	live.UpdatedAt = 1000
	cs.put(id, live) // daemon caches the live (not-halted) state

	if got, _ := cs.get(id); got.Halted {
		t.Fatal("precondition: cached state must be live")
	}

	// An external process advances the SAME cycle on disk to halted, newer.
	halted := live
	halted.Halted = true
	halted.HaltReason = contract.HaltStuck
	halted.UpdatedAt = 2000
	if err := cycle.NewStateFileSync(cs.statePath(id)).Write(halted); err != nil {
		t.Fatalf("write halted state: %v", err)
	}

	cs.refreshFromFile(id)
	got, ok := cs.get(id)
	if !ok {
		t.Fatal("cycle vanished after refresh")
	}
	if !got.Halted || got.HaltReason != contract.HaltStuck {
		t.Fatalf("stale status: halted=%v reason=%q, want halted+stuck after external halt", got.Halted, got.HaltReason)
	}
}

// TestRefreshFromFileDoesNotRegressNewerCache is the false-positive guard: an
// OLDER disk snapshot must NOT clobber a newer in-process cache (the daemon's own
// handleStep -> put write), or refresh would undo fresh state.
func TestRefreshFromFileDoesNotRegressNewerCache(t *testing.T) {
	dir := t.TempDir()
	cs := newCycleStore()
	cs.baseDir = dir

	id := "cyc_fresh_1"
	old, err := cycle.NewState(contract.NewCycleRequest{Goal: "feat: x", MaxCycles: 5}, id)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	old.UpdatedAt = 1000
	if err := cycle.NewStateFileSync(cs.statePath(id)).Write(old); err != nil {
		t.Fatalf("write old state: %v", err)
	}

	newer := old
	newer.NextCycleGoal = "feat: newer in-process goal"
	newer.UpdatedAt = 3000
	cs.put(id, newer) // daemon's newer in-process state

	cs.refreshFromFile(id)
	got, _ := cs.get(id)
	if got.NextCycleGoal != "feat: newer in-process goal" {
		t.Fatalf("newer cache regressed to stale disk: goal = %q", got.NextCycleGoal)
	}
}

func TestHandleCreateCycle(t *testing.T) {
	srv := newTestBroker(t)

	reqBody, _ := json.Marshal(contract.NewCycleRequest{
		Goal:       "feat: test create",
		Branch:     "feat/test",
		MaxCycles:  5,
		LocalGates: []string{" go test ./... ", ""},
	})
	req := httptest.NewRequest(http.MethodPost, "/cycles", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	var state contract.CycleState
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if state.NextCycleGoal != "feat: test create" {
		t.Fatalf("goal = %q", state.NextCycleGoal)
	}
	if state.MaxCycles != 5 {
		t.Fatalf("max_cycles = %d, want 5", state.MaxCycles)
	}
	if state.Branch != "feat/test" {
		t.Fatalf("branch = %q", state.Branch)
	}
	if len(state.LocalGates) != 1 || state.LocalGates[0] != "go test ./..." {
		t.Fatalf("local_gates = %v", state.LocalGates)
	}

	ledger := cycle.NewLedgerFileSync("tmp/cycle-" + state.ID + "-ledger.jsonl")
	entries, err := ledger.ReadAll()
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if len(entries) != 1 || entries[0].Type != contract.LedgerEventCycleCreated {
		t.Fatalf("ledger entries = %#v", entries)
	}
}

func TestHandleCreateCycleRejectsMain(t *testing.T) {
	srv := newTestBroker(t)

	reqBody, _ := json.Marshal(contract.NewCycleRequest{Goal: "feat: test", Branch: "main"})
	req := httptest.NewRequest(http.MethodPost, "/cycles", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleGetCycle(t *testing.T) {
	srv := newTestBroker(t)

	// Create a cycle.
	reqBody, _ := json.Marshal(contract.NewCycleRequest{Goal: "feat: test get"})
	req := httptest.NewRequest(http.MethodPost, "/cycles", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var created contract.CycleState
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Get it back.
	req = httptest.NewRequest(http.MethodGet, "/cycles/"+created.ID, nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var fetched contract.CycleState
	if err := json.Unmarshal(rec.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fetched.ID != created.ID {
		t.Fatalf("id mismatch")
	}
}

func TestHandleGetCycleLedger(t *testing.T) {
	srv := newTestBroker(t)

	reqBody, _ := json.Marshal(contract.NewCycleRequest{Goal: "feat: test ledger"})
	req := httptest.NewRequest(http.MethodPost, "/cycles", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var created contract.CycleState
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	req = httptest.NewRequest(http.MethodGet, "/cycles/"+created.ID+"/ledger", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var entries []contract.HarnessLedgerEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 1 || entries[0].Type != contract.LedgerEventCycleCreated {
		t.Fatalf("ledger entries = %#v", entries)
	}
}

func TestHandleGetCycleLedgerAfterHalt(t *testing.T) {
	srv := newTestBroker(t)

	reqBody, _ := json.Marshal(contract.NewCycleRequest{Goal: "feat: test halted ledger"})
	req := httptest.NewRequest(http.MethodPost, "/cycles", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var created contract.CycleState
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	haltBody, _ := json.Marshal(contract.HaltRequest{Reason: "user-requested"})
	req = httptest.NewRequest(http.MethodPost, "/cycles/"+created.ID+"/halt", bytes.NewReader(haltBody))
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("halt status = %d, want 200", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/cycles/"+created.ID+"/ledger", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ledger status = %d, want 200", rec.Code)
	}
	var entries []contract.HarnessLedgerEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 2 || entries[1].Type != contract.LedgerEventCycleHalted {
		t.Fatalf("ledger entries = %#v", entries)
	}
}

func TestHandleGetCycleNotFound(t *testing.T) {
	srv := newTestBroker(t)

	req := httptest.NewRequest(http.MethodGet, "/cycles/nonexistent", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleGetCycleLedgerNotFound(t *testing.T) {
	srv := newTestBroker(t)

	req := httptest.NewRequest(http.MethodGet, "/cycles/nonexistent/ledger", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleHaltCycle(t *testing.T) {
	srv := newTestBroker(t)

	// Create.
	reqBody, _ := json.Marshal(contract.NewCycleRequest{Goal: "feat: test halt"})
	req := httptest.NewRequest(http.MethodPost, "/cycles", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var created contract.CycleState
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Halt.
	haltBody, _ := json.Marshal(contract.HaltRequest{Reason: "user-requested"})
	req = httptest.NewRequest(http.MethodPost, "/cycles/"+created.ID+"/halt", bytes.NewReader(haltBody))
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var halted contract.CycleState
	if err := json.Unmarshal(rec.Body.Bytes(), &halted); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !halted.Halted {
		t.Fatal("expected halted")
	}
	if halted.HaltReason != contract.HaltUserRequested {
		t.Fatalf("reason = %q", halted.HaltReason)
	}
	ledger := cycle.NewLedgerFileSync("tmp/cycle-" + created.ID + "-ledger.jsonl")
	entries, err := ledger.ReadAll()
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("ledger entries = %#v, want create and halt", entries)
	}
	if entries[1].Type != contract.LedgerEventCycleHalted || entries[1].Summary != "user-requested" {
		t.Fatalf("halt ledger entry = %#v", entries[1])
	}

	// After halt, GET should return 404 because the file is removed.
	req = httptest.NewRequest(http.MethodGet, "/cycles/"+created.ID, nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 after halt", rec.Code)
	}
}

// testGate is a broker-test mock for the cycle Gate interface.
type testGate struct{ name string }

func (g testGate) Name() string { return g.name }
func (g testGate) Run(_ context.Context, state cycle.State) (contract.GateResult, cycle.State) {
	return contract.GateSuccess{R: contract.GateReport{Gate: g.name, Passed: true}}, state
}

func TestHandleStepCycleRequiresGoal(t *testing.T) {
	srv := newTestBroker(t)
	// Inject a mock pipeline so we don't need a real git repo.
	srv.SetCyclePipeline(cycle.Pipeline{testGate{name: "mock"}})

	// Create.
	reqBody, _ := json.Marshal(contract.NewCycleRequest{Goal: "feat: test step"})
	req := httptest.NewRequest(http.MethodPost, "/cycles", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var created contract.CycleState
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Step without overriding goal should succeed because the initial goal is present.
	req = httptest.NewRequest(http.MethodPost, "/cycles/"+created.ID+"/step", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("first step status = %d, want 200", rec.Code)
	}
	ledger := cycle.NewLedgerFileSync("tmp/cycle-" + created.ID + "-ledger.jsonl")
	entries, err := ledger.ReadAll()
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	// On a passing step the broker now records, after cycle_created:
	// validation_green (B1 — the verifier-produced progress signal the reviver
	// keys on) → cycle_completed → one gate_passed per report. Before the B1 fix
	// validation_green had no emit site here.
	if len(entries) < 4 {
		t.Fatalf("ledger entries = %#v, want at least create/green/complete/gate", entries)
	}
	if entries[1].Type != contract.LedgerEventValidationGreen {
		t.Fatalf("second ledger entry = %q, want validation_green", entries[1].Type)
	}
	if entries[2].Type != contract.LedgerEventCycleCompleted {
		t.Fatalf("third ledger entry = %q, want cycle_completed", entries[2].Type)
	}
	if entries[3].Type != contract.LedgerEventGatePassed || entries[3].Gate != "mock" {
		t.Fatalf("fourth ledger entry = %#v, want mock gate passed", entries[3])
	}

	// Step again without a new goal should fail because CompleteCycle cleared it.
	req = httptest.NewRequest(http.MethodPost, "/cycles/"+created.ID+"/step", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("second step status = %d, want 400", rec.Code)
	}
}

// TestHandleStepCycleRefusesSealedCycle is the regression guard for the step
// seal-bypass: a cycle the ledger has sealed with a sticky halt (cycle_halted,
// no later validation_green) must NOT be advanced by POST /step — not even with
// no preceding GET to refresh the cache. Before the fix the step ran on the
// stale (un-halted) cache, advanced the cycle, and emitted validation_green —
// the very signal LedgerSealsResume keys on — silently lifting the seal and
// resurrecting the halted cycle. A valid goal is supplied so the expected 409
// can only come from the seal guard, not the empty-goal 400 (false-positive
// guard); TestHandleStepCycleRequiresGoal already pins the un-sealed 200 path.
func TestHandleStepCycleRefusesSealedCycle(t *testing.T) {
	srv := newTestBroker(t)
	srv.SetCyclePipeline(cycle.Pipeline{testGate{name: "mock"}}) // a passing pipeline

	reqBody, _ := json.Marshal(contract.NewCycleRequest{Goal: "feat: sealed step"})
	req := httptest.NewRequest(http.MethodPost, "/cycles", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", rec.Code)
	}
	var created contract.CycleState
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}

	// Seal it out-of-band: append cycle_halted straight to the ledger, exactly as
	// an external `cycle run --once` halt would, with NO GET after — so the
	// broker's in-memory cache still reads halted=false.
	ledger := cycle.NewLedgerFileSync("tmp/cycle-" + created.ID + "-ledger.jsonl")
	if err := ledger.Append(contract.NewHarnessLedgerEntry(
		time.Now().UnixMilli(), created.ID, contract.LedgerEventCycleHalted, "stuck", nil)); err != nil {
		t.Fatalf("seal ledger: %v", err)
	}
	if entries, err := ledger.ReadAll(); err != nil {
		t.Fatalf("read ledger: %v", err)
	} else if _, sealed := cycle.LedgerSealsResume(entries); !sealed {
		t.Fatal("precondition: ledger must be sealed after the cycle_halted append")
	}

	// Step with a valid goal and no preceding GET → must be refused 409.
	req = httptest.NewRequest(http.MethodPost, "/cycles/"+created.ID+"/step",
		bytes.NewReader([]byte(`{"goal":"feat: try resume"}`)))
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("sealed step status = %d, want 409 (resume refused)", rec.Code)
	}

	// The seal must still hold: the refused step must not have appended
	// validation_green (which would lift it) or any cycle_completed.
	entries, err := ledger.ReadAll()
	if err != nil {
		t.Fatalf("read ledger after step: %v", err)
	}
	if _, sealed := cycle.LedgerSealsResume(entries); !sealed {
		t.Fatal("step must not have lifted the seal (no validation_green after the halt)")
	}
	for _, e := range entries {
		if e.Type == contract.LedgerEventValidationGreen || e.Type == contract.LedgerEventCycleCompleted {
			t.Fatalf("refused step must append no progress entries, found %q", e.Type)
		}
	}
}

func TestHandleOrchestrateNoRegistry(t *testing.T) {
	srv := newTestBroker(t)

	// Create.
	reqBody, _ := json.Marshal(contract.NewCycleRequest{Goal: "feat: test orch"})
	req := httptest.NewRequest(http.MethodPost, "/cycles", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var created contract.CycleState
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Orchestrate without registry.
	orchBody, _ := json.Marshal(contract.OrchestratorConfig{Agents: []string{"fake"}})
	req = httptest.NewRequest(http.MethodPost, "/cycles/"+created.ID+"/orchestrate", bytes.NewReader(orchBody))
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestHandleCycleEvents(t *testing.T) {
	srv := newTestBroker(t)

	// Create.
	reqBody, _ := json.Marshal(contract.NewCycleRequest{Goal: "feat: test events"})
	req := httptest.NewRequest(http.MethodPost, "/cycles", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var created contract.CycleState
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// SSE endpoint.
	req = httptest.NewRequest(http.MethodGet, "/cycles/"+created.ID+"/events", nil)
	rec = httptest.NewRecorder()

	// Use a context with timeout so the SSE loop doesn't block forever.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !bytes.Contains([]byte(body), []byte("data:")) {
		t.Fatalf("expected SSE data line, got: %s", body)
	}
}

func TestCycleStoreGetFileFallback(t *testing.T) {
	srv := newTestBroker(t)

	// Create a cycle.
	reqBody, _ := json.Marshal(contract.NewCycleRequest{Goal: "feat: test fallback"})
	req := httptest.NewRequest(http.MethodPost, "/cycles", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var created contract.CycleState
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Simulate daemon restart by creating a fresh store.
	srv.cycleStore = newCycleStore()

	// GET should still find the cycle via file fallback.
	req = httptest.NewRequest(http.MethodGet, "/cycles/"+created.ID, nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after store reset", rec.Code)
	}
}

func TestCycleEventLastFailedGate(t *testing.T) {
	srv := newTestBroker(t)

	// Create a cycle on a branch that preflight will reject (main).
	reqBody, _ := json.Marshal(contract.NewCycleRequest{Goal: "feat: test", Branch: "main"})
	req := httptest.NewRequest(http.MethodPost, "/cycles", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPipelineForStateInsertsLocalGatesAfterAudit(t *testing.T) {
	srv := newTestBroker(t)
	state, err := cycle.NewState(contract.NewCycleRequest{
		Goal:       "feat: test local gates",
		LocalGates: []string{"go test ./...", "go vet ./..."},
	}, "cyc_local_gates")
	if err != nil {
		t.Fatalf("new state: %v", err)
	}

	names := srv.pipelineForState(state).Names()
	want := []string{
		"preflight",
		"audit",
		"cmd:go test ./...",
		"cmd:go vet ./...",
		"philosophy",
		"polish",
		"commit",
	}
	if len(names) != len(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
}

func TestBrokerOrchestrateEndToEnd(t *testing.T) {
	srv := newTestBroker(t)

	// Inject a mock pipeline so the cycle steps succeed instantly.
	srv.SetCyclePipeline(cycle.Pipeline{testGate{name: "mock-ok"}})
	srv.SetCycleSleeper(cycle.NoOpSleeper{})

	// Register fake adapters.
	reg := adapter.NewRegistry()
	if err := reg.Register("alpha", fake.New(func(_ int) contract.TurnResponse {
		return contract.TurnResponse{Done: false, Summary: `{"next_goal":"alpha-goal"}`}
	})); err != nil {
		t.Fatalf("register alpha: %v", err)
	}
	if err := reg.Register("beta", fake.New(func(_ int) contract.TurnResponse {
		return contract.TurnResponse{Done: false, Summary: `{"next_goal":"beta-goal"}`}
	})); err != nil {
		t.Fatalf("register beta: %v", err)
	}
	srv.SetAdapterRegistry(reg)

	// Create a cycle with max 4 cycles.
	reqBody, _ := json.Marshal(contract.NewCycleRequest{Goal: "feat: e2e", MaxCycles: 4})
	req := httptest.NewRequest(http.MethodPost, "/cycles", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var created contract.CycleState
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Start orchestration.
	orchBody, _ := json.Marshal(contract.OrchestratorConfig{
		Agents:     []string{"alpha", "beta"},
		ResetEvery: 2,
		WorkingDir: t.TempDir(),
	})
	req = httptest.NewRequest(http.MethodPost, "/cycles/"+created.ID+"/orchestrate", bytes.NewReader(orchBody))
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("orchestrate status = %d, want 202", rec.Code)
	}

	// Poll the cycle state until it completes or times out.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	var final contract.CycleState
	poll := func() bool {
		// Force refresh from disk because the orchestrator goroutine only writes to file.
		srv.cycleStore.refreshFromFile(created.ID)
		req = httptest.NewRequest(http.MethodGet, "/cycles/"+created.ID, nil)
		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			return false
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &final)
		return final.CompletedCycles >= 4
	}

	for !poll() {
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for cycle completion; final cycles = %d", final.CompletedCycles)
		case <-ticker.C:
		}
	}

	if final.CompletedCycles != 4 {
		t.Fatalf("completed_cycles = %d, want 4", final.CompletedCycles)
	}
	if final.Halted {
		t.Fatal("expected not halted after max cycles")
	}
}

// TestCycleSyncsMapConcurrentAccess drives the real cycle handlers concurrently
// to assert the syncs map is never touched outside cs.mu. handleCreateCycle
// writes syncs (putSync) while handleGetCycle on a memory-miss id makes cs.get()
// mutate the same map under the lock. Run under -race this would report
// `DATA RACE` (and risk `fatal error: concurrent map writes`) if any handler
// reached s.cycleStore.syncs directly instead of via the locked accessors.
func TestCycleSyncsMapConcurrentAccess(t *testing.T) {
	srv := newTestBroker(t)
	srv.SetCyclesDir(t.TempDir())

	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers * 2)

	for i := 0; i < workers; i++ {
		// Writer: POST /cycles -> handleCreateCycle -> putSync(id, sync).
		go func(n int) {
			defer wg.Done()
			body, _ := json.Marshal(contract.NewCycleRequest{
				Goal:      "feat: race writer",
				Branch:    fmt.Sprintf("feat/race-%d", n),
				MaxCycles: 1,
			})
			req := httptest.NewRequest(http.MethodPost, "/cycles", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
		}(i)

		// Reader: GET /cycles/{miss} -> handleGetCycle -> cs.get() mutates syncs.
		// Unknown ids force the memory-miss filesystem path that writes the map
		// under cs.mu, racing the writers above on the shared map header.
		go func(n int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/cycles/miss_%d", n), nil)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
		}(i)
	}
	wg.Wait()

	// False-positive guard: the locked accessors must not have broken the happy
	// path. A fresh create still returns 201, and the sync it registers must be
	// retrievable via getSync (proving putSync actually stored it, not that the
	// race merely disappeared because writes were dropped).
	body, _ := json.Marshal(contract.NewCycleRequest{
		Goal:      "feat: guard",
		Branch:    "feat/guard",
		MaxCycles: 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/cycles", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("guard create status = %d, want 201", rec.Code)
	}
	var created contract.CycleState
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("guard unmarshal: %v", err)
	}
	if _, ok := srv.cycleStore.getSync(created.ID); !ok {
		t.Fatalf("getSync(%q) = false, want registered sync after create", created.ID)
	}
}

// realClock satisfies the session.Clock interface used by NewStore.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
