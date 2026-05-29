package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/jazz1x/rallish/internal/budget"
	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/internal/cycle/gates"
	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/spf13/cobra"
)

// cycle_run.go implements `cycle run --once`: a bounded, NON-watching, single
// orchestration pass over a persisted cycle.
//
// REFERENCE DRIVER, NOT THE PRODUCT LOOP. Per the north-star ("not the loop";
// the harness owns the guarded graph, not the traversal), this is the clean
// invocation a cron / scheduler / CI job calls. A pure harness can only halt;
// it cannot prevent revival — so an external driver re-triggers, and THIS is
// what it should re-trigger. The command:
//   - loads (resumes) state via StateFileSync (atomic write + .bak recovery — G1),
//   - runs the SAME G5 guards the orchestrator runs, by CALLING the public
//     primitives (LedgerSealsResume / Stuck / budget.ExceedsLifetimeCeiling) —
//     it does not re-implement the loop,
//   - runs exactly ONE gated cycle step (cycle.Driver.Step), then EXITS,
//   - returns an exit code derived from the terminal halt reason via the single
//     SSOT map exitCodeForHalt.
//
// It does NOT watch SSE and does NOT loop. The agent's own work happens
// out-of-band; this driver advances the guarded graph one bounded step.

// Exit codes for `cycle run --once`. SINGLE SOURCE OF TRUTH — every halt class
// maps here and nowhere else. Base offset 10 keeps these distinct from the
// process-level codes in cmd/rallish/main.go (1 generic, 2 baton-timeout,
// 64 EX_USAGE) and from shell conventions.
const (
	exitCleanPass = 0 // cycle advanced, or nothing to do (not halted)

	exitHaltStuck         = 10
	exitHaltBudget        = 11
	exitHaltPreflight     = 12
	exitHaltGateFailure   = 13
	exitHaltUnparseable   = 14
	exitHaltUserRequested = 15
	exitHaltSelfAudit     = 16
	exitHaltSSHAuth       = 17
	exitHaltMaxCycles     = 18
	exitHaltUnknownReason = 19 // a halt reason with no explicit mapping (forward-compat)
)

// exitCodeForHalt maps a terminal HaltReason to a process exit code. This is the
// ONE switch; callers and docs reference it, never a scattered literal.
//
// HaltSuccess and HaltMaxCyclesReached are terminal-but-not-failures: a cron
// reading the code learns the run is *done* (no point re-triggering), but they
// are not error conditions. HaltSuccess therefore maps to 0; HaltMaxCyclesReached
// gets its own non-zero code so a scheduler can distinguish "finished all the
// work it had" from "capped out and may want a new objective".
func exitCodeForHalt(reason contract.HaltReason) int {
	switch reason {
	case contract.HaltSuccess:
		return exitCleanPass
	case contract.HaltStuck:
		return exitHaltStuck
	case contract.HaltBudgetExceeded:
		return exitHaltBudget
	case contract.HaltPreflightFailed:
		return exitHaltPreflight
	case contract.HaltGateFailure:
		return exitHaltGateFailure
	case contract.HaltUnparseableTurn:
		return exitHaltUnparseable
	case contract.HaltUserRequested:
		return exitHaltUserRequested
	case contract.HaltSelfAuditViolation:
		return exitHaltSelfAudit
	case contract.HaltSSHAuthFailed:
		return exitHaltSSHAuth
	case contract.HaltMaxCyclesReached:
		return exitHaltMaxCycles
	default:
		return exitHaltUnknownReason
	}
}

// exitCodeError carries an explicit process exit code out of a cobra RunE so the
// root command in main.go can honour it without growing a halt-reason switch of
// its own (it already special-cases a few sentinel errors; this adds one generic
// branch via errors.As on the ExitCode() method).
type exitCodeError struct {
	code   int
	reason contract.HaltReason
}

func (e *exitCodeError) Error() string {
	return fmt.Sprintf("cycle halted: %s (exit %d)", e.reason, e.code)
}

// ExitCode reports the process exit code this error should produce.
func (e *exitCodeError) ExitCode() int { return e.code }

