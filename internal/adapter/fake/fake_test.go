package fake

import (
	"context"
	"testing"

	"github.com/jazz1x/hocketty/pkg/contract"
	"github.com/stretchr/testify/require"
)

func TestFakeAdapterCallback(t *testing.T) {
	a := New(func(turn int) contract.TurnResponse {
		if turn == 0 {
			return contract.TurnResponse{Done: false, Summary: "first"}
		}
		return contract.TurnResponse{Done: true}
	})

	resp, err := a.Run(context.Background(), contract.TurnRequest{Turn: 0})
	require.NoError(t, err)
	require.False(t, resp.Done)
	require.Equal(t, "first", resp.Summary)

	resp, err = a.Run(context.Background(), contract.TurnRequest{Turn: 1})
	require.NoError(t, err)
	require.True(t, resp.Done)
}

func TestFakeAdapterNoCallback(t *testing.T) {
	a := New(nil)
	resp, err := a.Run(context.Background(), contract.TurnRequest{Turn: 5})
	require.NoError(t, err)
	require.True(t, resp.Done)
}

func TestFakeAdapterPingPong(t *testing.T) {
	a := NewPingPong(4, 0)

	// Turns 1-3: not done.
	for turn := 1; turn <= 3; turn++ {
		resp, err := a.Run(context.Background(), contract.TurnRequest{Turn: turn})
		require.NoError(t, err)
		require.False(t, resp.Done)
		require.Contains(t, resp.Summary, "completed")
	}

	// Turn 4+: done.
	resp, err := a.Run(context.Background(), contract.TurnRequest{Turn: 4})
	require.NoError(t, err)
	require.True(t, resp.Done)
	require.Equal(t, "all done", resp.Summary)
}
