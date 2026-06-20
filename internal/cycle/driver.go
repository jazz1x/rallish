// Package cycle implements the autonomous-cycle domain layer.
package cycle

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jazz1x/rallish/pkg/contract"
)

// Sleeper abstracts sleep for testability.
type Sleeper interface {
	Sleep(ctx context.Context, d time.Duration) error
}

// NoOpSleeper returns immediately. Useful in tests to avoid real sleeps.
type NoOpSleeper struct{}

// Sleep returns immediately without blocking.
func (NoOpSleeper) Sleep(ctx context.Context, _ time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// defaultSleeper uses time.Sleep, respecting context cancellation.
type defaultSleeper struct{}

func (defaultSleeper) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Driver runs the autonomous cycle loop for a single agent.
type Driver struct {
	pipeline       Pipeline
	sync           *StateFileSync
	ledger         *LedgerFileSync
	sleeper        Sleeper
	StepTimeout    time.Duration
	ReadRetries    int
	goalDiscoverer func(context.Context, State) (string, error)
}

// NewCycleDriver creates a driver with no pipeline. The gates package imports
// cycle, so cycle cannot construct the real gate pipeline itself; the caller
// injects it via SetPipeline (CLI) or SetPipelineFactory (orchestrator) before
// the first Step. The canonical pipeline is gates.StandardPipeline.
func NewCycleDriver(sync *StateFileSync) *Driver {
	return &Driver{
		pipeline:       nil,
		sync:           sync,
		sleeper:        defaultSleeper{},
		StepTimeout:    10 * time.Minute,
		ReadRetries:    3,
		goalDiscoverer: discoverNextGoal,
	}
}

// SetPipeline assigns a custom pipeline to the driver.
func (d *Driver) SetPipeline(p Pipeline) {
	d.pipeline = p
}

// SetSleeper assigns a custom sleeper (useful in tests).
func (d *Driver) SetSleeper(s Sleeper) {
	d.sleeper = s
}

// SetLedger injects an append-only harness ledger so Step can record gate-pin
// events (G2 tamper-resistant gates). The ledger is optional; if nil, no pin
// events are recorded (silent absence, not a failure — the harness halts only
// on real gate failures).
func (d *Driver) SetLedger(l *LedgerFileSync) {
	d.ledger = l
}

// Run loops until the cycle halts or max cycles are reached.
// It sleeps 30s between cycles and retries transient read errors.
func (d *Driver) Run(ctx context.Context) error {
	for {
		state, err := d.readWithRetry()
		if err != nil {
			return fmt.Errorf("read state: %w", err)
		}
		if !state.CanAdvance() {
			return nil // graceful exit
		}

		result := d.Step(ctx, state)
		if result.IsFailure() {
			he, ok := result.Err().(*HaltedError)
			if ok {
				_ = d.sync.Write(result.Value())
				return he
			}
			return result.Err()
		}

		next := result.Value()

		if next.AutoGoal && !next.Halted && strings.TrimSpace(next.NextCycleGoal) == "" {
			goal, err := d.goalDiscoverer(ctx, next)
			if err != nil {
				return fmt.Errorf("goal discovery: %w", err)
			}
			if goal == "" {
				halted := next.Halt(contract.HaltSuccess).Value()
				_ = d.sync.Write(halted)
				return nil
			}
			next.NextCycleGoal = goal
			next.UpdatedAt = time.Now().UnixMilli()
		}

		if err := d.sync.Write(next); err != nil {
			return fmt.Errorf("write state: %w", err)
		}

		if !next.CanAdvance() {
			return nil
		}

		sleep := 30 * time.Second
		if result.IsFailure() {
			sleep = 60 * time.Second // back off on failure to avoid hammering
		}
		if err := d.sleeper.Sleep(ctx, sleep); err != nil {
			return err
		}
	}
}

func (d *Driver) readWithRetry() (State, error) {
	retries := d.ReadRetries
	if retries <= 0 {
		retries = 3
	}
	var lastErr error
	for i := 0; i < retries; i++ {
		state, err := d.sync.Read()
		if err == nil {
			return state, nil
		}
		lastErr = err
		if i < retries-1 {
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}
	return State{}, lastErr
}

// Step runs one full cycle: pipeline + complete cycle + clear goal.
// It enforces StepTimeout to prevent a single gate from blocking indefinitely.
func (d *Driver) Step(ctx context.Context, state State) Result[State] {
	if len(d.pipeline) == 0 {
		return Failure(state, fmt.Errorf("no pipeline configured"))
	}
	if strings.TrimSpace(state.NextCycleGoal) == "" {
		return Failure(state, fmt.Errorf("next_cycle_goal is empty: %w", contract.ErrGoalRequired))
	}

	stepCtx := ctx
	if d.StepTimeout > 0 {
		var cancel context.CancelFunc
		stepCtx, cancel = context.WithTimeout(ctx, d.StepTimeout)
		defer cancel()
	}

	// G2 tamper-resistant gates: pin the LocalGates command set before the
	// pipeline runs and record it in the ledger. A subsequent tamper check (same
	// cycle, before gate execution) detects in-cycle edits to the gate commands.
	//
	// DECLARE + RECORD only — no halt is issued here. The verifier/operator reads
	// the ledger and decides remediation.  Boundary: defends against edits to the
	// *declared command set*; does NOT defend against a malicious runtime that
	// falsifies gate *results* — that requires verifier ≠ executor separation.
	pinnedDigest, pins := contract.PinGates(state.LocalGates)
	if d.ledger != nil {
		_ = d.ledger.Append(contract.NewGatesPinnedEntry(
			time.Now().UnixMilli(), state.ID, pinnedDigest, pins, false))
	}

	// Pre-execution tamper check: re-hash the state's LocalGates immediately
	// before handing off to the pipeline. If an in-cycle edit to the gate
	// command set occurred between pin-time and here, the digest will differ.
	// Record a gate_tampered event — DECLARE + RECORD, the runtime/operator acts.
	if contract.GatesTampered(pinnedDigest, state.LocalGates) {
		currentDigest, currentPins := contract.PinGates(state.LocalGates)
		if d.ledger != nil {
			_ = d.ledger.Append(contract.NewGatesPinnedEntry(
				time.Now().UnixMilli(), state.ID, currentDigest, currentPins, true))
		}
	}

	// Run the gate pipeline.
	result := d.pipeline.Execute(stepCtx, state)
	if result.IsFailure() {
		return result
	}

	current := result.Value()

	// Complete the cycle: increment counter, reset phase.
	completed := current.CompleteCycle()
	if completed.IsFailure() {
		return completed
	}
	current = completed.Value()

	// Goal is consumed; caller (orchestrator or human) must set a new one before next step.
	current.NextCycleGoal = ""
	current.UpdatedAt = time.Now().UnixMilli()

	// VERIFIER-PRODUCED completion record (G4 audit completeness + G5 reviver
	// progress signal). The gate pipeline passed and the cycle completed, so the
	// *verifier* — the gates, not agent self-report — went green. Record, in order:
	//   - one gate_passed per gate report (the per-gate outcomes), then
	//   - one validation_green (the un-gameable progress signal the worker cannot
	//     write — the only thing that lifts a sticky cycle_halted seal, see
	//     LedgerSealsResume), then
	//   - one cycle_completed terminal marker.
	// These are the same events the broker step path records (gate_passed +
	// cycle_completed via handleStepCycle); validation_green closes the B1 gap
	// where the reviver's only revive-condition had no production emit site.
	//
	// Driver.Step is the SSOT success path for the one-shot reference driver and
	// the orchestrator; the broker uses its own pipeline path and does NOT call
	// Step, so there is no double-emit. Guarded by d.ledger != nil: with no ledger
	// injected the records are silently absent (additive, never a failure).
	if d.ledger != nil {
		now := time.Now().UnixMilli()
		for _, report := range result.Reports() {
			_ = d.ledger.Append(contract.NewGateLedgerEntry(now, current.ID, report))
		}
		_ = d.ledger.Append(contract.NewHarnessLedgerEntry(
			now, current.ID, contract.LedgerEventValidationGreen, "gate pipeline passed", nil))
		_ = d.ledger.Append(contract.NewHarnessLedgerEntry(
			now, current.ID, contract.LedgerEventCycleCompleted, "cycle step completed", nil))
	}

	return Success(current, result.Reports()...)
}