// CycleRunCmd returns the `cycle run` parent with the `--once` one-shot.
func CycleRunCmd() *cobra.Command {
	var (
		cycleID          string
		goal             string
		stateDir         string
		once             bool
		maxLifetimeTurns int
		stepTimeout      time.Duration
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a single bounded cycle pass (reference driver for cron/CI; not a watch loop)",
		Long: strings.TrimSpace(`
Run ONE bounded, non-watching cycle pass and exit with a code derived from the
terminal halt reason.

This is a REFERENCE DRIVER, not rallish's product loop. It is the clean
invocation a cron job, scheduler, or CI step calls to advance a persisted cycle
exactly one guarded step. It resumes state from disk (atomic + .bak recovery),
runs the same G5 anti-spin guards the orchestrator runs (sticky-halt reviver,
stuck-breaker, hard lifetime cost ceiling), runs one gated cycle step, then
exits. It never watches the event stream and never loops; an external driver
decides whether to re-trigger based on the exit code.

Exit codes:
  0   clean pass (advanced) or nothing to do (not halted) or success
  10  stuck
  11  budget-exceeded (lifetime turn ceiling)
  12  preflight-failed
  13  gate-failure
  14  unparseable-turn
  15  user-requested halt
  16  self-audit-violation
  17  ssh-auth-failed
  18  max-cycles-reached
  19  halt with no explicit mapping
  (1  operational error, e.g. unreadable state — from the root command)`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !once {
				return fmt.Errorf("cycle run requires --once (the only supported, bounded mode; this is not a watch loop)")
			}
			opts := runOnceOptions{
				cycleID:          cycleID,
				goal:             goal,
				stateDir:         stateDir,
				maxLifetimeTurns: maxLifetimeTurns,
				stepTimeout:      stepTimeout,
				pipeline:         buildCLIPipeline,
			}
			return runCycleOnce(cmd.Context(), opts, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&cycleID, "cycle-id", "", "Cycle ID to advance (required)")
	cmd.Flags().StringVar(&goal, "goal", "", "Override next_cycle_goal for this single pass")
	cmd.Flags().StringVar(&stateDir, "state-dir", "tmp", "Directory holding cycle-<id>.json and cycle-<id>-ledger.jsonl")
	cmd.Flags().BoolVar(&once, "once", false, "Run a single bounded pass then exit (required)")
	cmd.Flags().IntVar(&maxLifetimeTurns, "max-lifetime-turns", 0, "Hard ceiling on agent turns summed across revivals (0 = unlimited)")
	cmd.Flags().DurationVar(&stepTimeout, "step-timeout", 10*time.Minute, "Maximum duration for the single gated step")
	_ = cmd.MarkFlagRequired("cycle-id")
	return cmd
}

// buildCLIPipeline is the default gate pipeline for the local one-shot, mirroring
// the broker's standard pipeline plus any repo-local gates appended after audit.
func buildCLIPipeline(state cycle.State) cycle.Pipeline {
	base := cycle.Pipeline{
		gates.PreflightGate{},
		gates.AuditGate{},
		gates.PhilosophyGate{},
		gates.PolishGate{},
		gates.CommitGate{},
	}
	if len(state.LocalGates) == 0 {
		return base
	}
	pipeline := make(cycle.Pipeline, 0, len(base)+len(state.LocalGates))
	for _, gate := range base {
		pipeline = append(pipeline, gate)
		if gate.Name() != "audit" {
			continue
		}
		for _, rawCmd := range state.LocalGates {
			pipeline = append(pipeline, gates.CommandGate{RawCmd: rawCmd})
		}
	}
	return pipeline
}

// runOnceOptions bundles the inputs to a single bounded pass. pipeline is
// injectable so tests can supply a deterministic gate (the real default builds
// the git-backed standard pipeline).
type runOnceOptions struct {
	cycleID          string
	goal             string
	stateDir         string
	maxLifetimeTurns int
	stepTimeout      time.Duration
	pipeline         func(cycle.State) cycle.Pipeline
}

// runCycleOnce executes one bounded, non-watching cycle pass. It is the body of
// `cycle run --once`, factored out for testing. Terminal halts are returned as
// an *exitCodeError so the root command propagates the documented exit code; a
// clean advance (or a not-advanceable / success state) returns nil (exit 0).
func runCycleOnce(ctx context.Context, opts runOnceOptions, out io.Writer) error {
	statePath := filepath.Join(opts.stateDir, fmt.Sprintf("cycle-%s.json", opts.cycleID))
	ledgerPath := filepath.Join(opts.stateDir, fmt.Sprintf("cycle-%s-ledger.jsonl", opts.cycleID))

	stateSync := cycle.NewStateFileSync(statePath)
	ledger := cycle.NewLedgerFileSync(ledgerPath)

	// Resume: load persisted state (atomic + .bak recovery is inside Read()).
	state, err := stateSync.Read()
	if err != nil {
		return fmt.Errorf("read cycle state %q: %w", statePath, err)
	}
	if state.ID == "" {
		return fmt.Errorf("cycle %q not found at %s", opts.cycleID, statePath)
	}

	// An already-halted cycle: report its sticky reason and exit with that code.
	// Do not re-run a sealed cycle (zombie prevention is the orchestrator's job;
	// here we simply surface the terminal reason for the scheduler).
	if state.Halted {
		return reportHalt(state.HaltReason, statePath, out)
	}

	// Terminal-but-not-halted: max cycles / duration reached. Nothing to do.
	if !state.CanAdvance() {
		_, _ = fmt.Fprintf(out, "cycle %s: not advanceable (completed %d/%d); nothing to do\n",
			state.ID, state.CompletedCycles, state.MaxCycles)
		return nil // exit 0
	}

	// G5 anti-spin guards — CALL the same public primitives the orchestrator uses
	// (no new loop engine). The append-only ledger persists across revivals, so a
	// re-triggered cron pass sees a sealed cycle and refuses, and a productive
	// runaway trips the lifetime ceiling. All three seal a cycle_halted entry so
	// the next invocation's reviver guard stays sticky.
	entries, lerr := ledger.ReadAll()
	if lerr != nil {
		return fmt.Errorf("read cycle ledger %q: %w", ledgerPath, lerr)
	}
	if reason, sealed := cycle.LedgerSealsResume(entries); sealed {
		// Already sealed by a prior halt with no later progress — refuse to revive.
		return reportHalt(reason, statePath, out)
	}
	if reason, stuck := cycle.Stuck(entries); stuck {
		return haltOnce(stateSync, ledger, state, reason, statePath, out)
	}
	if budget.ExceedsLifetimeCeiling(entries, opts.maxLifetimeTurns) {
		return haltOnce(stateSync, ledger, state, contract.HaltBudgetExceeded, statePath, out)
	}

	// One bounded gated step. Optional per-pass goal override (the agent set the
	// goal out-of-band, or the scheduler supplies it).
	if strings.TrimSpace(opts.goal) != "" {
		state.NextCycleGoal = opts.goal
	}

	driver := cycle.NewCycleDriver(stateSync)
	driver.SetPipeline(opts.pipeline(state))
	if opts.stepTimeout > 0 {
		driver.StepTimeout = opts.stepTimeout
	}

	result := driver.Step(ctx, state)
	if result.IsFailure() {
		var he *cycle.HaltedError
		if errors.As(result.Err(), &he) {
			return haltOnce(stateSync, ledger, result.Value(), he.Reason, statePath, out)
		}
		// A non-halt operational failure (e.g. missing goal): propagate as a plain
		// error → the root command's generic exit 1, not a halt code.
		return fmt.Errorf("cycle step: %w", result.Err())
	}

	advanced := result.Value()
	if err := stateSync.Write(advanced); err != nil {
		return fmt.Errorf("write advanced state: %w", err)
	}
	_, _ = fmt.Fprintf(out, "cycle %s: advanced (completed %d/%d, phase %s)\n",
		advanced.ID, advanced.CompletedCycles, advanced.MaxCycles, advanced.Phase)
	return nil // exit 0 — clean single pass
}

// haltOnce seals the cycle: persists the halted state and records a single
// cycle_halted ledger entry (mirroring the orchestrator's halt path via the same
// public primitives) so the NEXT invocation's reviver guard sees a sticky halt.
// It returns the *exitCodeError carrying the documented code.
func haltOnce(
	stateSync *cycle.StateFileSync,
	ledger *cycle.LedgerFileSync,
	state cycle.State,
	reason contract.HaltReason,
	statePath string,
	out io.Writer,
) error {
	halted := state.Halt(reason).Value()
	if err := ledger.Append(contract.NewHarnessLedgerEntry(
		time.Now().UnixMilli(), halted.ID, contract.LedgerEventCycleHalted, string(reason), nil)); err != nil {
		return fmt.Errorf("append cycle_halted ledger entry: %w", err)
	}
	if err := stateSync.Write(halted); err != nil {
		return fmt.Errorf("write halted state: %w", err)
	}
	return reportHalt(reason, statePath, out)
}

// reportHalt prints the terminal reason and returns the exit-code error (or nil
// for a success/clean terminal, which is exit 0).
func reportHalt(reason contract.HaltReason, statePath string, out io.Writer) error {
	code := exitCodeForHalt(reason)
	_, _ = fmt.Fprintf(out, "cycle halted: %s (exit %d) [state: %s]\n", reason, code, statePath)
	if code == exitCleanPass {
		return nil
	}
	return &exitCodeError{code: code, reason: reason}
}
