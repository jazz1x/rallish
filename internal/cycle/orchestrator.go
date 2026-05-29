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
	ledger   *LedgerFileSync
	driver   *Driver
	pipeline func(State) Pipeline
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
	o.pipeline = nil
}

// SetPipelineFactory injects a state-aware pipeline into the underlying driver.
func (o *MultiAgentOrchestrator) SetPipelineFactory(fn func(State) Pipeline) {
	o.pipeline = fn
}

// SetDriverSleeper injects a sleeper into the underlying driver.
func (o *MultiAgentOrchestrator) SetDriverSleeper(s Sleeper) {
	o.driver.SetSleeper(s)
}

// SetLedger injects an append-only harness ledger for orchestration events.
func (o *MultiAgentOrchestrator) SetLedger(ledger *LedgerFileSync) {
	o.ledger = ledger
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

	// Reviver guard (G5): a cycle that already halted is sealed. A cron/driver
	// re-trigger must not silently revive it. The mutable state file is removed
	// on halt, but the append-only ledger persists, so read it once up front and
	// refuse to resume when the last cycle_halted has no later validation_green.
	if o.ledger != nil {
		entries, lerr := o.ledger.ReadAll()
		if lerr != nil {
			return fmt.Errorf("orchestrator read ledger: %w", lerr)
		}
		if reason, sealed := LedgerSealsResume(entries); sealed {
			return &HaltedError{Reason: reason}
		}
	}

	for {
		state, err := o.sync.Read()
		if err != nil {
			return fmt.Errorf("orchestrator read state: %w", err)
		}
		if !state.CanAdvance() {
			return nil
		}

		// Anti-spin (G5): halt a spinning / no-progress run before it bleeds
		// tokens. Stuck() is a cheap ledger-reader; halting reuses the canonical
		// HaltedError flow and records a cycle_halted entry so a reviver sees a
		// sticky halt.
		if o.ledger != nil {
			entries, lerr := o.ledger.ReadAll()
			if lerr != nil {
				return fmt.Errorf("orchestrator read ledger: %w", lerr)
			}
			if reason, stuck := Stuck(entries); stuck {
				halted := state.Halt(reason)
				st := halted.Value()
				_ = o.ledger.Append(contract.NewHarnessLedgerEntry(
					time.Now().UnixMilli(), st.ID, contract.LedgerEventCycleHalted, string(reason), nil))
				if werr := o.sync.Write(st); werr != nil {
					return fmt.Errorf("orchestrator write halted state: %w", werr)
				}
				return halted.Err()
			}
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
		if err := o.appendTurnLedger(state.ID, agentName, resp); err != nil {
			return err
		}

		// Parse response into cycle state mutations.
		if err := o.applyResponse(&state, resp); err != nil {
			return fmt.Errorf("orchestrator apply response: %w", err)
		}
		if o.pipeline != nil {
			o.driver.SetPipeline(o.pipeline(state))
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

func (o *MultiAgentOrchestrator) appendTurnLedger(cycleID, agentName string, resp contract.TurnResponse) error {
	if o.ledger == nil {
		return nil
	}
	if err := o.ledger.Append(contract.NewAgentTurnLedgerEntry(time.Now().UnixMilli(), cycleID, agentName, resp)); err != nil {
		return fmt.Errorf("orchestrator append agent turn ledger: %w", err)
	}
	if resp.HandoffTo == "" {
		return nil
	}
	entry := contract.NewHandoffLedgerEntry(time.Now().UnixMilli(), cycleID, agentName, resp)
	if err := o.ledger.Append(entry); err != nil {
		return fmt.Errorf("orchestrator append handoff ledger: %w", err)
	}
	return nil
}

// cycleStateSummary is a slim view of CycleState to avoid overflowing the
// adapter context window during long autonomous loops.
type cycleStateSummary struct {
	ID              string               `json:"id"`
	Phase           contract.CyclePhase  `json:"phase"`
	CompletedCycles int                  `json:"completed_cycles"`
	MaxCycles       int                  `json:"max_cycles"`
	Branch          string               `json:"branch"`
	BaselineSHA     string               `json:"baseline_sha,omitempty"`
	PendingFiles    []string             `json:"pending_files,omitempty"`
	LocalGates      []string             `json:"local_gates,omitempty"`
	NextCycleGoal   string               `json:"next_cycle_goal"`
	ViolationsFound []contract.Violation `json:"violations_found,omitempty"`
	Halted          bool                 `json:"halted"`
	HaltReason      contract.HaltReason  `json:"halt_reason,omitempty"`
}

func summariseState(state State) cycleStateSummary {
	s := cycleStateSummary{
		ID:              state.ID,
		Phase:           state.Phase,
		CompletedCycles: state.CompletedCycles,
		MaxCycles:       state.MaxCycles,
		Branch:          state.Branch,
		BaselineSHA:     state.BaselineSHA,
		NextCycleGoal:   state.NextCycleGoal,
		Halted:          state.Halted,
		HaltReason:      state.HaltReason,
	}
	// Cap slices to prevent context bloat in long loops.
	if len(state.PendingFiles) > 20 {
		s.PendingFiles = state.PendingFiles[:20]
	} else {
		s.PendingFiles = append([]string(nil), state.PendingFiles...)
	}
	if len(state.LocalGates) > 20 {
		s.LocalGates = state.LocalGates[:20]
	} else {
		s.LocalGates = append([]string(nil), state.LocalGates...)
	}
	if len(state.ViolationsFound) > 10 {
		s.ViolationsFound = state.ViolationsFound[:10]
	} else {
		s.ViolationsFound = append([]contract.Violation(nil), state.ViolationsFound...)
	}
	return s
}

func (o *MultiAgentOrchestrator) buildRequest(state State, agentName string, cfg contract.OrchestratorConfig) (contract.TurnRequest, error) {
	summary := summariseState(state)
	stateJSON, err := json.Marshal(summary)
	if err != nil {
		return contract.TurnRequest{}, fmt.Errorf("marshal state summary: %w", err)
	}
	workContract := state.WorkContract(cfg.WorkingDir, &cfg)

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
		WorkContract: &workContract,
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
