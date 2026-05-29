// Package cycle implements the autonomous-cycle domain layer.
package cycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jazz1x/rallish/internal/adapter"
	"github.com/jazz1x/rallish/internal/budget"
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

// SetDriverStepTimeout sets the per-step timeout on the underlying driver so a
// bounded one-shot driver can honour its own --step-timeout. A non-positive
// value leaves the driver default (10m) in place.
func (o *MultiAgentOrchestrator) SetDriverStepTimeout(d time.Duration) {
	if d > 0 {
		o.driver.StepTimeout = d
	}
}

// SetLedger injects an append-only harness ledger for orchestration events.
// It also forwards the ledger to the driver so Step can record gate-pin events
// (G2 tamper-resistant gates) without duplicating ledger state.
func (o *MultiAgentOrchestrator) SetLedger(ledger *LedgerFileSync) {
	o.ledger = ledger
	o.driver.SetLedger(ledger)
}

// Run executes the orchestration loop until halted or max cycles reached. It is
// a thin driver over RunOnce: the per-iteration guard sequence and agent turn are
// the SSOT loop body in RunOnce (also called by the bounded one-shot CLI driver).
// Run owns only the once-per-invocation reviver guard, the per-iteration state
// read, and the inter-cycle sleep.
func (o *MultiAgentOrchestrator) Run(ctx context.Context, cfg contract.OrchestratorConfig) error {
	if len(cfg.Agents) == 0 {
		return fmt.Errorf("orchestrator: no agents configured")
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
		outcome, err := o.RunOnce(ctx, cfg, state)
		if err != nil {
			return err
		}
		if outcome.Terminal {
			return nil
		}
		if err := o.driver.sleeper.Sleep(ctx, 30*time.Second); err != nil {
			return err
		}
	}
}

// IterationOutcome reports what a single orchestration iteration accomplished so
// callers (Run's loop, or the bounded one-shot CLI driver) can decide whether to
// continue. A nil error with Terminal=true means the cycle can no longer advance
// (success / max cycles reached); a *HaltedError means a guard or gate sealed it.
type IterationOutcome struct {
	State    State
	Advanced bool // a gated step completed and the advanced state was persisted
	Terminal bool // the cycle can no longer advance — do not loop / re-trigger
}

