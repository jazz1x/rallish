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
