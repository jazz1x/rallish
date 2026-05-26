package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestHandleGetCycleNotFound(t *testing.T) {
	srv := newTestBroker(t)

	req := httptest.NewRequest(http.MethodGet, "/cycles/nonexistent", nil)
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

	// Step again without a new goal should fail because CompleteCycle cleared it.
	req = httptest.NewRequest(http.MethodPost, "/cycles/"+created.ID+"/step", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("second step status = %d, want 400", rec.Code)
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

// realClock satisfies the session.Clock interface used by NewStore.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
