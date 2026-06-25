package gates

import (
	"context"
	"strings"
	"testing"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
)

// newAuditState is a helper that builds a valid cycle.State for audit gate tests.
func newAuditState(t *testing.T, id string) cycle.State {
	t.Helper()
	state, err := cycle.NewState(contract.NewCycleRequest{Goal: "feat: test"}, id)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	return state
}

// TestAuditGateDefaultRunsMakeCheckAll verifies that an AuditGate with no
// override runs `make check-all` (the default). In a test environment without
// a Makefile, the command exits non-zero, so we just verify the error message
// contains "make check-all" (not some override).
// False-positive guard: the default path is NOT affected by override logic.
func TestAuditGateDefaultRunsMakeCheckAll(t *testing.T) {
	state := newAuditState(t, "cyc_audit_default")

	result, _ := AuditGate{}.Run(context.Background(), state)
	// make check-all is unlikely to succeed in a test environment (no Makefile);
	// the key invariant is that the error mentions the default command, not any override.
	if _, ok := result.(contract.GateSuccess); ok {
		// If it somehow succeeds (e.g. running inside rallish with a Makefile), that is also correct.
		return
	}
	failure, ok := result.(contract.GateFailure)
	if !ok {
		t.Fatalf("result = %T, want GateFailure", result)
	}
	if failure.Reason != contract.HaltGateFailure {
		t.Fatalf("reason = %q, want %q", failure.Reason, contract.HaltGateFailure)
	}
	// Error must reference the default command, not a whitespace/empty-override error.
	if !strings.Contains(result.Report().Stderr, "make check-all") {
		t.Fatalf("stderr should mention 'make check-all', got: %q", result.Report().Stderr)
	}
}

// TestAuditGateOverrideRunsCustomCommand verifies that setting CmdOverride
// causes the gate to run the custom command instead of `make check-all`.
func TestAuditGateOverrideRunsCustomCommand(t *testing.T) {
	state := newAuditState(t, "cyc_audit_override")

	// Use `go env GOMOD` as a benign, always-succeeding command.
	result, next := AuditGate{CmdOverride: "go env GOMOD"}.Run(context.Background(), state)
	if _, ok := result.(contract.GateSuccess); !ok {
		t.Fatalf("result = %T, want GateSuccess: %q", result, result.Report().Stderr)
	}
	if next.ID != state.ID {
		t.Fatalf("state id = %q, want %q", next.ID, state.ID)
	}
	if result.Report().Gate != "audit" {
		t.Fatalf("gate = %q, want 'audit'", result.Report().Gate)
	}
}

// TestAuditGateOverrideWhitespaceOnlyFailsLoudly verifies the no-silent-fallback
// rule: an override that trims to empty must fail loudly (not silently revert to
// the default command). This is the false-positive guard for the empty-override path.
func TestAuditGateOverrideWhitespaceOnlyFailsLoudly(t *testing.T) {
	state := newAuditState(t, "cyc_audit_ws_override")

	result, _ := AuditGate{CmdOverride: "   "}.Run(context.Background(), state)
	failure, ok := result.(contract.GateFailure)
	if !ok {
		t.Fatalf("result = %T, want GateFailure for whitespace-only override", result)
	}
	if failure.Reason != contract.HaltGateFailure {
		t.Fatalf("reason = %q, want %q", failure.Reason, contract.HaltGateFailure)
	}
	if !strings.Contains(result.Report().Stderr, "whitespace") {
		t.Fatalf("stderr should mention whitespace misconfiguration, got: %q", result.Report().Stderr)
	}
}

// TestAuditGateDefaultUnaffectedByNilOverride is the false-positive guard confirming
// that a zero-value AuditGate (CmdOverride == "") behaves identically to the default
// path and does NOT trigger the whitespace-only misconfiguration error.
func TestAuditGateDefaultUnaffectedByNilOverride(t *testing.T) {
	state := newAuditState(t, "cyc_audit_zero")

	result, _ := AuditGate{CmdOverride: ""}.Run(context.Background(), state)
	// The gate must attempt make check-all (not fail with the misconfiguration error).
	if failure, ok := result.(contract.GateFailure); ok {
		if strings.Contains(failure.R.Stderr, "whitespace") {
			t.Fatalf("zero-value CmdOverride wrongly triggered misconfiguration error: %q", failure.R.Stderr)
		}
	}
}

// TestLocalAuditGateRunsMakeCheck verifies that LocalAuditGate runs `make check`.
// In a test environment without a Makefile the command fails; the invariant is
// that the stderr references the expected command.
func TestLocalAuditGateRunsMakeCheck(t *testing.T) {
	state := newAuditState(t, "cyc_local_audit")

	result, _ := LocalAuditGate{}.Run(context.Background(), state)
	if _, ok := result.(contract.GateSuccess); ok {
		// If a Makefile happens to exist, success is also acceptable.
		return
	}
	failure, ok := result.(contract.GateFailure)
	if !ok {
		t.Fatalf("result = %T, want GateFailure", result)
	}
	if failure.Reason != contract.HaltGateFailure {
		t.Fatalf("reason = %q, want %q", failure.Reason, contract.HaltGateFailure)
	}
	if !strings.Contains(result.Report().Stderr, "make check") {
		t.Fatalf("stderr should mention 'make check', got: %q", result.Report().Stderr)
	}
}
