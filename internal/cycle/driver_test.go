package cycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jazz1x/rallish/pkg/contract"
)

// mockGate is a test double for the Gate interface.
type mockGate struct {
	name   string
	result contract.GateResult
	state  State
}

func (m mockGate) Name() string { return m.name }

func (m mockGate) Run(_ context.Context, _ State) (contract.GateResult, State) {
	return m.result, m.state
}

// instantSleeper never sleeps (speeds up tests).
type instantSleeper struct{}

func (instantSleeper) Sleep(ctx context.Context, _ time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func TestDriverStepCompletesCycle(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "cycle-test.json")
	sync := NewStateFileSync(tmp)

	state, err := NewState(contract.NewCycleRequest{Goal: "feat: test", MaxCycles: 3}, "cyc_1_abcd")
	if err != nil {
		t.Fatalf("new state: %v", err)
	}

	d := NewCycleDriver(sync)
	d.SetSleeper(instantSleeper{})
	d.SetPipeline(Pipeline{
		mockGate{name: "preflight", result: contract.GateSuccess{R: contract.GateReport{Gate: "preflight", Passed: true}}, state: state},
	})

	result := d.Step(context.Background(), state)
	if result.IsFailure() {
		t.Fatalf("step failed: %v", result.Err())
	}

	final := result.Value()
	if final.CompletedCycles != 1 {
		t.Fatalf("completed_cycles = %d, want 1", final.CompletedCycles)
	}
	if final.NextCycleGoal != "" {
		t.Fatalf("goal should be cleared after step")
	}
}

// TestDriverStepEmitsValidationGreenOnGatePass proves the verifier-produced
// completion record (B1 fix): a successful gated Step with a ledger injected
// appends gate_passed (one per report), validation_green, and cycle_completed.
// Before the fix, validation_green had no production emit site (only tests wrote
// it), so the reviver's revive edge was dead.
func TestDriverStepEmitsValidationGreenOnGatePass(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "cycle-test.json")
	sync := NewStateFileSync(tmp)
	ledger := NewLedgerFileSync(filepath.Join(t.TempDir(), "cycle-ledger.jsonl"))

	state, err := NewState(contract.NewCycleRequest{Goal: "feat: green", MaxCycles: 3}, "cyc_green_1")
	if err != nil {
		t.Fatalf("new state: %v", err)
	}

	d := NewCycleDriver(sync)
	d.SetSleeper(instantSleeper{})
	d.SetLedger(ledger)
	d.SetPipeline(Pipeline{passGate{name: "preflight"}, passGate{name: "audit"}})

	result := d.Step(context.Background(), state)
	if result.IsFailure() {
		t.Fatalf("step failed: %v", result.Err())
	}

	entries, err := ledger.ReadAll()
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	var gatePassed, green, completed int
	for _, e := range entries {
		switch e.Type {
		case contract.LedgerEventGatePassed:
			gatePassed++
		case contract.LedgerEventValidationGreen:
			green++
		case contract.LedgerEventCycleCompleted:
			completed++
		}
	}
	if gatePassed != 2 {
		t.Fatalf("gate_passed count = %d, want 2 (one per gate report)", gatePassed)
	}
	if green != 1 {
		t.Fatalf("validation_green count = %d, want exactly 1", green)
	}
	if completed != 1 {
		t.Fatalf("cycle_completed count = %d, want exactly 1", completed)
	}
}

// TestDriverStepGreenUnsealsAfterHalt is THE key B1 test: a cycle with a sticky
// cycle_halted in its ledger is sealed (LedgerSealsResume true); after a real,
// verifier-produced green Step (the gate pipeline passed, ledger injected), the
// same ledger now PERMITS resume. This is the previously-impossible un-seal path
// — it proves the revive-on-measurable-progress edge works from production code.
func TestDriverStepGreenUnsealsAfterHalt(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "cycle-test.json")
	sync := NewStateFileSync(tmp)
	ledger := NewLedgerFileSync(filepath.Join(t.TempDir(), "cycle-ledger.jsonl"))

	const cycleID = "cyc_unseal_1"
	state, err := NewState(contract.NewCycleRequest{Goal: "feat: recover", MaxCycles: 5}, cycleID)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}

	// Seed a sticky halt: the cycle is sealed and must not resume yet.
	if err := ledger.Append(contract.NewHarnessLedgerEntry(
		1000, cycleID, contract.LedgerEventCycleHalted, string(contract.HaltStuck), nil)); err != nil {
		t.Fatalf("seed halt: %v", err)
	}
	sealedBefore, _ := ledger.ReadAll()
	if _, sealed := LedgerSealsResume(sealedBefore); !sealed {
		t.Fatal("precondition: a halt with no later green must seal the cycle")
	}

	// A real verifier green: a passing gated Step with the ledger injected.
	d := NewCycleDriver(sync)
	d.SetSleeper(instantSleeper{})
	d.SetLedger(ledger)
	d.SetPipeline(Pipeline{passGate{name: "ok"}})

	result := d.Step(context.Background(), state)
	if result.IsFailure() {
		t.Fatalf("recovery step failed: %v", result.Err())
	}

	// The seal must now be lifted: measurable progress (validation_green) was
	// recorded after the halt by the verifier, not by agent self-report.
	after, err := ledger.ReadAll()
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if _, sealed := LedgerSealsResume(after); sealed {
		t.Fatal("a verifier-produced green after the halt must un-seal the cycle (B1 revive edge)")
	}
}

