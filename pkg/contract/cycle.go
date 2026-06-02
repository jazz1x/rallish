// Package contract defines the public wire types for rallish.
//
// This file contains the autonomous-cycle subsystem types.
package contract

import (
	"encoding/json"
	"fmt"
	"time"
)

// CyclePhase is the lifecycle state of a single cycle step.
// Construct via ParseCyclePhase; the zero value is invalid.
type CyclePhase string

// Cycle phases in execution order.
const (
	CyclePhasePreflight  CyclePhase = "preflight"
	CyclePhaseAudit      CyclePhase = "audit"
	CyclePhasePhilosophy CyclePhase = "philosophy"
	CyclePhasePolish     CyclePhase = "polish"
	CyclePhaseCommit     CyclePhase = "commit"
	CyclePhaseHandoff    CyclePhase = "handoff"
	CyclePhaseHalted     CyclePhase = "halted"
)

// ParseCyclePhase converts a string to a validated CyclePhase.
// Returns ErrInvalidCyclePhase if s is not a recognised phase.
func ParseCyclePhase(s string) (CyclePhase, error) {
	switch CyclePhase(s) {
	case CyclePhasePreflight, CyclePhaseAudit, CyclePhasePhilosophy,
		CyclePhasePolish, CyclePhaseCommit, CyclePhaseHandoff, CyclePhaseHalted:
		return CyclePhase(s), nil
	default:
		return "", fmt.Errorf("phase %q: %w", s, ErrInvalidCyclePhase)
	}
}

// String returns the canonical string representation.
func (p CyclePhase) String() string { return string(p) }

// Next returns the phase that logically follows this one in the pipeline.
// Halted is terminal and returns itself.
func (p CyclePhase) Next() CyclePhase {
	switch p {
	case CyclePhasePreflight:
		return CyclePhaseAudit
	case CyclePhaseAudit:
		return CyclePhasePhilosophy
	case CyclePhasePhilosophy:
		return CyclePhasePolish
	case CyclePhasePolish:
		return CyclePhaseCommit
	case CyclePhaseCommit:
		return CyclePhaseHandoff
	default:
		return CyclePhaseHalted
	}
}

// HaltReason describes why a cycle halted.
// Construct via ParseHaltReason; the zero value is invalid.
type HaltReason string

// Known halt reasons.
const (
	HaltSelfAuditViolation HaltReason = "self-audit-violation"
	HaltSSHAuthFailed      HaltReason = "ssh-auth-failed"
	HaltMaxCyclesReached   HaltReason = "max-cycles-reached"
	HaltGateFailure        HaltReason = "gate-failure"
	HaltUserRequested      HaltReason = "user-requested"
	HaltPreflightFailed    HaltReason = "preflight-failed"
	HaltSuccess            HaltReason = "success"
	HaltStuck              HaltReason = "stuck"
	HaltBudgetExceeded     HaltReason = "budget-exceeded"
	// HaltUnparseableTurn is raised when an agent turn carries a non-empty
	// handshake payload that does not parse into the typed TurnPayload. The
	// harness trusts structural facts, not self-report: an unparseable turn is
	// an error, never silently coerced into a goal (parse-don't-validate).
	HaltUnparseableTurn HaltReason = "unparseable-turn"
)

// ParseHaltReason converts a string to a validated HaltReason.
// Returns ErrInvalidHaltReason if s is not recognised.
func ParseHaltReason(s string) (HaltReason, error) {
	switch HaltReason(s) {
	case HaltSelfAuditViolation, HaltSSHAuthFailed, HaltMaxCyclesReached,
		HaltGateFailure, HaltUserRequested, HaltPreflightFailed, HaltSuccess,
		HaltStuck, HaltBudgetExceeded, HaltUnparseableTurn:
		return HaltReason(s), nil
	default:
		return "", fmt.Errorf("halt reason %q: %w", s, ErrInvalidHaltReason)
	}
}

