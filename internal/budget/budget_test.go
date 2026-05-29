package budget

import (
	"testing"
	"time"

	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/stretchr/testify/require"
)

type fakeClock struct {
	t time.Time
}

func (f *fakeClock) Now() time.Time { return f.t }

func TestBudgeter_Remaining(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	b := NewBudgeter(clock)

	initial := contract.Budget{TokensLeft: 1000, TurnsLeft: 10, DeadlineMS: 60000}
	used := contract.Usage{TokensIn: 100, TokensOut: 50, Ms: 1000}

	got := b.Remaining(initial, used, 3)
	require.Equal(t, int64(850), got.TokensLeft)
	require.Equal(t, 7, got.TurnsLeft)
}

func TestBudgeter_IsExhausted(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	b := NewBudgeter(clock)

	require.True(t, b.IsExhausted(contract.Budget{TokensLeft: 0, TurnsLeft: 5}))
	require.True(t, b.IsExhausted(contract.Budget{TokensLeft: 10, TurnsLeft: 0}))
	require.True(t, b.IsExhausted(contract.Budget{TokensLeft: 0, TurnsLeft: 0}))
	require.False(t, b.IsExhausted(contract.Budget{TokensLeft: 10, TurnsLeft: 5}))
}

func TestBudgeter_IsDeadlinePassed(t *testing.T) {
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{t: now}
	b := NewBudgeter(clock)

	start := now.Add(-30 * time.Minute)
	require.True(t, b.IsDeadlinePassed(start, 20))

	start = now.Add(-10 * time.Minute)
	require.False(t, b.IsDeadlinePassed(start, 20))

	// Zero deadline means no deadline
	require.False(t, b.IsDeadlinePassed(start, 0))
}

func turn(at int64) contract.HarnessLedgerEntry {
	return contract.NewAgentTurnLedgerEntry(at, "cyc_b", "builder", contract.TurnResponse{Summary: "work"})
}

func TestLifetimeTurns(t *testing.T) {
	entries := []contract.HarnessLedgerEntry{
		contract.NewHarnessLedgerEntry(1, "cyc_b", contract.LedgerEventCycleCreated, "created", nil),
		turn(2),
		contract.NewHarnessLedgerEntry(3, "cyc_b", contract.LedgerEventGatePassed, "ok", nil),
		turn(4),
		contract.NewHarnessLedgerEntry(5, "cyc_b", contract.LedgerEventCycleHalted, "stuck", nil),
		turn(6),
	}
	require.Equal(t, 3, LifetimeTurns(entries))
	require.Equal(t, 0, LifetimeTurns(nil))
}

func TestExceedsLifetimeCeiling(t *testing.T) {
	entries := []contract.HarnessLedgerEntry{turn(1), turn(2), turn(3)}

	// Disabled ceiling (0 / negative) is never exceeded — preserves the
	// existing unlimited default.
	require.False(t, ExceedsLifetimeCeiling(entries, 0))
	require.False(t, ExceedsLifetimeCeiling(entries, -1))

	// Below ceiling.
	require.False(t, ExceedsLifetimeCeiling(entries, 4))

	// At the ceiling halts (>=), so the Nth turn is the last permitted.
	require.True(t, ExceedsLifetimeCeiling(entries, 3))

	// Over ceiling.
	require.True(t, ExceedsLifetimeCeiling(entries, 2))
}

func BenchmarkBudgeter_Remaining(b *testing.B) {
	clock := &fakeClock{t: time.Now()}
	budgeter := NewBudgeter(clock)
	initial := contract.Budget{TokensLeft: 100000, TurnsLeft: 1000, DeadlineMS: 60000}
	used := contract.Usage{TokensIn: 50000, TokensOut: 25000, Ms: 10000}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = budgeter.Remaining(initial, used, 500)
	}
}
