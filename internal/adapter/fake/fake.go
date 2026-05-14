// Package fake provides a deterministic test adapter for rallish.
package fake

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jazz1x/rallish/pkg/contract"
)

// Adapter is a test double that returns canned responses.
type Adapter struct {
	callback func(turn int) contract.TurnResponse
	delay    time.Duration
}

// New creates a fake adapter. If callback is nil, all turns return Done=true.
func New(callback func(turn int) contract.TurnResponse) *Adapter {
	return &Adapter{callback: callback}
}

// NewPingPong creates a fake adapter that simulates a ping-pong pattern.
// It returns Done=false for turns < maxTurns and Done=true afterwards,
// sleeping for delay between turns so the long-poll flow is visible.
func NewPingPong(maxTurns int, delay time.Duration) *Adapter {
	return &Adapter{
		delay: delay,
		callback: func(turn int) contract.TurnResponse {
			if turn < maxTurns {
				return contract.TurnResponse{
					Done:    false,
					Summary: fmt.Sprintf("fake turn %d completed", turn),
				}
			}
			return contract.TurnResponse{
				Done:    true,
				Summary: "all done",
			}
		},
	}
}

// Name returns the adapter name.
func (a *Adapter) Name() string { return "fake" }

// Check always succeeds.
func (a *Adapter) Check() error { return nil }

// Run returns the callback response for the given turn, or Done=true if no callback is set.
func (a *Adapter) Run(ctx context.Context, req contract.TurnRequest) (contract.TurnResponse, error) {
	if a.delay > 0 {
		slog.Info("🎭 fake adapter working", "turn", req.Turn, "role", req.Role)
		select {
		case <-time.After(a.delay):
		case <-ctx.Done():
			return contract.TurnResponse{}, ctx.Err()
		}
	}
	if a.callback != nil {
		return a.callback(req.Turn), nil
	}
	return contract.TurnResponse{Done: true}, nil
}
