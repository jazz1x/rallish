// Package cycle implements the autonomous-cycle domain layer.
package cycle

import (
	"context"

	"github.com/jazz1x/rallish/pkg/contract"
)

// Gate is the interface for a single phase in the cycle pipeline.
// Each gate has exactly one responsibility (SRP).
type Gate interface {
	// Name returns the canonical name of the gate.
	Name() string
	// Run executes the gate against the current state.
	// It returns a GateResult and the (possibly mutated) state.
	Run(ctx context.Context, state State) (contract.GateResult, State)
}

// Pipeline is an ordered chain of gates executed sequentially.
// A GateFailure short-circuits the pipeline and the cycle halts.
type Pipeline []Gate

// Execute runs each gate in order, accumulating reports.
// On GateFailure it halts the state and returns a Failure Result.
// On GateWarning it logs and continues.
func (p Pipeline) Execute(ctx context.Context, initial State) Result[State] {
	current := initial
	var reports []contract.GateReport

	for _, gate := range p {
		result, next := gate.Run(ctx, current)
		reports = append(reports, result.Report())

		switch r := result.(type) {
		case contract.GateFailure:
			next.LastFailedGate = r.Report().Gate
			halted := next.Halt(r.Reason).Value()
			return Failure(halted, &HaltedError{Reason: r.Reason, Reports: reports}, reports...)
		case contract.GateWarning:
			// Continue but keep the report.
		case contract.GateSuccess:
			// Continue.
		}

		current = next
	}

	return Success(current, reports...)
}

// Names returns the ordered list of gate names in this pipeline.
func (p Pipeline) Names() []string {
	names := make([]string, len(p))
	for i, g := range p {
		names[i] = g.Name()
	}
	return names
}
