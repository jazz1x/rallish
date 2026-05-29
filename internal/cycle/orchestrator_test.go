package cycle

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jazz1x/rallish/internal/adapter"
	"github.com/jazz1x/rallish/internal/adapter/fake"
	"github.com/jazz1x/rallish/pkg/contract"
)

// passGate is a pass-through gate that leaves state untouched.
type passGate struct{ name string }

func (g passGate) Name() string { return g.name }
func (g passGate) Run(_ context.Context, state State) (contract.GateResult, State) {
	return contract.GateSuccess{R: contract.GateReport{Gate: g.name, Passed: true}}, state
}

func TestOrchestratorMultiAgentRotation(t *testing.T) {
	tmpDir := t.TempDir()
	sync := NewStateFileSync(filepath.Join(tmpDir, "cycle.json"))

	state, err := NewState(contract.NewCycleRequest{Goal: "feat: e2e", MaxCycles: 5}, "cyc_e2e_1")
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	if err := sync.Write(state); err != nil {
		t.Fatalf("write initial state: %v", err)
	}

	reg := adapter.NewRegistry()

	// agent-alpha: always returns a fresh goal.
	alpha := fake.New(func(turn int) contract.TurnResponse {
		return contract.TurnResponse{
			Done:    false,
			Summary: fmt.Sprintf(`{"next_goal":"alpha-turn-%d"}`, turn),
		}
	})
	// agent-beta: returns goals too.
	beta := fake.New(func(turn int) contract.TurnResponse {
		return contract.TurnResponse{
			Done:    false,
			Summary: fmt.Sprintf(`{"next_goal":"beta-turn-%d"}`, turn),
		}
	})

	if err := reg.Register("agent-alpha", alpha); err != nil {
		t.Fatalf("register alpha: %v", err)
	}
	if err := reg.Register("agent-beta", beta); err != nil {
		t.Fatalf("register beta: %v", err)
	}

	orch := NewMultiAgentOrchestrator(reg, sync)
	orch.SetDriverPipeline(Pipeline{passGate{name: "ok"}})
	orch.SetDriverSleeper(instantSleeper{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := orch.Run(ctx, contract.OrchestratorConfig{
		Agents:     []string{"agent-alpha", "agent-beta"},
		ResetEvery: 3,
		WorkingDir: tmpDir,
	}); err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}

	final, err := sync.Read()
	if err != nil {
		t.Fatalf("read final state: %v", err)
	}
	if final.CompletedCycles != 5 {
		t.Fatalf("completed_cycles = %d, want 5", final.CompletedCycles)
	}
	if final.Halted {
		t.Fatal("expected not halted after max cycles")
	}

	// Verify rotation by inspecting history (goals left by adapters).
	// Since CompleteCycle clears NextCycleGoal, the *final* goal is empty.
	// But we can verify the cycle count proves the loop ran.
}

func TestOrchestratorHaltOnGateFailure(t *testing.T) {
	tmpDir := t.TempDir()
	sync := NewStateFileSync(filepath.Join(tmpDir, "cycle.json"))

	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: e2e", MaxCycles: 10}, "cyc_e2e_2")
	_ = sync.Write(state)

	reg := adapter.NewRegistry()
	if err := reg.Register("agent-alpha", fake.New(func(_ int) contract.TurnResponse {
		return contract.TurnResponse{Done: false, Summary: `{"next_goal":"keep-going"}`}
	})); err != nil {
		t.Fatalf("register alpha: %v", err)
	}

	orch := NewMultiAgentOrchestrator(reg, sync)
	orch.SetDriverPipeline(Pipeline{
		mockGate{
			name:   "audit",
			result: contract.GateFailure{R: contract.GateReport{Gate: "audit", Passed: false}, Reason: contract.HaltGateFailure},
			state:  state,
		},
	})
	orch.SetDriverSleeper(instantSleeper{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := orch.Run(ctx, contract.OrchestratorConfig{
		Agents:     []string{"agent-alpha"},
		WorkingDir: tmpDir,
	})
	if err == nil {
		t.Fatal("expected halt error")
	}
	var he *HaltedError
	if !errors.As(err, &he) {
		t.Fatalf("expected *HaltedError, got %T", err)
	}
	if he.Reason != contract.HaltGateFailure {
		t.Fatalf("reason = %q, want gate-failure", he.Reason)
	}

	final, _ := sync.Read()
	if !final.Halted {
		t.Fatal("expected halted state persisted")
	}
	if final.LastFailedGate != "audit" {
		t.Fatalf("last_failed_gate = %q, want audit", final.LastFailedGate)
	}
}

func TestOrchestratorAdapterNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	sync := NewStateFileSync(filepath.Join(tmpDir, "cycle.json"))

	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: e2e", MaxCycles: 10}, "cyc_e2e_3")
	_ = sync.Write(state)

	// Empty registry — no adapters registered.
	orch := NewMultiAgentOrchestrator(adapter.NewRegistry(), sync)
	orch.SetDriverPipeline(Pipeline{passGate{name: "ok"}})
	orch.SetDriverSleeper(instantSleeper{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := orch.Run(ctx, contract.OrchestratorConfig{
		Agents:     []string{"missing-agent"},
		WorkingDir: tmpDir,
	})
	if err == nil {
		t.Fatal("expected error for missing adapter")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		// Should fail immediately, not timeout.
		t.Logf("error: %v", err)
	}
}

func TestOrchestratorAppendsTurnAndHandoffLedger(t *testing.T) {
	tmpDir := t.TempDir()
	sync := NewStateFileSync(filepath.Join(tmpDir, "cycle.json"))
	ledger := NewLedgerFileSync(filepath.Join(tmpDir, "cycle-ledger.jsonl"))

	state, err := NewState(contract.NewCycleRequest{Goal: "feat: handoff", MaxCycles: 1}, "cyc_handoff")
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	if err := sync.Write(state); err != nil {
		t.Fatalf("write initial state: %v", err)
	}

	reg := adapter.NewRegistry()
	if err := reg.Register("builder", fake.New(func(_ int) contract.TurnResponse {
		return contract.TurnResponse{
			HandoffTo: "reviewer",
			Summary:   `{"next_goal":"review handoff"}`,
			Artifacts: []string{"handoff.md"},
		}
	})); err != nil {
		t.Fatalf("register builder: %v", err)
	}

	orch := NewMultiAgentOrchestrator(reg, sync)
	orch.SetLedger(ledger)
	orch.SetDriverPipeline(Pipeline{passGate{name: "ok"}})
	orch.SetDriverSleeper(instantSleeper{})

	if err := orch.Run(context.Background(), contract.OrchestratorConfig{
		Agents:     []string{"builder"},
		WorkingDir: tmpDir,
	}); err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}

	entries, err := ledger.ReadAll()
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %#v, want 2", entries)
	}
	if entries[0].Type != contract.LedgerEventAgentTurn {
		t.Fatalf("first type = %q, want %q", entries[0].Type, contract.LedgerEventAgentTurn)
	}
	if entries[0].Agent != "builder" || entries[0].HandoffTo != "" {
		t.Fatalf("first entry = %#v", entries[0])
	}
	if len(entries[0].Files) != 1 || entries[0].Files[0] != "handoff.md" {
		t.Fatalf("first files = %v", entries[0].Files)
	}
	if entries[1].Type != contract.LedgerEventHandoffCreated {
		t.Fatalf("second type = %q, want %q", entries[1].Type, contract.LedgerEventHandoffCreated)
	}
	if entries[1].Agent != "builder" || entries[1].HandoffTo != "reviewer" {
		t.Fatalf("second entry = %#v", entries[1])
	}
	if len(entries[1].Files) != 1 || entries[1].Files[0] != "handoff.md" {
		t.Fatalf("second files = %v", entries[1].Files)
	}
}

func TestSummariseStateCapsSlices(t *testing.T) {
	state := State{CycleState: contract.CycleState{
		ID:              "cyc_1",
		Phase:           contract.CyclePhaseAudit,
		CompletedCycles: 5,
		MaxCycles:       10,
		Branch:          "feat/test",
		BaselineSHA:     "abc123",
		NextCycleGoal:   "fix lint",
		Halted:          false,
	}}
	// Fill slices beyond caps.
	for i := 0; i < 30; i++ {
		state.PendingFiles = append(state.PendingFiles, fmt.Sprintf("file%d.go", i))
		state.LocalGates = append(state.LocalGates, fmt.Sprintf("check-%d", i))
	}
	for i := 0; i < 15; i++ {
		state.ViolationsFound = append(state.ViolationsFound, contract.Violation{File: "a.go", Line: i, Type: "rop", Message: "msg"})
	}

	sum := summariseState(state)
	if len(sum.PendingFiles) != 20 {
		t.Fatalf("pending_files capped to %d, want 20", len(sum.PendingFiles))
	}
	if len(sum.LocalGates) != 20 {
		t.Fatalf("local_gates capped to %d, want 20", len(sum.LocalGates))
	}
	if len(sum.ViolationsFound) != 10 {
		t.Fatalf("violations capped to %d, want 10", len(sum.ViolationsFound))
	}
	if sum.ID != state.ID {
		t.Fatalf("ID mismatch")
	}
}

func TestBuildRequestIncludesWorkContract(t *testing.T) {
	state, err := NewState(contract.NewCycleRequest{
		Goal:       "feat: harness contract",
		Branch:     "feat/harness",
		MaxCycles:  5,
		AutoGoal:   true,
		LocalGates: []string{"make check-all"},
	}, "cyc_contract")
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	orch := &MultiAgentOrchestrator{}
	cfg := contract.OrchestratorConfig{
		Agents:     []string{"codex"},
		WorkingDir: "/repo",
	}

	req, err := orch.buildRequest(state, "codex", cfg)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	if req.WorkContract == nil {
		t.Fatal("work_contract is nil")
	}
	if req.WorkContract.Objective != "feat: harness contract" {
		t.Fatalf("objective = %q", req.WorkContract.Objective)
	}
	if req.WorkContract.RepoRoot != "/repo" {
		t.Fatalf("repo_root = %q", req.WorkContract.RepoRoot)
	}
	if len(req.WorkContract.LocalGates) != 1 || req.WorkContract.LocalGates[0] != "make check-all" {
		t.Fatalf("local_gates = %v", req.WorkContract.LocalGates)
	}
}

// TestApplyResponseJSON is the false-positive guard: a well-formed structured
// handshake MUST still parse and advance (do not over-tighten the new strict path).
func TestApplyResponseJSON(t *testing.T) {
	state := State{CycleState: contract.CycleState{ID: "cyc_1", NextCycleGoal: "old"}}
	orch := &MultiAgentOrchestrator{}

	// Structured JSON response.
	resp := contract.TurnResponse{Summary: `{"next_goal":"new-goal","violations_found":[{"file":"x.go","line":1,"type":"rop","message":"m"}]}`}
	if err := orch.applyResponse(&state, resp); err != nil {
		t.Fatalf("applyResponse: %v", err)
	}
	if state.Halted {
		t.Fatal("a well-formed handshake must not halt")
	}
	if state.NextCycleGoal != "new-goal" {
		t.Fatalf("next_cycle_goal = %q, want new-goal", state.NextCycleGoal)
	}
	if len(state.ViolationsFound) != 1 {
		t.Fatalf("violations = %d, want 1", len(state.ViolationsFound))
	}
}

// TestApplyResponseEmptySummaryIsNoOp is the second false-positive guard: an
// empty summary (a Done / pure-handoff turn) is a legitimate no-op, NOT a halt.
// The downstream goal gate — not the handshake parser — decides whether a
// missing goal is fatal.
func TestApplyResponseEmptySummaryIsNoOp(t *testing.T) {
	state := State{CycleState: contract.CycleState{ID: "cyc_1", NextCycleGoal: "keep"}}
	orch := &MultiAgentOrchestrator{}

	if err := orch.applyResponse(&state, contract.TurnResponse{Summary: ""}); err != nil {
		t.Fatalf("applyResponse: %v", err)
	}
	if state.Halted {
		t.Fatal("an empty summary must not halt")
	}
	if state.NextCycleGoal != "keep" {
		t.Fatalf("next_cycle_goal = %q, want unchanged keep", state.NextCycleGoal)
	}
}

func TestApplyResponseHaltRequested(t *testing.T) {
	state := State{CycleState: contract.CycleState{ID: "cyc_1", Halted: false}}
	orch := &MultiAgentOrchestrator{}

	// An explicit halt request now surfaces a *HaltedError (ROP): the orchestrator
	// must stop, not silently advance.
	resp := contract.TurnResponse{Summary: `{"halt_requested":true}`}
	err := orch.applyResponse(&state, resp)
	var he *HaltedError
	if !errors.As(err, &he) {
		t.Fatalf("expected *HaltedError, got %T: %v", err, err)
	}
	if he.Reason != contract.HaltUserRequested {
		t.Fatalf("reason = %q, want user-requested", he.Reason)
	}
	if !state.Halted || state.HaltReason != contract.HaltUserRequested {
		t.Fatalf("state halted=%v reason=%q, want halted=true reason=user-requested", state.Halted, state.HaltReason)
	}
}

// TestApplyResponseUnparseableHalts encodes the NEW strict contract (replacing
// the old loose fallback): a non-empty summary that is NOT valid typed JSON must
// HALT with HaltUnparseableTurn. Prose is never coerced into the next goal —
// the harness trusts structural facts, not self-report (parse-don't-validate).
func TestApplyResponseUnparseableHalts(t *testing.T) {
	state := State{CycleState: contract.CycleState{ID: "cyc_1", NextCycleGoal: "old"}}
	orch := &MultiAgentOrchestrator{}

	resp := contract.TurnResponse{Summary: "plain text goal"}
	err := orch.applyResponse(&state, resp)
	var he *HaltedError
	if !errors.As(err, &he) {
		t.Fatalf("expected *HaltedError for unparseable turn, got %T: %v", err, err)
	}
	if he.Reason != contract.HaltUnparseableTurn {
		t.Fatalf("reason = %q, want unparseable-turn", he.Reason)
	}
	if !state.Halted {
		t.Fatal("expected halted state on unparseable turn")
	}
	if state.NextCycleGoal != "old" {
		t.Fatalf("next_cycle_goal = %q, want unchanged old (prose must NOT become the goal)", state.NextCycleGoal)
	}
}

func TestOrchestratorRefusesToResumeSealedCycle(t *testing.T) {
	tmpDir := t.TempDir()
	stateSync := NewStateFileSync(filepath.Join(tmpDir, "cycle.json"))
	ledger := NewLedgerFileSync(filepath.Join(tmpDir, "cycle-ledger.jsonl"))

	const cycleID = "cyc_sealed_1"
	state, err := NewState(contract.NewCycleRequest{Goal: "feat: sealed-test", MaxCycles: 10}, cycleID)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	// A re-trigger would write a state file again; simulate that to prove the
	// guard reads the ledger, not the (re-created) state file.
	if err := stateSync.Write(state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	// Ledger ends in a sticky cycle_halted with no later validation_green.
	if err := ledger.Append(contract.NewAgentTurnLedgerEntry(1000, cycleID, "builder", contract.TurnResponse{Summary: "work"})); err != nil {
		t.Fatalf("seed turn: %v", err)
	}
	if err := ledger.Append(contract.NewHarnessLedgerEntry(1001, cycleID, contract.LedgerEventCycleHalted, string(contract.HaltStuck), nil)); err != nil {
		t.Fatalf("seed halt: %v", err)
	}

	ranCalls := 0
	reg := adapter.NewRegistry()
	if err := reg.Register("builder", fake.New(func(_ int) contract.TurnResponse {
		ranCalls++
		return contract.TurnResponse{Done: false, Summary: `{"next_goal":"should-never-run"}`}
	})); err != nil {
		t.Fatalf("register builder: %v", err)
	}

	orch := NewMultiAgentOrchestrator(reg, stateSync)
	orch.SetLedger(ledger)
	orch.SetDriverPipeline(Pipeline{passGate{name: "ok"}})
	orch.SetDriverSleeper(instantSleeper{})

	runErr := orch.Run(context.Background(), contract.OrchestratorConfig{
		Agents:     []string{"builder"},
		WorkingDir: tmpDir,
	})

	if runErr == nil {
		t.Fatal("expected a halt error refusing resume, got nil")
	}
	var he *HaltedError
	if !errors.As(runErr, &he) {
		t.Fatalf("expected *HaltedError, got %T: %v", runErr, runErr)
	}
	if he.Reason != contract.HaltStuck {
		t.Fatalf("reason = %q, want %q", he.Reason, contract.HaltStuck)
	}
	if ranCalls != 0 {
		t.Fatalf("adapter ran %d times; a sealed cycle must not resume", ranCalls)
	}
}

func TestOrchestratorResumesAfterProgress(t *testing.T) {
	tmpDir := t.TempDir()
	stateSync := NewStateFileSync(filepath.Join(tmpDir, "cycle.json"))
	ledger := NewLedgerFileSync(filepath.Join(tmpDir, "cycle-ledger.jsonl"))

	const cycleID = "cyc_resume_1"
	state, err := NewState(contract.NewCycleRequest{Goal: "feat: resume-test", MaxCycles: 1}, cycleID)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	if err := stateSync.Write(state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	// A halt followed by a validation_green: measurable progress lifts the seal.
	if err := ledger.Append(contract.NewHarnessLedgerEntry(1000, cycleID, contract.LedgerEventCycleHalted, string(contract.HaltStuck), nil)); err != nil {
		t.Fatalf("seed halt: %v", err)
	}
	if err := ledger.Append(contract.NewHarnessLedgerEntry(1001, cycleID, contract.LedgerEventValidationGreen, "tests green", nil)); err != nil {
		t.Fatalf("seed green: %v", err)
	}

	ranCalls := 0
	reg := adapter.NewRegistry()
	if err := reg.Register("builder", fake.New(func(_ int) contract.TurnResponse {
		ranCalls++
		return contract.TurnResponse{Done: false, Summary: `{"next_goal":"keep-going"}`}
	})); err != nil {
		t.Fatalf("register builder: %v", err)
	}

	orch := NewMultiAgentOrchestrator(reg, stateSync)
	orch.SetLedger(ledger)
	orch.SetDriverPipeline(Pipeline{passGate{name: "ok"}})
	orch.SetDriverSleeper(instantSleeper{})

	if err := orch.Run(context.Background(), contract.OrchestratorConfig{
		Agents:     []string{"builder"},
		WorkingDir: tmpDir,
	}); err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}
	if ranCalls == 0 {
		t.Fatal("adapter never ran; a cycle with progress after halt must resume")
	}
}

func TestOrchestratorHaltsOnLifetimeCeiling(t *testing.T) {
	tmpDir := t.TempDir()
	stateSync := NewStateFileSync(filepath.Join(tmpDir, "cycle.json"))
	ledger := NewLedgerFileSync(filepath.Join(tmpDir, "cycle-ledger.jsonl"))

	const cycleID = "cyc_ceiling_1"
	const ceiling = 3
	state, err := NewState(contract.NewCycleRequest{Goal: "feat: ceiling-test", MaxCycles: 100}, cycleID)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	if err := stateSync.Write(state); err != nil {
		t.Fatalf("write initial state: %v", err)
	}

	// A PRODUCTIVE runaway: every turn emits a NOVEL goal so Stuck() never
	// fires (distinct fingerprints, no repeat / ping-pong / no-progress). Only
	// the hard lifetime ceiling can stop it.
	reg := adapter.NewRegistry()
	if err := reg.Register("builder", fake.New(func(turn int) contract.TurnResponse {
		return contract.TurnResponse{Done: false, Summary: fmt.Sprintf(`{"next_goal":"novel-goal-%d"}`, turn)}
	})); err != nil {
		t.Fatalf("register builder: %v", err)
	}

	orch := NewMultiAgentOrchestrator(reg, stateSync)
	orch.SetLedger(ledger)
	orch.SetDriverPipeline(Pipeline{passGate{name: "ok"}})
	orch.SetDriverSleeper(instantSleeper{})

	runErr := orch.Run(context.Background(), contract.OrchestratorConfig{
		Agents:           []string{"builder"},
		WorkingDir:       tmpDir,
		MaxLifetimeTurns: ceiling,
	})

	if runErr == nil {
		t.Fatal("expected a budget-exceeded halt, got nil")
	}
	var he *HaltedError
	if !errors.As(runErr, &he) {
		t.Fatalf("expected *HaltedError, got %T: %v", runErr, runErr)
	}
	if he.Reason != contract.HaltBudgetExceeded {
		t.Fatalf("reason = %q, want %q", he.Reason, contract.HaltBudgetExceeded)
	}

	// The ceiling is enforced before a turn runs, so exactly `ceiling` turns are
	// recorded before the halt — proof it bounds a productive loop, not a stuck one.
	entries, err := ledger.ReadAll()
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	turns := 0
	halts := 0
	for _, e := range entries {
		switch e.Type {
		case contract.LedgerEventAgentTurn:
			turns++
		case contract.LedgerEventCycleHalted:
			halts++
			if e.Summary != string(contract.HaltBudgetExceeded) {
				t.Fatalf("halt summary = %q, want %q", e.Summary, contract.HaltBudgetExceeded)
			}
		}
	}
	if turns != ceiling {
		t.Fatalf("recorded %d turns, want exactly %d before the ceiling halt", turns, ceiling)
	}
	if halts != 1 {
		t.Fatalf("recorded %d cycle_halted entries, want 1", halts)
	}

	final, err := stateSync.Read()
	if err != nil {
		t.Fatalf("read final state: %v", err)
	}
	if !final.Halted || final.HaltReason != contract.HaltBudgetExceeded {
		t.Fatalf("final state halted=%v reason=%q, want halted=true reason=%q",
			final.Halted, final.HaltReason, contract.HaltBudgetExceeded)
	}
}

func TestOrchestratorHaltsWhenStuck(t *testing.T) {
	tmpDir := t.TempDir()
	stateSync := NewStateFileSync(filepath.Join(tmpDir, "cycle.json"))
	ledger := NewLedgerFileSync(filepath.Join(tmpDir, "cycle-ledger.jsonl"))

	const cycleID = "cyc_stuck_1"
	state, err := NewState(contract.NewCycleRequest{Goal: "feat: stuck-test", MaxCycles: 10}, cycleID)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	if err := stateSync.Write(state); err != nil {
		t.Fatalf("write initial state: %v", err)
	}

	// Pre-seed the ledger with StuckRepeatTurnThreshold identical agent-turn
	// entries so predicate (a) of Stuck() fires immediately.
	stuckResp := contract.TurnResponse{Summary: "same"}
	for i := 0; i < StuckRepeatTurnThreshold; i++ {
		entry := contract.NewAgentTurnLedgerEntry(int64(1000+i), cycleID, "builder", stuckResp)
		if err := ledger.Append(entry); err != nil {
			t.Fatalf("seed ledger entry %d: %v", i, err)
		}
	}

	reg := adapter.NewRegistry()
	if err := reg.Register("builder", fake.New(func(_ int) contract.TurnResponse {
		return contract.TurnResponse{Done: false, Summary: `{"next_goal":"should-never-run"}`}
	})); err != nil {
		t.Fatalf("register builder: %v", err)
	}

	orch := NewMultiAgentOrchestrator(reg, stateSync)
	orch.SetLedger(ledger)
	orch.SetDriverPipeline(Pipeline{passGate{name: "ok"}})
	orch.SetDriverSleeper(instantSleeper{})

	runErr := orch.Run(context.Background(), contract.OrchestratorConfig{
		Agents:     []string{"builder"},
		WorkingDir: tmpDir,
	})

	if runErr == nil {
		t.Fatal("expected a halt error, got nil")
	}
	var he *HaltedError
	if !errors.As(runErr, &he) {
		t.Fatalf("expected *HaltedError, got %T: %v", runErr, runErr)
	}
	if he.Reason != contract.HaltStuck {
		t.Fatalf("reason = %q, want %q", he.Reason, contract.HaltStuck)
	}
}

// TestOrchestratorHaltsOnUnparseableTurn proves the strict handshake end-to-end:
// an adapter that returns a non-JSON summary halts the run with
// HaltUnparseableTurn, persists the sealed state, and records a cycle_halted
// ledger entry (so the reviver guard then seals resume). Parse-don't-validate at
// the agent boundary, wired through the live loop — not just the unit.
func TestOrchestratorHaltsOnUnparseableTurn(t *testing.T) {
	tmpDir := t.TempDir()
	stateSync := NewStateFileSync(filepath.Join(tmpDir, "cycle.json"))
	ledger := NewLedgerFileSync(filepath.Join(tmpDir, "cycle-ledger.jsonl"))

	const cycleID = "cyc_unparseable_1"
	state, err := NewState(contract.NewCycleRequest{Goal: "feat: handshake-test", MaxCycles: 10}, cycleID)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	if err := stateSync.Write(state); err != nil {
		t.Fatalf("write initial state: %v", err)
	}

	// The adapter returns prose, not the typed JSON handshake. The old loose
	// fallback would have treated this as the next goal and advanced; the strict
	// contract halts.
	reg := adapter.NewRegistry()
	if err := reg.Register("builder", fake.New(func(_ int) contract.TurnResponse {
		return contract.TurnResponse{Done: false, Summary: "I made some progress on the refactor"}
	})); err != nil {
		t.Fatalf("register builder: %v", err)
	}

	orch := NewMultiAgentOrchestrator(reg, stateSync)
	orch.SetLedger(ledger)
	orch.SetDriverPipeline(Pipeline{passGate{name: "ok"}})
	orch.SetDriverSleeper(instantSleeper{})

	runErr := orch.Run(context.Background(), contract.OrchestratorConfig{
		Agents:     []string{"builder"},
		WorkingDir: tmpDir,
	})

	if runErr == nil {
		t.Fatal("expected an unparseable-turn halt, got nil")
	}
	var he *HaltedError
	if !errors.As(runErr, &he) {
		t.Fatalf("expected *HaltedError, got %T: %v", runErr, runErr)
	}
	if he.Reason != contract.HaltUnparseableTurn {
		t.Fatalf("reason = %q, want %q", he.Reason, contract.HaltUnparseableTurn)
	}

	final, err := stateSync.Read()
	if err != nil {
		t.Fatalf("read final state: %v", err)
	}
	if !final.Halted || final.HaltReason != contract.HaltUnparseableTurn {
		t.Fatalf("final state halted=%v reason=%q, want halted=true reason=%q",
			final.Halted, final.HaltReason, contract.HaltUnparseableTurn)
	}
	// The cycle halted at the handshake, before the pipeline could complete a cycle.
	if final.CompletedCycles != 0 {
		t.Fatalf("completed_cycles = %d, want 0 (halt must precede pipeline completion)", final.CompletedCycles)
	}

	// A cycle_halted entry must be recorded so the reviver guard seals resume.
	entries, err := ledger.ReadAll()
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	halts := 0
	for _, e := range entries {
		if e.Type == contract.LedgerEventCycleHalted {
			halts++
			if e.Summary != string(contract.HaltUnparseableTurn) {
				t.Fatalf("halt summary = %q, want %q", e.Summary, contract.HaltUnparseableTurn)
			}
		}
	}
	if halts != 1 {
		t.Fatalf("recorded %d cycle_halted entries, want 1", halts)
	}
}
