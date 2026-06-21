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

// CommandGate runs an arbitrary shell command as a cycle gate.
// The command is split on whitespace; the first token is the executable.
// Designed for per-project local checks ("bun run tsc:check", "cargo clippy", etc.)
// that differ across repos and cannot be hardcoded in AuditGate.
type CommandGate struct {
	// RawCmd is the whitespace-separated command string.
	RawCmd string
}

// Name returns a stable gate name derived from the command string.
func (g CommandGate) Name() string { return "cmd:" + g.RawCmd }

// Run executes the command and returns GateFailure on non-zero exit.
func (g CommandGate) Run(ctx context.Context, state cycle.State) (contract.GateResult, cycle.State) {
	start := timeNow()
	report := contract.GateReport{Gate: g.Name()}

	parts := strings.Fields(g.RawCmd)
	if len(parts) == 0 {
		report.Passed = false
		report.Stderr = "empty command"
		report.DurationMS = elapsed(start)
		return contract.GateFailure{R: report, Reason: contract.HaltGateFailure}, state
	}

	return runShellGate(ctx, parts, fmt.Sprintf("command %q", g.RawCmd), report, start, state)
}

// runShellGate executes argv (argv[0] is the executable) for a command-style
// gate, folding combined output, duration, and pass/fail into report. label is
// the command description used in the "<label> failed: <err>" Stderr message on
// non-zero exit. It returns GateSuccess on exit 0 and GateFailure
// (HaltGateFailure) otherwise. The caller supplies report (with Gate set) and
// start so each gate keeps its own name and timing window.
func runShellGate(ctx context.Context, argv []string, label string, report contract.GateReport, start int64, state cycle.State) (contract.GateResult, cycle.State) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // gate commands are an explicit cycle-owner configuration surface
	out, err := cmd.CombinedOutput()
	report.Stdout = truncateOutput(string(out), 4000)
	report.DurationMS = elapsed(start)

	if err != nil {
		report.Passed = false
		report.Stderr = fmt.Sprintf("%s failed: %v", label, err)
		return contract.GateFailure{R: report, Reason: contract.HaltGateFailure}, state
	}

	report.Passed = true
	return contract.GateSuccess{R: report}, state
}
