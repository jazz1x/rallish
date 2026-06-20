package gates

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
)

func TestHasConventionalPrefix(t *testing.T) {
	prefixes := []string{
		"feat:", "fix:", "refactor:", "docs:", "test:",
		"chore:", "sec:", "ci:", "build:", "perf:", "style:",
	}
	for _, p := range prefixes {
		if !hasConventionalPrefix(p) {
			t.Errorf("hasConventionalPrefix(%q) = false, want true", p)
		}
	}

	if hasConventionalPrefix("") {
		t.Error("empty string should not have conventional prefix")
	}
	if hasConventionalPrefix("no prefix") {
		t.Error("'no prefix' should not have conventional prefix")
	}
	if hasConventionalPrefix("unknown: blah") {
		t.Error("'unknown:' should not have conventional prefix")
	}
}

// newPolishState builds a valid cycle.State for polish gate tests.
func newPolishState(t *testing.T, id string) cycle.State {
	t.Helper()
	state, err := cycle.NewState(contract.NewCycleRequest{Goal: "feat: test"}, id)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	return state
}

// TestPolishGateDefaultRunsGoTest verifies that a zero-value PolishGate (no override)
// attempts `go test -race ./...` (the default), not any override. We run it in a temp
// dir with no go.mod so the command fails fast (no Go packages to test), and verify:
// (a) the failure message references the default command, not an override or
// misconfiguration, and (b) the zero-value path never triggers the whitespace error.
// False-positive guard: zero-value CmdOverride must NOT trigger misconfiguration.
func TestPolishGateDefaultRunsGoTest(t *testing.T) {
	// Run in a temp dir with no go.mod so `go test` fails quickly.
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	state := newPolishState(t, "cyc_polish_default")

	result, _ := PolishGate{}.Run(context.Background(), state)
	switch r := result.(type) {
	case contract.GateSuccess:
		// Unlikely in an empty temp dir, but not wrong.
	case contract.GateFailure:
		// Must NOT be a misconfiguration error (false-positive guard).
		if strings.Contains(r.R.Stderr, "whitespace") {
			t.Fatalf("zero-value PolishGate wrongly triggered misconfiguration error: %q", r.R.Stderr)
		}
		// The violation message must reference the default command.
		if len(r.R.Violations) > 0 && !strings.Contains(r.R.Violations[0].Message, "go test") {
			t.Fatalf("expected violation referencing 'go test', got: %q", r.R.Violations[0].Message)
		}
	default:
		t.Fatalf("unexpected result type %T", result)
	}
}

// TestPolishGateOverrideRunsCustomCommand verifies that setting TestCmdOverride
// causes the gate to run the custom command instead of `go test -race ./...`.
// Uses `go env GOMOD` as a benign, always-succeeding command.
func TestPolishGateOverrideRunsCustomCommand(t *testing.T) {
	state := newPolishState(t, "cyc_polish_override")
	// `go env GOMOD` always succeeds and produces output.
	result, next := PolishGate{TestCmdOverride: "go env GOMOD"}.Run(context.Background(), state)
	if _, ok := result.(contract.GateSuccess); !ok {
		t.Fatalf("result = %T, want GateSuccess: stderr=%q violations=%v",
			result, result.Report().Stderr, result.Report().Violations)
	}
	if next.ID != state.ID {
		t.Fatalf("state id = %q, want %q", next.ID, state.ID)
	}
	if result.Report().Gate != "polish" {
		t.Fatalf("gate = %q, want 'polish'", result.Report().Gate)
	}
}

// TestPolishGateOverrideFailingCommandFailsGate verifies that when the override
// command exits non-zero, the gate records a violation and fails.
func TestPolishGateOverrideFailingCommandFailsGate(t *testing.T) {
	state := newPolishState(t, "cyc_polish_override_fail")
	result, _ := PolishGate{TestCmdOverride: "false"}.Run(context.Background(), state)
	if _, ok := result.(contract.GateSuccess); ok {
		t.Fatal("expected GateFailure for a failing test command, got GateSuccess")
	}
	failure, ok := result.(contract.GateFailure)
	if !ok {
		t.Fatalf("result = %T, want GateFailure", result)
	}
	if failure.Reason != contract.HaltGateFailure {
		t.Fatalf("reason = %q, want %q", failure.Reason, contract.HaltGateFailure)
	}
}

// TestPolishGateWhitespaceOnlyOverrideFailsLoudly verifies the no-silent-fallback
// rule: an override that trims to empty must fail loudly (misconfiguration), not
// silently revert to the default. This is the false-positive guard.
func TestPolishGateWhitespaceOnlyOverrideFailsLoudly(t *testing.T) {
	state := newPolishState(t, "cyc_polish_ws_override")
	result, _ := PolishGate{TestCmdOverride: "   "}.Run(context.Background(), state)
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

// TestPolishGateANSICheckSkippedWhenAbsent verifies that when
// scripts/check-no-raw-ansi.sh does not exist in the working directory, the gate
// does not fail due to the missing script. This ensures the gate is usable in
// non-rallish repos that do not have the script.
func TestPolishGateANSICheckSkippedWhenAbsent(t *testing.T) {
	// Change to a temp dir that definitely has no scripts/ directory.
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Use `go env GOMOD` as the test command so check 1 passes, and supply a
	// conventional goal so check 3 passes too.
	state := newPolishState(t, "cyc_polish_ansi_absent")

	result, _ := PolishGate{TestCmdOverride: "go env GOMOD"}.Run(context.Background(), state)
	if _, ok := result.(contract.GateSuccess); !ok {
		t.Fatalf("gate failed when ANSI script absent — should skip gracefully: stderr=%q violations=%v",
			result.Report().Stderr, result.Report().Violations)
	}
}

// TestPolishGateANSICheckRunsWhenPresent verifies that when
// scripts/check-no-raw-ansi.sh IS present in the working directory, the gate
// runs it. We supply a script that exits 1 to confirm the failure is picked up.
func TestPolishGateANSICheckRunsWhenPresent(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Create a scripts/check-no-raw-ansi.sh that exits 1.
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o750); err != nil { //nolint:gosec // test-only temp directory
		t.Fatalf("mkdir scripts: %v", err)
	}
	scriptPath := filepath.Join(scriptsDir, "check-no-raw-ansi.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 1\n"), 0o750); err != nil { //nolint:gosec // test-only executable script
		t.Fatalf("write script: %v", err)
	}

	state := newPolishState(t, "cyc_polish_ansi_present")

	// Use `go env GOMOD` so test command passes; ANSI script will fail.
	result, _ := PolishGate{TestCmdOverride: "go env GOMOD"}.Run(context.Background(), state)
	if _, ok := result.(contract.GateSuccess); ok {
		t.Fatal("expected gate to fail when ANSI script exits 1, got GateSuccess")
	}
	failure, ok := result.(contract.GateFailure)
	if !ok {
		t.Fatalf("result = %T, want GateFailure", result)
	}
	if failure.Reason != contract.HaltGateFailure {
		t.Fatalf("reason = %q, want %q", failure.Reason, contract.HaltGateFailure)
	}
	// At least one violation should mention "ANSI".
	var found bool
	for _, v := range result.Report().Violations {
		if strings.Contains(v.Message, "ANSI") || strings.Contains(v.Message, "ansi") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no ANSI violation found; violations: %v", result.Report().Violations)
	}
}