// RunOnce executes exactly ONE orchestration iteration against the supplied
// state: anti-spin guards (Stuck / lifetime budget) → one agent turn → handshake
// apply → one gated Driver.Step → persist. It is the single source of truth for
// the loop body, shared by Run and the bounded `cycle run --once --agents` driver.
//
// It deliberately does NOT perform the reviver guard (LedgerSealsResume): that is
// a once-per-invocation preamble owned by the caller (Run runs it before the
// loop; the CLI one-shot runs it before delegating here), so it is not repeated
// per iteration. The caller reads fresh state and passes it in.
//
// On a halt — anti-spin, an unparseable/explicit-halt handshake, or a gate
// failure — it returns a *HaltedError after persisting the sealed state (and, for
// the anti-spin and handshake paths, recording the cycle_halted ledger entry):
// the same canonical halt flow Run has always used.
func (o *MultiAgentOrchestrator) RunOnce(ctx context.Context, cfg contract.OrchestratorConfig, state State) (IterationOutcome, error) {
	if len(cfg.Agents) == 0 {
		return IterationOutcome{State: state}, fmt.Errorf("orchestrator: no agents configured")
	}
	resetEvery := cfg.ResetEvery
	if resetEvery <= 0 {
		resetEvery = 3
	}

	if !state.CanAdvance() {
		return IterationOutcome{State: state, Terminal: true}, nil
	}

	// Anti-spin (G5): halt before the run bleeds resources. Two distinct
	// breakers share one ledger read:
	//   - Stuck() — a cheap diagnostic for a spinning / no-progress run.
	//   - the hard cost ceiling — a lifetime turn bound (summed across
	//     revivals) that catches a *productive* runaway Stuck() cannot see.
	// Both halt via the canonical HaltedError flow and record a cycle_halted
	// entry so the reviver guard then sees a sticky halt.
	if o.ledger != nil {
		entries, lerr := o.ledger.ReadAll()
		if lerr != nil {
			return IterationOutcome{State: state}, fmt.Errorf("orchestrator read ledger: %w", lerr)
		}
		reason, halt := Stuck(entries)
		if !halt && budget.ExceedsLifetimeCeiling(entries, cfg.MaxLifetimeTurns) {
			reason, halt = contract.HaltBudgetExceeded, true
		}
		if halt {
			halted := state.Halt(reason)
			st := halted.Value()
			_ = o.ledger.Append(contract.NewHarnessLedgerEntry(
				time.Now().UnixMilli(), st.ID, contract.LedgerEventCycleHalted, string(reason), nil))
			if werr := o.sync.Write(st); werr != nil {
				return IterationOutcome{State: st}, fmt.Errorf("orchestrator write halted state: %w", werr)
			}
			return IterationOutcome{State: st}, halted.Err()
		}
	}

	// Determine current agent.
	agentIdx := state.CompletedCycles / resetEvery
	agentIdx %= len(cfg.Agents)
	agentName := cfg.Agents[agentIdx]

	adapt, ok := o.registry.Get(agentName)
	if !ok {
		return IterationOutcome{State: state}, fmt.Errorf("orchestrator: adapter %q not found in registry", agentName)
	}

	// Build TurnRequest embedding the cycle state.
	req, err := o.buildRequest(state, agentName, cfg)
	if err != nil {
		return IterationOutcome{State: state}, fmt.Errorf("orchestrator build request: %w", err)
	}

	// Execute one turn.
	resp, err := adapt.Run(ctx, req)
	if err != nil {
		return IterationOutcome{State: state}, fmt.Errorf("orchestrator adapter %q run: %w", agentName, err)
	}
	if err := o.appendTurnLedger(state.ID, agentName, resp); err != nil {
		return IterationOutcome{State: state}, err
	}

	// Parse response into cycle state mutations. An unparseable handshake (or
	// an explicit halt request) returns a *HaltedError: seal the cycle, record
	// the halt, and stop — never advance on a turn we could not parse.
	if err := o.applyResponse(&state, resp); err != nil {
		var he *HaltedError
		if errors.As(err, &he) {
			return IterationOutcome{State: state}, o.handleHandshakeHalt(state, he)
		}
		return IterationOutcome{State: state}, fmt.Errorf("orchestrator apply response: %w", err)
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
			return IterationOutcome{State: result.Value()}, he
		}
		return IterationOutcome{State: result.Value()}, result.Err()
	}

	state = result.Value()
	if err := o.sync.Write(state); err != nil {
		return IterationOutcome{State: state}, fmt.Errorf("orchestrator write state: %w", err)
	}

	return IterationOutcome{State: state, Advanced: true, Terminal: !state.CanAdvance()}, nil
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

// applyResponse parses the agent handshake (TurnResponse.Summary) into a typed
// payload and applies it to the cycle state.
//
// Parse-don't-validate at the agent boundary: a non-empty summary that does not
// parse into contract.TurnPayload HALTS the cycle (HaltUnparseableTurn). Prose
// is never silently coerced into the next goal — the harness trusts structural
// facts, not self-report. An empty summary is a legitimate no-op turn (the
// downstream goal gate, not the handshake, decides whether a missing goal halts).
//
// When a halt is warranted (unparseable, or an explicit halt_requested), it
// returns a *HaltedError so the orchestrator persists the sealed state and
// records the halt to the ledger via handleHandshakeHalt.
func (o *MultiAgentOrchestrator) applyResponse(state *State, resp contract.TurnResponse) error {
	payload, ok := contract.ParseTurnPayload(resp.Summary)
	if !ok {
		return state.Halt(contract.HaltUnparseableTurn).Err()
	}
	if payload.HaltRequested {
		return state.Halt(contract.HaltUserRequested).Err()
	}
	if payload.NextGoal != "" {
		state.NextCycleGoal = payload.NextGoal
	}
	if len(payload.ViolationsFound) > 0 {
		state.AppendViolations(payload.ViolationsFound)
	}
	state.UpdatedAt = time.Now().UnixMilli()
	return nil
}

// handleHandshakeHalt persists a handshake-driven halt (unparseable turn or
// explicit halt request) and records it to the ledger, mirroring the Stuck /
// budget halt path so the reviver guard sees a sticky cycle_halted entry.
func (o *MultiAgentOrchestrator) handleHandshakeHalt(state State, he *HaltedError) error {
	if o.ledger != nil {
		_ = o.ledger.Append(contract.NewHarnessLedgerEntry(
			time.Now().UnixMilli(), state.ID, contract.LedgerEventCycleHalted, string(he.Reason), nil))
	}
	if werr := o.sync.Write(state); werr != nil {
		return fmt.Errorf("orchestrator write halted state: %w", werr)
	}
	return he
}
