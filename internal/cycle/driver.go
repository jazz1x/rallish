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
	sleeper        Sleeper
	StepTimeout    time.Duration
	ReadRetries    int
	goalDiscoverer func(context.Context, State) (string, error)
}

// NewCycleDriver creates a driver with the standard gate pipeline.
func NewCycleDriver(sync *StateFileSync) *Driver {
	return &Driver{
		pipeline:       StandardPipeline(),
		sync:           sync,
		sleeper:        defaultSleeper{},
		StepTimeout:    10 * time.Minute,
		ReadRetries:    3,
		goalDiscoverer: discoverNextGoal,
	}
}

// StandardPipeline returns the default ordered gate list.
func StandardPipeline() Pipeline {
	// We import gates here to avoid an import cycle.
	// gates package imports cycle; cycle must not import gates.
	// Therefore StandardPipeline lives outside cycle package or we use a registry.
	// To keep it simple, we leave pipeline construction to the caller (broker/CLI).
	return nil
}

// SetPipeline assigns a custom pipeline to the driver.
func (d *Driver) SetPipeline(p Pipeline) {
	d.pipeline = p
}

// SetSleeper assigns a custom sleeper (useful in tests).
func (d *Driver) SetSleeper(s Sleeper) {
	d.sleeper = s
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

	return Success(current, result.Reports()...)
}
