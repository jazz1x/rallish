// Package gates implements the autonomous-cycle gate pipeline.
package gates

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
)

// PreflightGate validates that the repository is in a safe state before work begins.
type PreflightGate struct{}

// Name returns the canonical gate name.
func (PreflightGate) Name() string { return "preflight" }

// Run checks branch safety, working tree cleanliness, goal presence, and SSH auth.
func (PreflightGate) Run(ctx context.Context, state cycle.State) (contract.GateResult, cycle.State) {
	start := timeNow()
	report := contract.GateReport{Gate: "preflight"}

	// 1. Branch safety: never main or master.
	branch, err := gitBranch(ctx)
	if err != nil {
		report.Passed = false
		report.Stderr = err.Error()
		report.DurationMS = elapsed(start)
		return contract.GateFailure{R: report, Reason: contract.HaltPreflightFailed}, state
	}
	if branch == "main" || branch == "master" {
		report.Passed = false
		report.Stderr = fmt.Sprintf("branch %q is forbidden", branch)
		report.DurationMS = elapsed(start)
		return contract.GateFailure{R: report, Reason: contract.HaltPreflightFailed}, state
	}
	state.Branch = branch

	// 2. Working tree cleanliness.
	clean, err := gitIsClean(ctx)
	if err != nil {
		report.Passed = false
		report.Stderr = err.Error()
		report.DurationMS = elapsed(start)
		return contract.GateFailure{R: report, Reason: contract.HaltPreflightFailed}, state
	}
	if !clean {
		report.Passed = false
		report.Stderr = "working tree is not clean; commit or stash changes first"
		report.DurationMS = elapsed(start)
		return contract.GateFailure{R: report, Reason: contract.HaltPreflightFailed}, state
	}

	// 3. Baseline SHA.
	sha, err := gitHead(ctx)
	if err != nil {
		report.Passed = false
		report.Stderr = err.Error()
		report.DurationMS = elapsed(start)
		return contract.GateFailure{R: report, Reason: contract.HaltPreflightFailed}, state
	}
	if state.BaselineSHA == "" {
		state.BaselineSHA = sha
	}

	// 4. Goal presence.
	if strings.TrimSpace(state.NextCycleGoal) == "" {
		report.Passed = false
		report.Stderr = "next_cycle_goal is empty"
		report.DurationMS = elapsed(start)
		return contract.GateFailure{R: report, Reason: contract.HaltPreflightFailed}, state
	}

	// 5. SSH auth (best-effort; failure is a warning, not a hard halt).
	if err := sshAuthCheck(ctx); err != nil {
		report.Stderr = err.Error()
		report.Passed = true
		report.DurationMS = elapsed(start)
		return contract.GateWarning{R: report}, state
	}

	report.Passed = true
	report.DurationMS = elapsed(start)
	return contract.GateSuccess{R: report}, state
}

func gitBranch(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "branch", "--show-current").Output()
	if err != nil {
		return "", fmt.Errorf("git branch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func gitIsClean(ctx context.Context) (bool, error) {
	out, err := exec.CommandContext(ctx, "git", "status", "--porcelain").Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return strings.TrimSpace(string(out)) == "", nil
}

func gitHead(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func sshAuthCheck(ctx context.Context) error {
	// Attempt GitHub first; if remote points elsewhere, detect it.
	remote, err := exec.CommandContext(ctx, "git", "remote", "get-url", "origin").Output()
	if err == nil {
		url := strings.TrimSpace(string(remote))
		host := "github.com"
		if strings.Contains(url, "gitlab.com") {
			host = "gitlab.com"
		} else if strings.Contains(url, "bitbucket.org") {
			host = "bitbucket.org"
		}
		cmd := exec.CommandContext(ctx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "git@"+host)
		out, err := cmd.CombinedOutput()
		// ssh -T exits 1 on success with "successfully authenticated" banner.
		if err != nil && !strings.Contains(string(out), "successfully authenticated") {
			return fmt.Errorf("ssh auth to %s: %w (%s)", host, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	// No origin remote; skip SSH check.
	return nil
}

// timeNow and elapsed are thin helpers for timing; they keep the gate code testable.
func timeNow() int64 { return time.Now().UnixMilli() }

func elapsed(start int64) int64 { return time.Now().UnixMilli() - start }
