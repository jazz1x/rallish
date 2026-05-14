package exit

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/jazz1x/hocketty/pkg/contract"
)

// Clock abstracts time for testability.
type Clock interface {
	Now() time.Time
}

// Evaluator checks session exit conditions.
type Evaluator struct {
	allowShell bool
	cmdMap     map[string][]string
	clock      Clock
}

// NewEvaluator creates a new Evaluator.
func NewEvaluator(allowShell bool) *Evaluator {
	return &Evaluator{
		allowShell: allowShell,
		cmdMap: map[string][]string{
			string(contract.ExitTestsPass):           {"go", "test", "./..."},
			string(contract.ExitAllArtifactsCompile): {"go", "vet", "./..."},
		},
		clock: &realClock{},
	}
}

// WithClock sets the clock for testing.
func (e *Evaluator) WithClock(c Clock) *Evaluator {
	e.clock = c
	return e
}

type realClock struct{}

func (r *realClock) Now() time.Time { return time.Now() }

// State holds the current session state for evaluation.
type State struct {
	TurnCount       int
	Usage           contract.Usage
	Budget          contract.Budget
	StartTime       time.Time
	LastResponse    *contract.TurnResponse
	DeadlineMinutes int
}

// Evaluate iterates conditions in order and returns true if any match.
func (e *Evaluator) Evaluate(ctx context.Context, state State, conditions []contract.ExitCondition) (bool, string, error) {
	for _, cond := range conditions {
		if err := ctx.Err(); err != nil {
			return false, "", err
		}
		matched, reason, err := e.evaluateOne(ctx, state, cond)
		if err != nil {
			return false, "", err
		}
		if matched {
			return true, reason, nil
		}
	}
	return false, "", nil
}

func (e *Evaluator) evaluateOne(ctx context.Context, state State, cond contract.ExitCondition) (bool, string, error) {
	switch cond {
	case contract.ExitTurnsExhausted:
		if state.Budget.TurnsLeft <= 0 {
			return true, "turns exhausted", nil
		}
	case contract.ExitTokensExhausted:
		if state.Budget.TokensLeft <= 0 {
			return true, "tokens exhausted", nil
		}
	case contract.ExitDeadlinePassed:
		deadline := state.StartTime.Add(time.Duration(state.DeadlineMinutes) * time.Minute)
		if e.clock.Now().After(deadline) {
			return true, "deadline passed", nil
		}
	case contract.ExitReviewerApproved:
		if state.LastResponse != nil && state.LastResponse.SelfEval == contract.SelfEvalConfident && state.LastResponse.Done {
			return true, "reviewer approved", nil
		}
	case contract.ExitTestsPass, contract.ExitAllArtifactsCompile:
		if !e.allowShell {
			return false, "", fmt.Errorf("shell predicate %q disabled", cond)
		}
		return e.runShellPredicate(ctx, cond)
	default:
		return false, "", nil
	}
	return false, "", nil
}

func (e *Evaluator) runShellPredicate(ctx context.Context, cond contract.ExitCondition) (bool, string, error) {
	args, ok := e.cmdMap[string(cond)]
	if !ok || len(args) == 0 {
		return false, "", fmt.Errorf("no command mapped for predicate %q", cond)
	}

	//nolint:gosec // commands come from a predefined map
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if err := cmd.Run(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false, "", err
		}
		return false, "", nil
	}
	return true, string(cond) + " satisfied", nil
}
