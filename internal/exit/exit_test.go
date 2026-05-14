package exit

import (
	"context"
	"testing"
	"time"

	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/stretchr/testify/require"
)

type fakeClock struct {
	t time.Time
}

func (f *fakeClock) Now() time.Time { return f.t }

func TestEvaluator_TurnsExhausted(t *testing.T) {
	e := NewEvaluator(false)
	state := State{Budget: contract.Budget{TurnsLeft: 0}}
	matched, reason, err := e.Evaluate(context.Background(), state, []contract.ExitCondition{contract.ExitTurnsExhausted})
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, "turns exhausted", reason)
}

func TestEvaluator_TokensExhausted(t *testing.T) {
	e := NewEvaluator(false)
	state := State{Budget: contract.Budget{TokensLeft: 0}}
	matched, reason, err := e.Evaluate(context.Background(), state, []contract.ExitCondition{contract.ExitTokensExhausted})
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, "tokens exhausted", reason)
}

func TestEvaluator_DeadlinePassed(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	e := NewEvaluator(false).WithClock(&fakeClock{t: now})
	state := State{
		StartTime:  now.Add(-30 * time.Minute),
		DeadlineMS: 20 * 60 * 1000,
	}
	matched, reason, err := e.Evaluate(context.Background(), state, []contract.ExitCondition{contract.ExitDeadlinePassed})
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, "deadline passed", reason)
}

func TestEvaluator_DeadlineNotPassed(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	e := NewEvaluator(false).WithClock(&fakeClock{t: now})
	state := State{
		StartTime:  now.Add(-10 * time.Minute),
		DeadlineMS: 20 * 60 * 1000,
	}
	matched, _, err := e.Evaluate(context.Background(), state, []contract.ExitCondition{contract.ExitDeadlinePassed})
	require.NoError(t, err)
	require.False(t, matched)
}

func TestEvaluator_ReviewerApproved(t *testing.T) {
	e := NewEvaluator(false)
	state := State{
		LastResponse: &contract.TurnResponse{
			SelfEval: contract.SelfEvalConfident,
			Done:     true,
		},
	}
	matched, reason, err := e.Evaluate(context.Background(), state, []contract.ExitCondition{contract.ExitReviewerApproved})
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, "reviewer approved", reason)
}

func TestEvaluator_ReviewerApproved_Missing(t *testing.T) {
	e := NewEvaluator(false)
	state := State{
		LastResponse: &contract.TurnResponse{
			SelfEval: contract.SelfEvalUncertain,
			Done:     true,
		},
	}
	matched, _, err := e.Evaluate(context.Background(), state, []contract.ExitCondition{contract.ExitReviewerApproved})
	require.NoError(t, err)
	require.False(t, matched)
}

func TestEvaluator_ShellPredicates_Disabled(t *testing.T) {
	e := NewEvaluator(false)
	_, _, err := e.Evaluate(context.Background(), State{}, []contract.ExitCondition{contract.ExitTestsPass})
	require.Error(t, err)
	require.Contains(t, err.Error(), "disabled")
}

func TestEvaluator_ShellPredicates_Enabled(t *testing.T) {
	e := NewEvaluator(true)
	e.cmdMap[string(contract.ExitTestsPass)] = []string{"true"}
	matched, reason, err := e.Evaluate(context.Background(), State{}, []contract.ExitCondition{contract.ExitTestsPass})
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, "tests_pass satisfied", reason)
}

func TestEvaluator_ShellPredicates_Fail(t *testing.T) {
	e := NewEvaluator(true)
	e.cmdMap[string(contract.ExitTestsPass)] = []string{"false"}
	matched, _, err := e.Evaluate(context.Background(), State{}, []contract.ExitCondition{contract.ExitTestsPass})
	require.NoError(t, err)
	require.False(t, matched)
}

func TestEvaluator_NoMatch(t *testing.T) {
	e := NewEvaluator(false)
	state := State{Budget: contract.Budget{TurnsLeft: 5, TokensLeft: 100}}
	matched, _, err := e.Evaluate(context.Background(), state, []contract.ExitCondition{
		contract.ExitTurnsExhausted,
		contract.ExitTokensExhausted,
	})
	require.NoError(t, err)
	require.False(t, matched)
}
