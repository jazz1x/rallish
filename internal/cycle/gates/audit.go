// Package gates implements the autonomous-cycle gate pipeline.
package gates

import (
	"context"
	"strings"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
)

// defaultAuditCmd is the command run by AuditGate when no override is set.
const defaultAuditCmd = "make check-all"

// AuditGate runs the project's local test suite and linter.
// It enforces the rule: local gates must be green BEFORE commit.
//
// CmdOverride, if non-empty, replaces the default `make check-all` command.
// It is split on whitespace; the first token is the executable. An override
// that is set but contains only whitespace is a misconfiguration and causes
// the gate to fail loudly (no silent revert to default).
type AuditGate struct {
	// CmdOverride replaces the default `make check-all` when non-empty.
	// Configure via the cycle's audit_cmd config field. Leave empty to
	// use the default.
	CmdOverride string
}

// Name returns the canonical gate name.
func (AuditGate) Name() string { return "audit" }

// Run executes the audit command. The default is `make check-all`; set
// CmdOverride to use a project-specific command (e.g. "npm test",
// "cargo test"). An override that trims to empty is a misconfiguration and
// fails loudly — it does not silently revert to the default.
func (g AuditGate) Run(ctx context.Context, state cycle.State) (contract.GateResult, cycle.State) {
	start := timeNow()
	report := contract.GateReport{Gate: "audit"}

	rawCmd := defaultAuditCmd
	if g.CmdOverride != "" {
		trimmed := strings.TrimSpace(g.CmdOverride)
		if trimmed == "" {
			// Override was set to whitespace-only: misconfiguration — fail loudly.
			report.Passed = false
			report.Stderr = "audit_cmd override is set but contains only whitespace — cannot silently revert to default; set it to a valid command or leave it unset"
			report.DurationMS = elapsed(start)
			return contract.GateFailure{R: report, Reason: contract.HaltGateFailure}, state
		}
		rawCmd = trimmed
	}

	parts := strings.Fields(rawCmd)
	return runShellGate(ctx, parts, rawCmd, report, start, state)
}

// truncateOutput caps output to maxRunes to prevent state bloat in long loops.
func truncateOutput(s string, maxRunes int) string {
	if len(s) <= maxRunes {
		return s
	}
	// Keep the tail (most recent output) because errors are usually at the end.
	return "...truncated...\n" + s[len(s)-maxRunes:]
}

// LocalAuditGate runs a lighter subset (`make check`) for faster feedback during development.
type LocalAuditGate struct{}

// Name returns the canonical gate name.
func (LocalAuditGate) Name() string { return "local-audit" }

// Run executes `make check`.
func (LocalAuditGate) Run(ctx context.Context, state cycle.State) (contract.GateResult, cycle.State) {
	start := timeNow()
	report := contract.GateReport{Gate: "local-audit"}

	return runShellGate(ctx, []string{"make", "check"}, "make check", report, start, state)
}