// TestDriverStepFailureKeepsSeal is the false-positive guard for B1: a halted
// cycle whose subsequent Step FAILS its gate emits NO validation_green, so the
// anti-spin seal must still hold. This proves the fix does not weaken the sticky
// halt — only a genuine verifier green lifts it.
func TestDriverStepFailureKeepsSeal(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "cycle-test.json")
	sync := NewStateFileSync(tmp)
	ledger := NewLedgerFileSync(filepath.Join(t.TempDir(), "cycle-ledger.jsonl"))

	const cycleID = "cyc_stay_sealed_1"
	state, err := NewState(contract.NewCycleRequest{Goal: "feat: still-broken", MaxCycles: 5}, cycleID)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}

	if err := ledger.Append(contract.NewHarnessLedgerEntry(
		1000, cycleID, contract.LedgerEventCycleHalted, string(contract.HaltStuck), nil)); err != nil {
		t.Fatalf("seed halt: %v", err)
	}

	d := NewCycleDriver(sync)
	d.SetSleeper(instantSleeper{})
	d.SetLedger(ledger)
	d.SetPipeline(Pipeline{failGate{name: "audit"}})

	result := d.Step(context.Background(), state)
	if result.IsSuccess() {
		t.Fatal("expected a halt on the failing gate")
	}

	after, err := ledger.ReadAll()
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	for _, e := range after {
		if e.Type == contract.LedgerEventValidationGreen {
			t.Fatal("a failing gate must NOT emit validation_green")
		}
	}
	reason, sealed := LedgerSealsResume(after)
	if !sealed {
		t.Fatal("a halt with no later green must stay sealed (anti-spin seal intact)")
	}
	if reason != contract.HaltStuck {
		t.Fatalf("sticky reason = %q, want %q", reason, contract.HaltStuck)
	}
}

func TestDriverStepHaltOnGateFailure(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "cycle-test.json")
	sync := NewStateFileSync(tmp)

	state, err := NewState(contract.NewCycleRequest{Goal: "feat: test", MaxCycles: 10}, "cyc_2_abcd")
	if err != nil {
		t.Fatalf("new state: %v", err)
	}

	d := NewCycleDriver(sync)
	d.SetPipeline(Pipeline{
		mockGate{
			name:   "audit",
			result: contract.GateFailure{R: contract.GateReport{Gate: "audit", Passed: false}, Reason: contract.HaltGateFailure},
			state:  state,
		},
	})

	result := d.Step(context.Background(), state)
	if result.IsSuccess() {
		t.Fatal("expected halt error")
	}
	var he *HaltedError
	if !errors.As(result.Err(), &he) {
		t.Fatalf("expected *HaltedError, got %T", result.Err())
	}
	if he.Reason != contract.HaltGateFailure {
		t.Fatalf("reason = %q, want gate-failure", he.Reason)
	}

	final := result.Value()
	if !final.Halted {
		t.Fatal("expected halted")
	}
	if final.LastFailedGate != "audit" {
		t.Fatalf("last_failed_gate = %q, want audit", final.LastFailedGate)
	}
}

func TestDriverStepRequiresGoal(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "cycle-test.json")
	sync := NewStateFileSync(tmp)

	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_3_abcd")
	state.NextCycleGoal = "" // clear goal

	d := NewCycleDriver(sync)
	d.SetPipeline(Pipeline{mockGate{name: "ok", result: contract.GateSuccess{R: contract.GateReport{Gate: "ok", Passed: true}}, state: state}})

	result := d.Step(context.Background(), state)
	if result.IsSuccess() {
		t.Fatal("expected failure for empty goal")
	}
	if !errors.Is(result.Err(), contract.ErrGoalRequired) {
		t.Fatalf("expected ErrGoalRequired, got %v", result.Err())
	}
}

