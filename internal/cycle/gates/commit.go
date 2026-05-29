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

// CommitGate creates a git commit with a conventional message derived from NextCycleGoal.
// Rules: never --amend, never --no-verify.
type CommitGate struct{}

// Name returns the canonical gate name.
func (CommitGate) Name() string { return "commit" }

// Run stages all changes and commits.
func (CommitGate) Run(ctx context.Context, state cycle.State) (contract.GateResult, cycle.State) {
	start := timeNow()
	report := contract.GateReport{Gate: "commit"}

	msg := deriveCommitMessage(state.NextCycleGoal, state.CompletedCycles+1)

	// Stage all changes.
	if out, err := exec.CommandContext(ctx, "git", "add", "-A").CombinedOutput(); err != nil {
		report.Passed = false
		report.Stderr = fmt.Sprintf("git add failed: %v (%s)", err, strings.TrimSpace(string(out)))
		report.DurationMS = elapsed(start)
		return contract.GateFailure{R: report, Reason: contract.HaltGateFailure}, state
	}

	// Commit with conventional message.
	cmd := exec.CommandContext(ctx, "git", "commit", "-m", msg) //nolint:gosec // msg is derived from internal goal, not user input
	out, err := cmd.CombinedOutput()
	report.Stdout = string(out)
	report.DurationMS = elapsed(start)

	if err != nil {
		// Check for "nothing to commit" which is acceptable.
		if strings.Contains(string(out), "nothing to commit") {
			report.Passed = true
			report.Stdout = "nothing to commit, working tree clean"
			return contract.GateSuccess{R: report}, state
		}
		report.Passed = false
		report.Stderr = fmt.Sprintf("git commit failed: %v", err)
		return contract.GateFailure{R: report, Reason: contract.HaltGateFailure}, state
	}

	report.Passed = true
	return contract.GateSuccess{R: report}, state
}

func deriveCommitMessage(goal string, cycleNum int) string {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		goal = "autonomous cycle step"
	}
	// Ensure the goal already has a conventional prefix; if not, default to refactor.
	if !hasConventionalPrefix(goal) {
		goal = "refactor: " + goal
	}
	return fmt.Sprintf("%s [cycle-%d]", goal, cycleNum)
}
