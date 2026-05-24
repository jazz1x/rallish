package cycle

import (
	"context"
	"testing"

	"github.com/jazz1x/rallish/pkg/contract"
)

// testGate is a test double for the Gate interface.
type testGate struct {
	name   string
	result contract.GateResult
	state  State
}

func (m testGate) Name() string { return m.name }

func (m testGate) Run(_ context.Context, _ State) (contract.GateResult, State) {
	return m.result, m.state
}

func TestPipelineExecuteAllSuccess(t *testing.T) {
	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_1_abcd")
	p := Pipeline{
		testGate{name: "g1", result: contract.GateSuccess{R: contract.GateReport{Gate: "g1", Passed: true}}, state: state},
		testGate{name: "g2", result: contract.GateSuccess{R: contract.GateReport{Gate: "g2", Passed: true}}, state: state},
	}
	result := p.Execute(context.Background(), state)
	if result.IsFailure() {
		t.Fatalf("expected success, got %v", result.Err())
	}
	if len(result.Reports()) != 2 {
		t.Fatalf("reports = %d, want 2", len(result.Reports()))
	}
}

func TestPipelineExecuteFailureShortCircuits(t *testing.T) {
	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_1_abcd")
	haltedState := state
	haltedState.Halted = true
	p := Pipeline{
		testGate{name: "g1", result: contract.GateSuccess{R: contract.GateReport{Gate: "g1", Passed: true}}, state: state},
		testGate{name: "g2", result: contract.GateFailure{R: contract.GateReport{Gate: "g2", Passed: false}, Reason: contract.HaltGateFailure}, state: haltedState},
		testGate{name: "g3", result: contract.GateSuccess{R: contract.GateReport{Gate: "g3", Passed: true}}, state: state}, // never runs
	}
	result := p.Execute(context.Background(), state)
	if result.IsSuccess() {
		t.Fatal("expected failure")
	}
	reports := result.Reports()
	if len(reports) != 2 {
		t.Fatalf("reports = %d, want 2", len(reports))
	}
	he, ok := result.Err().(*HaltedError)
	if !ok {
		t.Fatalf("expected *HaltedError, got %T", result.Err())
	}
	if he.Reason != contract.HaltGateFailure {
		t.Fatalf("reason = %q, want gate-failure", he.Reason)
	}
}

func TestPipelineExecuteWarningContinues(t *testing.T) {
	state, _ := NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_1_abcd")
	p := Pipeline{
		testGate{name: "g1", result: contract.GateWarning{R: contract.GateReport{Gate: "g1", Passed: true}}, state: state},
		testGate{name: "g2", result: contract.GateSuccess{R: contract.GateReport{Gate: "g2", Passed: true}}, state: state},
	}
	result := p.Execute(context.Background(), state)
	if result.IsFailure() {
		t.Fatalf("expected success, got %v", result.Err())
	}
	if len(result.Reports()) != 2 {
		t.Fatalf("reports = %d, want 2", len(result.Reports()))
	}
}

func TestPipelineNames(t *testing.T) {
	p := Pipeline{
		testGate{name: "alpha"},
		testGate{name: "beta"},
	}
	names := p.Names()
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Fatalf("names = %v", names)
	}
}
