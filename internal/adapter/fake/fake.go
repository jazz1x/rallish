// Package fake provides a deterministic test adapter for hocketty.
package fake

import (
	"context"

	"github.com/jazz1x/hocketty/pkg/contract"
)

// Adapter is a test double that returns canned responses.
type Adapter struct {
	callback func(turn int) contract.TurnResponse
}

// New creates a fake adapter. If callback is nil, all turns return Done=true.
func New(callback func(turn int) contract.TurnResponse) *Adapter {
	return &Adapter{callback: callback}
}

// Name returns the adapter name.
func (a *Adapter) Name() string { return "fake" }

// Check always succeeds.
func (a *Adapter) Check() error { return nil }

// Run returns the callback response for the given turn, or Done=true if no callback is set.
func (a *Adapter) Run(_ context.Context, req contract.TurnRequest) (contract.TurnResponse, error) {
	if a.callback != nil {
		return a.callback(req.Turn), nil
	}
	return contract.TurnResponse{Done: true}, nil
}