func TestDriverRunSingleCycle(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "cycle-test.json")
	sync := NewStateFileSync(tmp)

	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test", MaxCycles: 1}, "cyc_4_abcd")
	_ = sync.Write(state)

	d := NewCycleDriver(sync)
	d.SetSleeper(instantSleeper{})
	d.SetPipeline(Pipeline{
		mockGate{name: "preflight", result: contract.GateSuccess{R: contract.GateReport{Gate: "preflight", Passed: true}}, state: state},
	})

	// Run should complete one cycle then exit because max cycles reached.
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	final, err := sync.Read()
	if err != nil {
		t.Fatalf("read final state: %v", err)
	}
	if final.CompletedCycles != 1 {
		t.Fatalf("completed_cycles = %d, want 1", final.CompletedCycles)
	}
}

func TestDriverRunHaltOnFailure(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "cycle-test.json")
	sync := NewStateFileSync(tmp)

	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test", MaxCycles: 10}, "cyc_5_abcd")
	_ = sync.Write(state)

	d := NewCycleDriver(sync)
	d.SetSleeper(instantSleeper{})
	d.SetPipeline(Pipeline{
		mockGate{
			name:   "audit",
			result: contract.GateFailure{R: contract.GateReport{Gate: "audit", Passed: false}, Reason: contract.HaltGateFailure},
			state:  state,
		},
	})

	err := d.Run(context.Background())
	if err == nil {
		t.Fatal("expected halt error")
	}
	var he *HaltedError
	if !errors.As(err, &he) {
		t.Fatalf("expected *HaltedError, got %T", err)
	}
}

func TestDriverReadWithRetry(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "cycle-test.json")
	sync := NewStateFileSync(tmp)

	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_6_abcd")
	_ = sync.Write(state)

	d := NewCycleDriver(sync)
	d.ReadRetries = 3

	read, err := d.readWithRetry()
	if err != nil {
		t.Fatalf("readWithRetry: %v", err)
	}
	if read.ID != "cyc_6_abcd" {
		t.Fatalf("id = %q", read.ID)
	}
}

func TestStateFileSyncAtomicWriteAndRecovery(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "cycle.json")
	sync := NewStateFileSync(tmp)

	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_7_abcd")
	// Write twice so that .bak is created from the first write.
	if err := sync.Write(state); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	state.NextCycleGoal = "updated"
	if err := sync.Write(state); err != nil {
		t.Fatalf("write 2: %v", err)
	}

	// Corrupt the primary file.
	if err := os.WriteFile(tmp, []byte("not json"), 0o600); err != nil {
		t.Fatalf("corrupt: %v", err)
	}

	// Read should recover from .bak (which holds the first write).
	recovered, err := sync.Read()
	if err != nil {
		t.Fatalf("read after corrupt: %v", err)
	}
	if recovered.ID != "cyc_7_abcd" {
		t.Fatalf("recovered id = %q", recovered.ID)
	}
	if recovered.NextCycleGoal != "feat: test" {
		t.Fatalf("recovered goal = %q, want feat: test", recovered.NextCycleGoal)
	}
}

func TestStateFileSyncRemove(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "cycle.json")
	sync := NewStateFileSync(tmp)

	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_8_abcd")
	_ = sync.Write(state)

	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("file should exist: %v", err)
	}

	if err := sync.Remove(); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("file should not exist")
	}
}

func TestDriverAutoGoalDiscoversNextGoal(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "cycle-test.json")
	sync := NewStateFileSync(tmp)

	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test", MaxCycles: 5, AutoGoal: true}, "cyc_ag_1")
	_ = sync.Write(state)

	d := NewCycleDriver(sync)
	d.SetSleeper(instantSleeper{})
	d.SetPipeline(Pipeline{passGate{name: "ok"}})

	callCount := 0
	d.goalDiscoverer = func(_ context.Context, _ State) (string, error) {
		callCount++
		if callCount <= 2 {
			return fmt.Sprintf("auto-goal-%d", callCount), nil
		}
		return "", nil
	}

	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	final, _ := sync.Read()
	if final.CompletedCycles != 3 {
		t.Fatalf("completed_cycles = %d, want 3", final.CompletedCycles)
	}
	if final.HaltReason != contract.HaltSuccess {
		t.Fatalf("halt_reason = %q, want success", final.HaltReason)
	}
}

func TestDriverAutoGoalHaltWhenNoMoreWork(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "cycle-test.json")
	sync := NewStateFileSync(tmp)

	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test", MaxCycles: 10, AutoGoal: true}, "cyc_ag_2")
	_ = sync.Write(state)

	d := NewCycleDriver(sync)
	d.SetSleeper(instantSleeper{})
	d.SetPipeline(Pipeline{passGate{name: "ok"}})
	d.goalDiscoverer = func(_ context.Context, _ State) (string, error) {
		return "", nil // no more work found
	}

	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	final, _ := sync.Read()
	if final.CompletedCycles != 1 {
		t.Fatalf("completed_cycles = %d, want 1", final.CompletedCycles)
	}
	if final.HaltReason != contract.HaltSuccess {
		t.Fatalf("halt_reason = %q, want success", final.HaltReason)
	}
}
