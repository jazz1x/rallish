package gates

import (
	"context"
	"strings"
	"testing"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
)

func TestCommandGateRunSuccess(t *testing.T) {
	state, err := cycle.NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_cmd_1")
	if err != nil {
		t.Fatalf("new state: %v", err)
	}

	result, next := CommandGate{RawCmd: "go env GOMOD"}.Run(context.Background(), state)
	if _, ok := result.(contract.GateSuccess); !ok {
		t.Fatalf("result = %T, want GateSuccess: %#v", result, result.Report())
	}
	if next.ID != state.ID {
		t.Fatalf("state id = %q, want %q", next.ID, state.ID)
	}
	if result.Report().Gate != "cmd:go env GOMOD" {
		t.Fatalf("gate = %q", result.Report().Gate)
	}
}

func TestCommandGateRunRejectsEmptyCommand(t *testing.T) {
	state, err := cycle.NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_cmd_2")
	if err != nil {
		t.Fatalf("new state: %v", err)
	}

	result, _ := CommandGate{RawCmd: "   "}.Run(context.Background(), state)
	failure, ok := result.(contract.GateFailure)
	if !ok {
		t.Fatalf("result = %T, want GateFailure", result)
	}
	if failure.Reason != contract.HaltGateFailure {
		t.Fatalf("reason = %q, want %q", failure.Reason, contract.HaltGateFailure)
	}
	if !strings.Contains(result.Report().Stderr, "empty command") {
		t.Fatalf("stderr = %q", result.Report().Stderr)
	}
}

func TestCommandGateRunFailsOnNonZeroExit(t *testing.T) {
	state, err := cycle.NewState(contract.NewCycleRequest{Goal: "feat: test"}, "cyc_cmd_3")
	if err != nil {
		t.Fatalf("new state: %v", err)
	}

	result, _ := CommandGate{RawCmd: "go test ./definitely-missing-package"}.Run(context.Background(), state)
	failure, ok := result.(contract.GateFailure)
	if !ok {
		t.Fatalf("result = %T, want GateFailure", result)
	}
	if failure.Reason != contract.HaltGateFailure {
		t.Fatalf("reason = %q, want %q", failure.Reason, contract.HaltGateFailure)
	}
	if !strings.Contains(result.Report().Stderr, "failed") {
		t.Fatalf("stderr = %q", result.Report().Stderr)
	}
}
