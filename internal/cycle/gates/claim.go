package gates

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
)

const claimGateTimeout = 30 * time.Second

// ClaimGate verifies any ViolationCheck commands accumulated in the cycle state.
// It runs after the audit gate and before polish so claimed defects are re-checked
// before the cycle declares green.
type ClaimGate struct{}

// Name returns the canonical gate name.
func (ClaimGate) Name() string { return "claim" }

// Run executes each claim's check command and reports verified/falsified results.
func (ClaimGate) Run(ctx context.Context, state cycle.State) (contract.GateResult, cycle.State) {
	start := time.Now()
	var checks []contract.ClaimCheck
	var combinedOut, combinedErr bytes.Buffer

	for _, v := range state.ViolationsFound {
		if v.Check == nil || v.Check.Command == "" {
			continue
		}

		verified, out, errOut := runClaimCheck(ctx, v.Check)
		_, _ = combinedOut.WriteString(out)
		_, _ = combinedErr.WriteString(errOut)
		checks = append(checks, contract.ClaimCheck{
			Violation: v,
			Verified:  verified,
		})
	}

	duration := time.Since(start).Milliseconds()
	report := contract.GateReport{
		Gate:        ClaimGate{}.Name(),
		Passed:      true,
		DurationMS:  duration,
		Stdout:      combinedOut.String(),
		Stderr:      combinedErr.String(),
		ClaimChecks: checks,
	}

	for _, c := range checks {
		if !c.Verified {
			report.Passed = false
			report.Violations = append(report.Violations, c.Violation)
		}
	}

	state.AppendReport(report)

	if !report.Passed {
		return contract.GateFailure{
			R:      report,
			Reason: contract.HaltGateFailure,
		}, state
	}
	return contract.GateSuccess{R: report}, state
}

func runClaimCheck(ctx context.Context, check *contract.ViolationCheck) (bool, string, string) {
	checkCtx, cancel := context.WithTimeout(ctx, claimGateTimeout)
	defer cancel()

	//nolint:gosec // claim checks are user-supplied reproducible verification
	// commands executed inside the gated cycle pipeline; the command is the claim.
	cmd := exec.CommandContext(checkCtx, "sh", "-c", check.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_, _ = fmt.Fprintf(&stderr, "claim check failed: %v\n", err)
		return false, stdout.String(), stderr.String()
	}
	output := stdout.String() + stderr.String()
	if check.Expected == "" {
		return true, stdout.String(), stderr.String()
	}
	return contains(output, check.Expected), stdout.String(), stderr.String()
}

func contains(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}
