package router

import (
	"context"
	"testing"

	"github.com/jazz1x/hocketty/pkg/contract"
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

func TestRouter_Next_UnsupportedRouting(t *testing.T) {
	ctx := context.Background()
	preset := contract.Preset{
		Routing: "last_writer_wins",
		Roles: []contract.Role{
			{ID: "a", Runtime: "x"},
		},
	}
	r := NewRouter(preset)

	_, err := r.Next(ctx, nil, 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}
