// Package cycle implements the autonomous-cycle domain layer.
package cycle

import (
	"fmt"
	"time"

	"github.com/jazz1x/rallish/pkg/contract"
)

// State wraps contract.CycleState with domain behaviour.
// It is the single source of truth for a cycle's progress.
type State struct {
	contract.CycleState
}

// NewState initialises a cycle State from a NewCycleRequest.
// It validates branch safety and ensures a non-empty goal.
func NewState(req contract.NewCycleRequest, id string) (State, error) {
	branch := req.Branch
	if branch == "" {
		branch = "feat/autonomous-cycle"
	}
	if branch == "main" || branch == "master" {
		return State{}, fmt.Errorf("branch %q: %w", branch, contract.ErrMainBranchForbidden)
	}
	if req.Goal == "" {
		return State{}, contract.ErrGoalRequired
	}
	maxCycles := req.MaxCycles
	if maxCycles <= 0 {
		maxCycles = 10
	}
	now := time.Now().UnixMilli()
	return State{
		CycleState: contract.CycleState{
			ID:                 id,
			Phase:              contract.CyclePhasePreflight,
			CompletedCycles:    0,
			MaxCycles:          maxCycles,
			Branch:             branch,
			PendingFiles:       req.PendingFiles,
			NextCycleGoal:      req.Goal,
			Halted:             false,
			CreatedAt:          now,
			UpdatedAt:          now,
			StartedAt:          now,
			AutoGoal:           req.AutoGoal,
			MaxDurationMinutes: req.MaxDurationMinutes,
		},
	}, nil
}

// Advance transitions the state to the next phase.
// If the state is halted, it returns ErrCycleHalted.
func (s *State) Advance() Result[State] {
	if s.Halted {
		return Failure(*s, contract.ErrCycleHalted)
	}
	s.Phase = s.Phase.Next()
	s.UpdatedAt = time.Now().UnixMilli()
	return Success(*s)
}

// Halt sets Halted=true and records the reason.
func (s *State) Halt(reason contract.HaltReason) Result[State] {
	s.Halted = true
	s.HaltReason = reason
	s.Phase = contract.CyclePhaseHalted
	s.UpdatedAt = time.Now().UnixMilli()
	return Failure(*s, &HaltedError{Reason: reason})
}

// CompleteCycle finishes the current cycle, increments the counter, resets the phase to Preflight,
// and clears the current goal (the caller must set a new NextCycleGoal before the next step).
func (s *State) CompleteCycle() Result[State] {
	if s.Halted {
		return Failure(*s, contract.ErrCycleHalted)
	}
	s.CompletedCycles++
	s.Phase = contract.CyclePhasePreflight
	s.UpdatedAt = time.Now().UnixMilli()
	return Success(*s)
}

// SetGoal updates NextCycleGoal and bumps UpdatedAt.
func (s *State) SetGoal(goal string) Result[State] {
	s.NextCycleGoal = goal
	s.UpdatedAt = time.Now().UnixMilli()
	return Success(*s)
}

// AppendReport adds a gate report to the history.
func (s *State) AppendReport(r contract.GateReport) Result[State] {
	s.History = append(s.History, r)
	s.UpdatedAt = time.Now().UnixMilli()
	return Success(*s)
}

// AppendViolations merges new violations into ViolationsFound, deduplicating by (File, Line, Type).
func (s *State) AppendViolations(vs []contract.Violation) Result[State] {
	key := func(v contract.Violation) string {
		return fmt.Sprintf("%s:%d:%s", v.File, v.Line, v.Type)
	}
	seen := make(map[string]struct{}, len(s.ViolationsFound))
	for _, v := range s.ViolationsFound {
		seen[key(v)] = struct{}{}
	}
	for _, v := range vs {
		k := key(v)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		s.ViolationsFound = append(s.ViolationsFound, v)
	}
	s.UpdatedAt = time.Now().UnixMilli()
	return Success(*s)
}

// ClearViolations removes violations of a given type (used after they are fixed).
func (s *State) ClearViolations(vtype string) Result[State] {
	filtered := make([]contract.Violation, 0, len(s.ViolationsFound))
	for _, v := range s.ViolationsFound {
		if v.Type != vtype {
			filtered = append(filtered, v)
		}
	}
	s.ViolationsFound = filtered
	s.UpdatedAt = time.Now().UnixMilli()
	return Success(*s)
}
