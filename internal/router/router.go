// Package router decides which role receives the next turn in a rallish session.
package router

import (
	"context"
	"errors"
	"fmt"

	"github.com/jazz1x/rallish/pkg/contract"
)

// Router decides which role receives the next turn.
type Router struct {
	preset contract.Preset
}

// NewRouter creates a Router for the given preset.
func NewRouter(preset contract.Preset) *Router {
	return &Router{preset: preset}
}

// Next returns the next role ID.
func (r *Router) Next(ctx context.Context, prev *contract.TurnResponse, turn int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Blocked escalation is a safety override for every routing strategy.
	if prev != nil && prev.SelfEval == contract.SelfEvalBlocked {
		reviewerID := r.findReviewer()
		if reviewerID != "" {
			return reviewerID, nil
		}
		return "", errors.New("role blocked and no reviewer defined")
	}

	switch r.preset.Routing {
	case "round_robin", "handoff_then_round_robin":
		if prev != nil && prev.HandoffTo != "" && r.isValidRole(prev.HandoffTo) {
			return prev.HandoffTo, nil
		}
		return r.roundRobin(turn), nil
	case "strict_round_robin":
		// Strict round-robin ignores explicit handoffs and always advances in order.
		return r.roundRobin(turn), nil
	case "last_writer_wins":
		// A handoff request is still honoured as an explicit signal. Otherwise the
		// role that just wrote continues to hold the baton, inferred from the
		// previous turn's round-robin slot. This keeps the same writer active
		// until it explicitly hands off.
		if prev != nil && prev.HandoffTo != "" && r.isValidRole(prev.HandoffTo) {
			return prev.HandoffTo, nil
		}
		if prev != nil {
			return r.roundRobin(turn - 1), nil
		}
		return r.roundRobin(turn), nil
	default:
		return "", fmt.Errorf("routing rule %q not supported", r.preset.Routing)
	}
}

func (r *Router) isValidRole(roleID string) bool {
	for _, role := range r.preset.Roles {
		if role.ID == roleID {
			return true
		}
	}
	return false
}

func (r *Router) findReviewer() string {
	for _, role := range r.preset.Roles {
		if role.ID == "reviewer" {
			return role.ID
		}
	}
	return ""
}

func (r *Router) roundRobin(turn int) string {
	if len(r.preset.Roles) == 0 {
		return ""
	}
	idx := (turn - 1) % len(r.preset.Roles)
	if idx < 0 {
		idx = 0
	}
	return r.preset.Roles[idx].ID
}
