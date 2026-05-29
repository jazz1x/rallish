package cycle

import (
	"errors"
	"testing"

	"github.com/jazz1x/rallish/pkg/contract"
)

func TestNewStateDefaults(t *testing.T) {
	req := contract.NewCycleRequest{Goal: "feat: test", LocalGates: []string{" go test ./... ", "", "   "}}
	state, err := NewState(req, "cyc_1_abcd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.ID != "cyc_1_abcd" {
		t.Fatalf("id = %q, want cyc_1_abcd", state.ID)
	}
	if state.Phase != contract.CyclePhasePreflight {
		t.Fatalf("phase = %q, want preflight", state.Phase)
	}
	if state.MaxCycles != 10 {
		t.Fatalf("max_cycles = %d, want 10", state.MaxCycles)
	}
	if state.Branch != "feat/autonomous-cycle" {
		t.Fatalf("branch = %q, want feat/autonomous-cycle", state.Branch)
	}
	if state.NextCycleGoal != "feat: test" {
		t.Fatalf("goal = %q", state.NextCycleGoal)
	}
	if len(state.LocalGates) != 1 || state.LocalGates[0] != "go test ./..." {
		t.Fatalf("local_gates = %v", state.LocalGates)
	}
}

func TestNewStateRejectsMain(t *testing.T) {
	req := contract.NewCycleRequest{Goal: "feat: test", Branch: "main"}
	_, err := NewState(req, "cyc_1_abcd")
	if err == nil {
		t.Fatal("expected error for main branch")
	}
	if !errors.Is(err, contract.ErrMainBranchForbidden) {
		t.Fatalf("expected ErrMainBranchForbidden, got %v", err)
	}
}

func TestNewStateRejectsEmptyGoal(t *testing.T) {
	req := contract.NewCycleRequest{Goal: ""}
	_, err := NewState(req, "cyc_1_abcd")
	if err == nil {
		t.Fatal("expected error for empty goal")
	}
	if !errors.Is(err, contract.ErrGoalRequired) {
		t.Fatalf("expected ErrGoalRequired, got %v", err)
	}
}

func TestStateAdvance(t *testing.T) {
	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_1_abcd")
	result := state.Advance()
	if result.IsFailure() {
		t.Fatalf("unexpected failure: %v", result.Err())
	}
	next := result.Value()
	if next.Phase != contract.CyclePhaseAudit {
		t.Fatalf("phase = %q, want audit", next.Phase)
	}
}

func TestStateAdvanceWhenHalted(t *testing.T) {
	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_1_abcd")
	state.Halt(contract.HaltUserRequested)
	result := state.Advance()
	if result.IsSuccess() {
		t.Fatal("expected failure when halted")
	}
	if !errors.Is(result.Err(), contract.ErrCycleHalted) {
		t.Fatalf("expected ErrCycleHalted, got %v", result.Err())
	}
}

func TestStateHalt(t *testing.T) {
	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_1_abcd")
	result := state.Halt(contract.HaltSelfAuditViolation)
	if result.IsSuccess() {
		t.Fatal("expected failure")
	}
	halted := result.Value()
	if !halted.Halted {
		t.Fatal("expected halted")
	}
	if halted.HaltReason != contract.HaltSelfAuditViolation {
		t.Fatalf("halt_reason = %q", halted.HaltReason)
	}
	if halted.Phase != contract.CyclePhaseHalted {
		t.Fatalf("phase = %q, want halted", halted.Phase)
	}
}

func TestStateCompleteCycle(t *testing.T) {
	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_1_abcd")
	state.Phase = contract.CyclePhaseCommit
	result := state.CompleteCycle()
	if result.IsFailure() {
		t.Fatalf("unexpected failure: %v", result.Err())
	}
	completed := result.Value()
	if completed.CompletedCycles != 1 {
		t.Fatalf("completed_cycles = %d, want 1", completed.CompletedCycles)
	}
	if completed.Phase != contract.CyclePhasePreflight {
		t.Fatalf("phase = %q, want preflight", completed.Phase)
	}
}

func TestStateAppendViolationsDedup(t *testing.T) {
	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_1_abcd")
	v1 := contract.Violation{File: "a.go", Line: 1, Type: "rop", Message: "m1"}
	v2 := contract.Violation{File: "a.go", Line: 1, Type: "rop", Message: "m2"} // same key
	v3 := contract.Violation{File: "b.go", Line: 2, Type: "ssot", Message: "m3"}
	state.AppendViolations([]contract.Violation{v1, v2, v3})
	if len(state.ViolationsFound) != 2 {
		t.Fatalf("violations = %d, want 2", len(state.ViolationsFound))
	}
}

func TestStateClearViolations(t *testing.T) {
	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_1_abcd")
	state.AppendViolations([]contract.Violation{
		{File: "a.go", Type: "rop"},
		{File: "b.go", Type: "ssot"},
	})
	state.ClearViolations("rop")
	if len(state.ViolationsFound) != 1 {
		t.Fatalf("violations = %d, want 1", len(state.ViolationsFound))
	}
	if state.ViolationsFound[0].Type != "ssot" {
		t.Fatalf("remaining type = %q, want ssot", state.ViolationsFound[0].Type)
	}
}
