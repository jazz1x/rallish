package router

import (
	"context"
	"testing"

	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/stretchr/testify/require"
)

func TestRouter_Next_RoundRobin(t *testing.T) {
	ctx := context.Background()
	preset := contract.Preset{
		Routing: "round_robin",
		Roles: []contract.Role{
			{ID: "a", Runtime: "x"},
			{ID: "b", Runtime: "y"},
			{ID: "c", Runtime: "z"},
		},
	}
	r := NewRouter(preset)

	tests := []struct {
		turn int
		want string
	}{
		{1, "a"},
		{2, "b"},
		{3, "c"},
		{4, "a"},
		{5, "b"},
	}
	for _, tt := range tests {
		got, err := r.Next(ctx, nil, tt.turn)
		require.NoError(t, err)
		require.Equal(t, tt.want, got)
	}
}

func TestRouter_Next_Handoff(t *testing.T) {
	ctx := context.Background()
	preset := contract.Preset{
		Routing: "round_robin",
		Roles: []contract.Role{
			{ID: "a", Runtime: "x"},
			{ID: "b", Runtime: "y"},
		},
	}
	r := NewRouter(preset)

	// Handoff to valid role
	prev := &contract.TurnResponse{HandoffTo: "b"}
	got, err := r.Next(ctx, prev, 1)
	require.NoError(t, err)
	require.Equal(t, "b", got)

	// Handoff to invalid role falls through to round_robin
	prev = &contract.TurnResponse{HandoffTo: "z"}
	got, err = r.Next(ctx, prev, 1)
	require.NoError(t, err)
	require.Equal(t, "a", got)
}

func TestRouter_Next_BlockedEscalation(t *testing.T) {
	ctx := context.Background()
	preset := contract.Preset{
		Routing: "round_robin",
		Roles: []contract.Role{
			{ID: "executor", Runtime: "x"},
			{ID: "reviewer", Runtime: "y"},
		},
	}
	r := NewRouter(preset)

	prev := &contract.TurnResponse{SelfEval: contract.SelfEvalBlocked}
	got, err := r.Next(ctx, prev, 1)
	require.NoError(t, err)
	require.Equal(t, "reviewer", got)
}

func TestRouter_Next_BlockedNoReviewer(t *testing.T) {
	ctx := context.Background()
	preset := contract.Preset{
		Routing: "round_robin",
		Roles: []contract.Role{
			{ID: "executor", Runtime: "x"},
		},
	}
	r := NewRouter(preset)

	prev := &contract.TurnResponse{SelfEval: contract.SelfEvalBlocked}
	_, err := r.Next(ctx, prev, 1)
	require.Error(t, err)
}

func TestRouter_Next_HandoffThenRoundRobin(t *testing.T) {
	ctx := context.Background()
	preset := contract.Preset{
		Routing: "handoff_then_round_robin",
		Roles: []contract.Role{
			{ID: "planner", Runtime: "x"},
			{ID: "executor", Runtime: "y"},
			{ID: "reviewer", Runtime: "z"},
		},
	}
	r := NewRouter(preset)

	// (c) No prior response starts round_robin at roles[0].
	got, err := r.Next(ctx, nil, 1)
	require.NoError(t, err)
	require.Equal(t, "planner", got)

	// (a) Valid handoff wins over round_robin.
	prev := &contract.TurnResponse{HandoffTo: "reviewer"}
	got, err = r.Next(ctx, prev, 2)
	require.NoError(t, err)
	require.Equal(t, "reviewer", got)

	// Without a prior handoff, the next turn falls through to plain turn-indexed
	// round_robin: (turn-1) % len(roles). For turn=3 with 3 roles that is index 2.
	got, err = r.Next(ctx, nil, 3)
	require.NoError(t, err)
	require.Equal(t, "reviewer", got)

	// (b) Handoff to unknown role falls back to round_robin.
	prev = &contract.TurnResponse{HandoffTo: "unknown"}
	got, err = r.Next(ctx, prev, 2)
	require.NoError(t, err)
	require.Equal(t, "executor", got)
}

func TestRouter_Next_StrictRoundRobin(t *testing.T) {
	ctx := context.Background()
	preset := contract.Preset{
		Routing: "strict_round_robin",
		Roles: []contract.Role{
			{ID: "a", Runtime: "x"},
			{ID: "b", Runtime: "y"},
			{ID: "c", Runtime: "z"},
		},
	}
	r := NewRouter(preset)

	// Without a handoff the behaviour is identical to round_robin.
	got, err := r.Next(ctx, nil, 1)
	require.NoError(t, err)
	require.Equal(t, "a", got)

	// A handoff request is ignored; the turn index still decides.
	prev := &contract.TurnResponse{HandoffTo: "c"}
	got, err = r.Next(ctx, prev, 2)
	require.NoError(t, err)
	require.Equal(t, "b", got)
}

func TestRouter_Next_LastWriterWins(t *testing.T) {
	ctx := context.Background()
	preset := contract.Preset{
		Routing: "last_writer_wins",
		Roles: []contract.Role{
			{ID: "a", Runtime: "x"},
			{ID: "b", Runtime: "y"},
			{ID: "c", Runtime: "z"},
		},
	}
	r := NewRouter(preset)

	// First turn starts at roles[0].
	got, err := r.Next(ctx, nil, 1)
	require.NoError(t, err)
	require.Equal(t, "a", got)

	// No handoff: the last writer (turn 1 -> role a) keeps the baton.
	prev := &contract.TurnResponse{}
	got, err = r.Next(ctx, prev, 2)
	require.NoError(t, err)
	require.Equal(t, "a", got)

	// A handoff request is still honoured.
	prev = &contract.TurnResponse{HandoffTo: "c"}
	got, err = r.Next(ctx, prev, 3)
	require.NoError(t, err)
	require.Equal(t, "c", got)
}
