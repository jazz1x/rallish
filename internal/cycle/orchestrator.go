// Package cycle implements the autonomous-cycle domain layer.
package cycle

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jazz1x/rallish/internal/adapter"
	"github.com/jazz1x/rallish/pkg/contract"
)

// MultiAgentOrchestrator drives autonomous cycles across multiple adapters,
// rotating every N cycles (default 3) to mimic the "fresh-agent reset" pattern.
type MultiAgentOrchestrator struct {
	registry *adapter.Registry
	sync     *StateFileSync
	driver   *Driver
}

// NewMultiAgentOrchestrator creates an orchestrator.
func NewMultiAgentOrchestrator(reg *adapter.Registry, sync *StateFileSync) *MultiAgentOrchestrator {
	return &MultiAgentOrchestrator{
		registry: reg,
		sync:     sync,
		driver:   NewCycleDriver(sync),
	}
}

// SetDriverPipeline injects a pipeline into the underlying driver.
func (o *MultiAgentOrchestrator) SetDriverPipeline(p Pipeline) {
	o.driver.SetPipeline(p)
}

// SetDriverSleeper injects a sleeper into the underlying driver.
func (o *MultiAgentOrchestrator) SetDriverSleeper(s Sleeper) {
	o.driver.SetSleeper(s)
}

// Run executes the orchestration loop until halted or max cycles reached.
func (o *MultiAgentOrchestrator) Run(ctx context.Context, cfg contract.OrchestratorConfig) error {
	if len(cfg.Agents) == 0 {
		return fmt.Errorf("orchestrator: no agents configured")
	}
	resetEvery := cfg.ResetEvery
	if resetEvery <= 0 {
		resetEvery = 3
	}

	for {
		state, err := o.sync.Read()
		if err != nil {
			return fmt.Errorf("orchestrator read state: %w", err)
		}
		if !state.CanAdvance() {
			return nil
		}

		// Determine current agent.
		agentIdx := state.CompletedCycles / resetEvery
		agentIdx %= len(cfg.Agents)
		agentName := cfg.Agents[agentIdx]

		adapt, ok := o.registry.Get(agentName)
		if !ok {
			return fmt.Errorf("orchestrator: adapter %q not found in registry", agentName)
		}

		// Build TurnRequest embedding the cycle state.
		req, err := o.buildRequest(state, agentName, cfg)
		if err != nil {
			return fmt.Errorf("orchestrator build request: %w", err)
		}

		// Execute one turn.
		resp, err := adapt.Run(ctx, req)
		if err != nil {
			return fmt.Errorf("orchestrator adapter %q run: %w", agentName, err)
		}

		// Parse response into cycle state mutations.
		if err := o.applyResponse(&state, resp); err != nil {
			return fmt.Errorf("orchestrator apply response: %w", err)
		}

		// Run the driver step (pipeline + commit).
		result := o.driver.Step(ctx, state)
		if result.IsFailure() {
			he, ok := result.Err().(*HaltedError)
			if ok {
				_ = o.sync.Write(result.Value())
				return he
			}
			return result.Err()
		}

		state = result.Value()
		if err := o.sync.Write(state); err != nil {
			return fmt.Errorf("orchestrator write state: %w", err)
		}

		if !state.CanAdvance() {
			return nil
		}

		if err := o.driver.sleeper.Sleep(ctx, 30*time.Second); err != nil {
			return err
		}
	}
}

func (o *MultiAgentOrchestrator) buildRequest(state State, agentName string, cfg contract.OrchestratorConfig) (contract.TurnRequest, error) {
	stateJSON, err := json.Marshal(state.CycleState)
	if err != nil {
		return contract.TurnRequest{}, fmt.Errorf("marshal state: %w", err)
	}

	return contract.TurnRequest{
		Session:     state.ID,
		Turn:        state.CompletedCycles + 1,
		Role:        agentName,
		RuntimeHint: agentName,
		Task: contract.Task{
			Title:    "autonomous-cycle",
			Body:     string(stateJSON),
			RepoRoot: cfg.WorkingDir,
		},
		Budget: contract.Budget{
			TurnsLeft: state.MaxCycles - state.CompletedCycles,
		},
	}, nil
}

func (o *MultiAgentOrchestrator) applyResponse(state *State, resp contract.TurnResponse) error {
	// Parse structured fields from the response summary if present.
	var payload struct {
		NextGoal        string               `json:"next_goal"`
		ViolationsFound []contract.Violation `json:"violations_found"`
		HaltRequested   bool                 `json:"halt_requested"`
	}
	if err := json.Unmarshal([]byte(resp.Summary), &payload); err == nil {
		if payload.HaltRequested {
			state.Halt(contract.HaltUserRequested)
			return nil
		}
		if payload.NextGoal != "" {
			state.NextCycleGoal = payload.NextGoal
		}
		if len(payload.ViolationsFound) > 0 {
			state.AppendViolations(payload.ViolationsFound)
		}
	} else if resp.Summary != "" {
		// Fallback: treat the entire summary as the next goal.
		state.NextCycleGoal = resp.Summary
	}
	state.UpdatedAt = time.Now().UnixMilli()
	return nil
}
