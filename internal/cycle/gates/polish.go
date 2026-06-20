// Package gates implements the autonomous-cycle gate pipeline.
package gates

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
)

// defaultPolishTestCmd is the test command run by PolishGate when no override is set.
const defaultPolishTestCmd = "go test -race ./..."

// PolishGate runs the completion self-check table before claiming a cycle is done.
// It mirrors the /polish skill gate: tests pass, no raw ANSI (rallish-repo-specific),
// and conventional commit prefix present.
//
// TestCmdOverride, if non-empty, replaces the default `go test -race ./...` test command.
// It is split on whitespace; the first token is the executable. An override that is set
// but contains only whitespace is a misconfiguration and causes the gate to fail loudly
// (no silent revert to default).
//
// The `scripts/check-no-raw-ansi.sh` check is skipped when the script is not present in
// the working directory — it is a rallish-repo-specific check that does not apply to
// arbitrary repositories. This is "not applicable" (structural skip), not error-masking,
// and is therefore not a ROP violation. When the script IS present, a failure hard-fails
// the gate exactly as before.
type PolishGate struct {
	// TestCmdOverride replaces the default `go test -race ./...` when non-empty.
	// Configure via the cycle's polish_test_cmd config field. Leave empty to use
	// the default.
	TestCmdOverride string
}

// Name returns the canonical gate name.
func (PolishGate) Name() string { return "polish" }

// Run executes the polish checks. The default test command is `go test -race ./...`;
// set TestCmdOverride to use a project-specific command (e.g. "npm test", "cargo test").
// An override that trims to empty is a misconfiguration and fails loudly — it does not
// silently revert to the default.
func (g PolishGate) Run(ctx context.Context, state cycle.State) (contract.GateResult, cycle.State) {
	start := timeNow()
	report := contract.GateReport{Gate: "polish"}
	var violations []contract.Violation

	// 1. Tests pass.
	rawCmd := defaultPolishTestCmd
	if g.TestCmdOverride != "" {
		trimmed := strings.TrimSpace(g.TestCmdOverride)
		if trimmed == "" {
			// Override was set to whitespace-only: misconfiguration — fail loudly.
			report.Passed = false
			report.Stderr = "polish_test_cmd override is set but contains only whitespace — cannot silently revert to default; set it to a valid command or leave it unset"
			report.DurationMS = elapsed(start)
			return contract.GateFailure{R: report, Reason: contract.HaltGateFailure}, state
		}
		rawCmd = trimmed
	}
	parts := strings.Fields(rawCmd)
	testCmd := exec.CommandContext(ctx, parts[0], parts[1:]...) //nolint:gosec // polish_test_cmd is an explicit cycle-owner configuration surface
	testOut, testErr := testCmd.CombinedOutput()
	if testErr != nil {
		violations = append(violations, contract.Violation{
			Type:    "polish",
			Message: truncateOutput(fmt.Sprintf("tests failed (%s): %v (%s)", rawCmd, testErr, strings.TrimSpace(string(testOut))), 500),
		})
	}

	// 2. No raw ANSI in new code (rallish-repo-specific; skipped when absent).
	// If the script does not exist in the working directory, this check is not
	// applicable to this repository and is silently skipped — it is not an error.
	const ansiScript = "scripts/check-no-raw-ansi.sh"
	if _, statErr := os.Stat(ansiScript); statErr == nil {
		if err := runCheck(ctx, "bash", ansiScript); err != nil {
			violations = append(violations, contract.Violation{
				Type:    "polish",
				Message: truncateOutput(fmt.Sprintf("raw ANSI check failed: %v", err), 500),
			})
		}
	}

	// 3. Conventional commit message ready (non-empty, contains a prefix).
	goal := strings.TrimSpace(state.NextCycleGoal)
	if goal == "" {
		violations = append(violations, contract.Violation{
			Type:    "polish",
			Message: "next_cycle_goal is empty; cannot derive conventional commit message",
		})
	} else if !hasConventionalPrefix(goal) {
		violations = append(violations, contract.Violation{
			Type:    "polish",
			Message: "next_cycle_goal lacks conventional commit prefix (feat:/fix:/refactor:/docs:/test:/chore:/sec:/ci:/build:/perf:/style:)",
		})
	}

	report.Violations = violations
	report.DurationMS = elapsed(start)

	if len(violations) > 0 {
		report.Passed = false
		report.Stderr = fmt.Sprintf("polish found %d issue(s)", len(violations))
		return contract.GateFailure{R: report, Reason: contract.HaltGateFailure}, state
	}

	report.Passed = true
	return contract.GateSuccess{R: report}, state
}

func runCheck(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // name/args are hardcoded check commands
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w (%s)", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func hasConventionalPrefix(s string) bool {
	prefixes := []string{
		"feat:", "fix:", "refactor:", "docs:", "test:",
		"chore:", "sec:", "ci:", "build:", "perf:", "style:",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