// String returns the canonical string representation.
func (r HaltReason) String() string { return string(r) }

// TurnPayload is the typed handshake an agent encodes into TurnResponse.Summary
// for the autonomous cycle. It is the structured contract between the agent and
// the orchestrator — parsed strictly, never inferred from free text.
type TurnPayload struct {
	// NextGoal is the one-sentence objective the agent proposes for the next cycle.
	NextGoal string `json:"next_goal,omitempty"`
	// ViolationsFound carries issues the agent reported during its turn.
	ViolationsFound []Violation `json:"violations_found,omitempty"`
	// HaltRequested, when true, asks the orchestrator to stop the cycle.
	HaltRequested bool `json:"halt_requested,omitempty"`
}

// ParseTurnPayload parses an agent handshake summary into a typed TurnPayload.
//
// It is parse-don't-validate at the agent boundary:
//   - An empty summary is a no-op turn (handoff/Done with nothing to apply) and
//     returns the zero payload with ok=true — the downstream goal gate decides
//     whether a missing goal is fatal, not the handshake.
//   - A non-empty summary MUST be valid JSON for the typed payload. If it does
//     not parse, ok=false and the caller MUST halt (HaltUnparseableTurn). Prose
//     is never coerced into a goal — the harness trusts structure, not text.
func ParseTurnPayload(summary string) (TurnPayload, bool) {
	if summary == "" {
		return TurnPayload{}, true
	}
	var p TurnPayload
	if err := json.Unmarshal([]byte(summary), &p); err != nil {
		return TurnPayload{}, false
	}
	return p, true
}

// Violation records a single issue discovered by a gate or philosophy sweep.
type Violation struct {
	// File is the path to the file containing the violation.
	File string `json:"file,omitempty"`
	// Line is the approximate line number (0 if unknown).
	Line int `json:"line,omitempty"`
	// Type categorises the violation (e.g. "rop", "ssot", "srp", "hardcoded-version").
	Type string `json:"type"`
	// Message is a human-readable description.
	Message string `json:"message"`
}

// GateReport carries the output of a single gate execution.
type GateReport struct {
	// Gate is the name of the gate that produced this report.
	Gate string `json:"gate"`
	// Passed indicates whether the gate succeeded.
	Passed bool `json:"passed"`
	// DurationMS is how long the gate took in milliseconds.
	DurationMS int64 `json:"duration_ms"`
	// Stdout captures the gate's standard output.
	Stdout string `json:"stdout,omitempty"`
	// Stderr captures the gate's standard error.
	Stderr string `json:"stderr,omitempty"`
	// Violations lists issues found by this gate.
	Violations []Violation `json:"violations,omitempty"`
}

// GateResult is the sealed sum type returned by a gate.
// Use GateSuccess, GateFailure, or GateWarning; never implement this yourself.
type GateResult interface {
	sealGateResult()
	// Report returns the gate report associated with this result.
	Report() GateReport
}

// GateSuccess means the gate passed with no actionable issues.
type GateSuccess struct{ R GateReport }

func (GateSuccess) sealGateResult() {}

// Report returns the embedded report.
func (g GateSuccess) Report() GateReport { return g.R }

// GateFailure means the gate failed and the cycle should halt.
type GateFailure struct {
	R      GateReport
	Reason HaltReason
}

func (GateFailure) sealGateResult() {}

// Report returns the embedded report.
func (g GateFailure) Report() GateReport { return g.R }

// GateWarning means the gate passed but flagged issues that should be logged.
type GateWarning struct{ R GateReport }

func (GateWarning) sealGateResult() {}

// Report returns the embedded report.
func (g GateWarning) Report() GateReport { return g.R }

