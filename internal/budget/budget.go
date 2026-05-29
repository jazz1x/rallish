// Package budget tracks token, turn, and wall-clock budgets for rallish sessions.
package budget

import (
	"time"

	"github.com/jazz1x/rallish/pkg/contract"
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

// LifetimeTurns counts the agent turns recorded across the whole ledger. The
// ledger persists every agent_turn entry across process revivals, so this is a
// lifetime total (not per-process) summed straight from the append-only log —
// no extra field on the JSONL is needed.
func LifetimeTurns(entries []contract.HarnessLedgerEntry) int {
	turns := 0
	for _, e := range entries {
		if e.Type == contract.LedgerEventAgentTurn {
			turns++
		}
	}
	return turns
}

// ExceedsLifetimeCeiling reports whether the lifetime turn total has reached the
// configured hard ceiling. A ceiling <= 0 means unlimited (disabled).
//
// This is the hard cost ceiling that is DISTINCT from the stuck-breaker: a
// productive runaway emits all-novel turns that never trip Stuck(), so a hard
// bound on lifetime tool-calls/turns is the only thing that stops it. Summed
// across revivals from the ledger, it survives a cron/driver restart.
func ExceedsLifetimeCeiling(entries []contract.HarnessLedgerEntry, ceiling int) bool {
	if ceiling <= 0 {
		return false
	}
	return LifetimeTurns(entries) >= ceiling
}
