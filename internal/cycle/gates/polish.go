// Package gates implements the autonomous-cycle gate pipeline.
package gates

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
)

// PolishGate runs the completion self-check table before claiming a cycle is done.
// It mirrors the /polish skill gate: tests, lint, no raw ANSI, conventional commit ready.
type PolishGate struct{}

// Name returns the canonical gate name.
func (PolishGate) Name() string { return "polish" }

// Run executes the polish checks.
func (PolishGate) Run(ctx context.Context, state cycle.State) (contract.GateResult, cycle.State) {
	start := timeNow()
	report := contract.GateReport{Gate: "polish"}
	var violations []contract.Violation

	// 1. Tests pass.
	if err := runCheck(ctx, "go", "test", "-race", "./..."); err != nil {
		violations = append(violations, contract.Violation{
			Type:    "polish",
			Message: fmt.Sprintf("tests failed: %v", err),
		})
	}

	// 2. Lint pass (golangci-lint).
	if err := runCheck(ctx, "make", "check"); err != nil {
		// make check is a superset; if it fails we note it but don't hard-fail
		// because the AuditGate already ran the full suite.
		violations = append(violations, contract.Violation{
			Type:    "polish",
			Message: truncateOutput(fmt.Sprintf("lint check: %v", err), 500),
		})
	}

	// 3. No raw ANSI in new code.
	if err := runCheck(ctx, "bash", "scripts/check-no-raw-ansi.sh"); err != nil {
		violations = append(violations, contract.Violation{
			Type:    "polish",
			Message: truncateOutput(fmt.Sprintf("raw ANSI check failed: %v", err), 500),
		})
	}

	// 4. Conventional commit message ready (non-empty, contains a prefix).
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
