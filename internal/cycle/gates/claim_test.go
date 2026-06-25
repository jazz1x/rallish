package gates

import (
	"context"
	"testing"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/stretchr/testify/require"
)

func TestClaimGate_NoClaims(t *testing.T) {
	state := cycle.State{CycleState: contract.CycleState{ID: "c1"}}
	gate := ClaimGate{}
	result, _ := gate.Run(context.Background(), state)
	require.IsType(t, contract.GateSuccess{}, result)
	report := result.Report()
	require.Equal(t, "claim", report.Gate)
	require.True(t, report.Passed)
}

func TestClaimGate_Verified(t *testing.T) {
	state := cycle.State{CycleState: contract.CycleState{ID: "c1", ViolationsFound: []contract.Violation{
		{
			Type:    "ssot",
			Message: "docs mention cross-check",
			Check:   &contract.ViolationCheck{Command: "echo cross-check", Expected: "cross-check"},
		},
	}}}
	gate := ClaimGate{}
	result, _ := gate.Run(context.Background(), state)
	require.IsType(t, contract.GateSuccess{}, result)
	report := result.Report()
	require.Len(t, report.ClaimChecks, 1)
	require.True(t, report.ClaimChecks[0].Verified)
}

func TestClaimGate_Falsified(t *testing.T) {
	state := cycle.State{CycleState: contract.CycleState{ID: "c1", ViolationsFound: []contract.Violation{
		{
			Type:    "rop",
			Message: "no raw ansi escapes",
			Check:   &contract.ViolationCheck{Command: "echo hello", Expected: "notfound"},
		},
	}}}
	gate := ClaimGate{}
	result, _ := gate.Run(context.Background(), state)
	require.IsType(t, contract.GateFailure{}, result)
	report := result.Report()
	require.Len(t, report.ClaimChecks, 1)
	require.False(t, report.ClaimChecks[0].Verified)
}

func TestClaimGate_SkipsUnchecked(t *testing.T) {
	state := cycle.State{CycleState: contract.CycleState{ID: "c1", ViolationsFound: []contract.Violation{
		{Type: "note", Message: "unverified observation"},
	}}}
	gate := ClaimGate{}
	result, _ := gate.Run(context.Background(), state)
	require.IsType(t, contract.GateSuccess{}, result)
	require.Empty(t, result.Report().ClaimChecks)
}
