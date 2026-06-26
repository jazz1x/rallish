package session

import (
	"testing"

	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/stretchr/testify/require"
)

func TestDryRounds_ConsecutiveDry(t *testing.T) {
	records := []TurnRecord{
		{Req: contract.TurnRequest{Role: "a"}, Resp: contract.TurnResponse{Summary: "1"}},
		{Req: contract.TurnRequest{Role: "a"}, Resp: contract.TurnResponse{Summary: "2"}},
		{Req: contract.TurnRequest{Role: "a"}, Resp: contract.TurnResponse{Summary: "3"}},
	}
	require.True(t, DryRounds(records, 3))
	require.False(t, DryRounds(records, 4))
}

func TestDryRounds_ResetOnArtifact(t *testing.T) {
	records := []TurnRecord{
		{Req: contract.TurnRequest{Role: "a"}, Resp: contract.TurnResponse{Summary: "1"}},
		{Req: contract.TurnRequest{Role: "a"}, Resp: contract.TurnResponse{Summary: "2", Artifacts: []string{"f.go"}}},
		{Req: contract.TurnRequest{Role: "a"}, Resp: contract.TurnResponse{Summary: "3"}},
	}
	require.False(t, DryRounds(records, 2))
}

func TestDryRounds_ThresholdZero(t *testing.T) {
	records := []TurnRecord{
		{Req: contract.TurnRequest{Role: "a"}, Resp: contract.TurnResponse{Summary: "1"}},
	}
	require.False(t, DryRounds(records, 0))
}

func TestStuck_NoProgress(t *testing.T) {
	records := make([]TurnRecord, 6)
	for i := range records {
		records[i] = TurnRecord{
			Req:  contract.TurnRequest{Role: "a"},
			Resp: contract.TurnResponse{Summary: "nothing"},
		}
	}
	reason, ok := Stuck(records)
	require.True(t, ok)
	require.Equal(t, "no progress", reason)
}

func TestStuck_PingPong(t *testing.T) {
	records := make([]TurnRecord, 6)
	roles := []string{"alpha", "beta", "alpha", "beta", "alpha", "beta"}
	for i, role := range roles {
		records[i] = TurnRecord{
			Req:  contract.TurnRequest{Role: role},
			Resp: contract.TurnResponse{Summary: "ping"},
		}
	}
	reason, ok := Stuck(records)
	require.True(t, ok)
	require.Contains(t, reason, "ping-pong")
}

func TestStuck_RepeatedFingerprint(t *testing.T) {
	records := []TurnRecord{
		{Req: contract.TurnRequest{Role: "a"}, Resp: contract.TurnResponse{Summary: "same", Artifacts: []string{"x.go"}}},
		{Req: contract.TurnRequest{Role: "a"}, Resp: contract.TurnResponse{Summary: "same", Artifacts: []string{"x.go"}}},
		{Req: contract.TurnRequest{Role: "a"}, Resp: contract.TurnResponse{Summary: "same", Artifacts: []string{"x.go"}}},
		{Req: contract.TurnRequest{Role: "a"}, Resp: contract.TurnResponse{Summary: "same", Artifacts: []string{"x.go"}}},
	}
	reason, ok := Stuck(records)
	require.True(t, ok)
	require.Equal(t, "repeated fingerprint", reason)
}

func TestStuck_NotStuck(t *testing.T) {
	records := []TurnRecord{
		{Req: contract.TurnRequest{Role: "a"}, Resp: contract.TurnResponse{Summary: "first", Artifacts: []string{"a.go"}}},
		{Req: contract.TurnRequest{Role: "b"}, Resp: contract.TurnResponse{Summary: "second", Artifacts: []string{"b.go"}}},
	}
	_, ok := Stuck(records)
	require.False(t, ok)
}