// CycleState is the single source of truth for an autonomous cycle.
type CycleState struct {
	// ID is the unique cycle identifier.
	ID string `json:"id"`
	// Phase is the current cycle phase.
	Phase CyclePhase `json:"phase"`
	// CompletedCycles is how many full cycles have finished successfully.
	CompletedCycles int `json:"completed_cycles"`
	// MaxCycles caps the total number of cycles (safety bound).
	MaxCycles int `json:"max_cycles"`
	// Branch is the git branch being worked on (never "main").
	Branch string `json:"branch"`
	// BaselineSHA is the commit SHA to diff against for regression checks.
	BaselineSHA string `json:"baseline_sha"`
	// PendingFiles lists files still needing work.
	PendingFiles []string `json:"pending_files,omitempty"`
	// LocalGates lists repository-specific validation commands to run after the built-in audit gate.
	LocalGates []string `json:"local_gates,omitempty"`
	// AuditCmd overrides the audit gate command (default: "make check-all").
	// Must be a non-empty, non-whitespace-only string when set; leave empty for the default.
	AuditCmd string `json:"audit_cmd,omitempty"`
	// PolishTestCmd overrides the polish gate test command (default: "go test -race ./...").
	// Must be a non-empty, non-whitespace-only string when set; leave empty for the default.
	// A whitespace-only value when set is a misconfiguration and causes the gate to fail loudly.
	PolishTestCmd string `json:"polish_test_cmd,omitempty"`
	// ViolationsFound accumulates issues discovered but not yet fixed.
	ViolationsFound []Violation `json:"violations_found,omitempty"`
	// NextCycleGoal is the mandatory one-sentence goal for the next cycle.
	NextCycleGoal string `json:"next_cycle_goal"`
	// Halted, when true, means the cycle must not advance further.
	Halted bool `json:"halted"`
	// HaltReason is populated when Halted is true.
	HaltReason HaltReason `json:"halt_reason,omitempty"`
	// CreatedAt is the Unix millisecond timestamp of cycle creation.
	CreatedAt int64 `json:"created_at"`
	// UpdatedAt is the Unix millisecond timestamp of the last state change.
	UpdatedAt int64 `json:"updated_at"`
	// History is the ordered list of gate reports from completed cycles.
	History []GateReport `json:"history,omitempty"`
	// LastFailedGate names the gate that caused the most recent halt.
	LastFailedGate string `json:"last_failed_gate,omitempty"`
	// AutoGoal, when true, enables automatic goal discovery after each cycle.
	AutoGoal bool `json:"auto_goal,omitempty"`
	// MaxDurationMinutes caps the total runtime (0 = unlimited).
	MaxDurationMinutes int `json:"max_duration_minutes,omitempty"`
	// StartedAt is the Unix millisecond timestamp when the cycle was created.
	StartedAt int64 `json:"started_at"`
}

// CanAdvance returns true if the cycle is not halted, has not reached max cycles,
// and has not exceeded the max duration.
func (s *CycleState) CanAdvance() bool {
	if s.Halted {
		return false
	}
	if s.MaxCycles > 0 && s.CompletedCycles >= s.MaxCycles {
		return false
	}
	if s.MaxDurationMinutes > 0 && s.StartedAt > 0 {
		if int(time.Since(time.UnixMilli(s.StartedAt)).Minutes()) >= s.MaxDurationMinutes {
			return false
		}
	}
	return true
}

// ShouldRotateAgent returns true when a fresh-agent reset is recommended
// (every 3 completed cycles, matching the autonomous-cycle skill convention).
func (s *CycleState) ShouldRotateAgent() bool {
	return s.CompletedCycles > 0 && s.CompletedCycles%3 == 0
}

// CycleEvent is the SSE payload streamed by /cycles/:id/events.
type CycleEvent struct {
	// CycleID identifies the cycle.
	CycleID string `json:"cycle_id"`
	// Phase is the current phase.
	Phase CyclePhase `json:"phase"`
	// CompletedCycles is the number of finished cycles.
	CompletedCycles int `json:"completed_cycles"`
	// Halted indicates whether the cycle has stopped.
	Halted bool `json:"halted,omitempty"`
	// HaltReason is set when Halted is true.
	HaltReason HaltReason `json:"halt_reason,omitempty"`
	// LastFailedGate names the gate that caused the most recent failure.
	LastFailedGate string `json:"last_failed_gate,omitempty"`
	// At is the Unix millisecond timestamp of the event.
	At int64 `json:"at"`
}

