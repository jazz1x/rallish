// Package gates implements the autonomous-cycle gate pipeline.
package gates

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
)

// AuditGate runs the project's local test suite and linter.
// It enforces the rule: local gates must be green BEFORE commit.
type AuditGate struct{}

// Name returns the canonical gate name.
func (AuditGate) Name() string { return "audit" }

// Run executes `make check-all` (or a project-specific audit command).
func (AuditGate) Run(ctx context.Context, state cycle.State) (contract.GateResult, cycle.State) {
	start := timeNow()
	report := contract.GateReport{Gate: "audit"}

	cmd := exec.CommandContext(ctx, "make", "check-all")
	out, err := cmd.CombinedOutput()
	report.Stdout = string(out)
	report.DurationMS = elapsed(start)

	if err != nil {
		report.Passed = false
		report.Stderr = fmt.Sprintf("make check-all failed: %v", err)
		return contract.GateFailure{R: report, Reason: contract.HaltGateFailure}, state
	}

	report.Passed = true
	return contract.GateSuccess{R: report}, state
}

// LocalAuditGate runs a lighter subset (`make check`) for faster feedback during development.
type LocalAuditGate struct{}

// Name returns the canonical gate name.
func (LocalAuditGate) Name() string { return "local-audit" }

// Run executes `make check`.
func (LocalAuditGate) Run(ctx context.Context, state cycle.State) (contract.GateResult, cycle.State) {
	start := timeNow()
	report := contract.GateReport{Gate: "local-audit"}

	cmd := exec.CommandContext(ctx, "make", "check")
	out, err := cmd.CombinedOutput()
	report.Stdout = string(out)
	report.DurationMS = elapsed(start)

	if err != nil {
		report.Passed = false
		report.Stderr = fmt.Sprintf("make check failed: %v", err)
		return contract.GateFailure{R: report, Reason: contract.HaltGateFailure}, state
	}

	report.Passed = true
	return contract.GateSuccess{R: report}, state
}
