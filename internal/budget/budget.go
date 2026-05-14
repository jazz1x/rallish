// Package budget tracks token, turn, and wall-clock budgets for hocketty sessions.
package budget

import (
	"time"

	"github.com/jazz1x/hocketty/pkg/contract"
)

// Clock abstracts time for testability.
type Clock interface {
	Now() time.Time
}

// Budgeter tracks token, turn, and wall-clock budgets.
type Budgeter struct {
	clock Clock
}

// NewBudgeter creates a new Budgeter.
func NewBudgeter(clock Clock) *Budgeter {
	return &Budgeter{clock: clock}
}

// Remaining returns the remaining budget after usage.
func (b *Budgeter) Remaining(initial contract.Budget, used contract.Usage, turnsUsed int) contract.Budget {
	tokensUsed := used.TokensIn + used.TokensOut
	return contract.Budget{
		TokensLeft: initial.TokensLeft - tokensUsed,
		TurnsLeft:  initial.TurnsLeft - turnsUsed,
		DeadlineMS: initial.DeadlineMS,
	}
}

// IsExhausted returns true if turns_left <= 0 or tokens_left <= 0.
func (b *Budgeter) IsExhausted(budget contract.Budget) bool {
	return budget.TurnsLeft <= 0 || budget.TokensLeft <= 0
}

// IsDeadlinePassed returns true if the deadline has passed.
func (b *Budgeter) IsDeadlinePassed(start time.Time, deadlineMinutes int) bool {
	if deadlineMinutes <= 0 {
		return false
	}
	deadline := start.Add(time.Duration(deadlineMinutes) * time.Minute)
	return b.clock.Now().After(deadline)
}