// NewCycleEvent builds a CycleEvent from a CycleState with the current timestamp.
func NewCycleEvent(state *CycleState) CycleEvent {
	return CycleEvent{
		CycleID:         state.ID,
		Phase:           state.Phase,
		CompletedCycles: state.CompletedCycles,
		Halted:          state.Halted,
		HaltReason:      state.HaltReason,
		LastFailedGate:  state.LastFailedGate,
		At:              time.Now().UnixMilli(),
	}
}

// OrchestratorConfig controls multi-agent ping-pong behaviour.
type OrchestratorConfig struct {
	// Agents is the ordered list of adapter names to rotate through.
	Agents []string `json:"agents"`
	// Pattern is the coordination pattern name (e.g. "cycle", "discuss", "help").
	Pattern string `json:"pattern,omitempty"`
	// ResetEvery is the number of cycles between agent rotations (default 3).
	ResetEvery int `json:"reset_every,omitempty"`
	// RepoURL is the optional remote repository for cross-repo orchestration.
	RepoURL string `json:"repo_url,omitempty"`
	// WorkingDir is the local working directory for this cycle.
	WorkingDir string `json:"working_dir,omitempty"`
	// MaxLifetimeTurns is a hard ceiling on agent turns summed across ALL
	// revivals from the ledger (0 = unlimited). Distinct from the stuck-breaker:
	// it halts a productive runaway that never trips Stuck(). Enforcing it across
	// revivals depends on a cooperating driver re-running orchestration against
	// the same persisted ledger.
	MaxLifetimeTurns int `json:"max_lifetime_turns,omitempty"`
}

// NewCycleRequest is the body for POST /cycles.
type NewCycleRequest struct {
	// Goal is the one-sentence objective for the first cycle.
	Goal string `json:"goal"`
	// Branch is the target git branch (must not be "main").
	Branch string `json:"branch,omitempty"`
	// MaxCycles caps the run (0 = unlimited, default 10).
	MaxCycles int `json:"max_cycles,omitempty"`
	// MaxDurationMinutes caps total runtime in minutes (0 = unlimited).
	MaxDurationMinutes int `json:"max_duration_minutes,omitempty"`
	// AutoGoal enables automatic goal discovery after each cycle.
	AutoGoal bool `json:"auto_goal,omitempty"`
	// PendingFiles lists files the cycle should focus on.
	PendingFiles []string `json:"pending_files,omitempty"`
	// LocalGates lists repository-specific validation commands to run after the built-in audit gate.
	LocalGates []string `json:"local_gates,omitempty"`
	// AuditCmd overrides the audit gate command (default: "make check-all").
	// Must be a non-empty, non-whitespace-only string when set; leave empty for the default.
	AuditCmd string `json:"audit_cmd,omitempty"`
	// PolishTestCmd overrides the polish gate test command (default: "go test -race ./...").
	// Must be a non-empty, non-whitespace-only string when set; leave empty for the default.
	// A whitespace-only value when set is a misconfiguration and causes the gate to fail loudly.
	PolishTestCmd string `json:"polish_test_cmd,omitempty"`
	// Orchestrator, when set, enables multi-agent ping-pong.
	Orchestrator *OrchestratorConfig `json:"orchestrator,omitempty"`
}

// StepRequest is the body for POST /cycles/:id/step.
type StepRequest struct {
	// Goal overrides NextCycleGoal for this step only.
	Goal string `json:"goal,omitempty"`
}

// HaltRequest is the body for POST /cycles/:id/halt.
type HaltRequest struct {
	// Reason is the human-readable halt reason.
	Reason string `json:"reason,omitempty"`
}
