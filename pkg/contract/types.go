// Package contract defines the public wire types for rallish.
// These are the only types that third-party adapters need to import.
package contract

import (
	"fmt"
	"strings"
)

// ExitCondition defines when a session should terminate.
type ExitCondition string

// Exit conditions.
const (
	ExitAllArtifactsCompile ExitCondition = "all_artifacts_compile"
	ExitTestsPass           ExitCondition = "tests_pass"
	ExitReviewerApproved    ExitCondition = "reviewer_approved"
	ExitTurnsExhausted      ExitCondition = "turns_exhausted"
	ExitTokensExhausted     ExitCondition = "tokens_exhausted"
	ExitDeadlinePassed      ExitCondition = "deadline_passed"
	ExitHumanSignal         ExitCondition = "human_signal"
	ExitDryRounds           ExitCondition = "dry_rounds"
	ExitStuck               ExitCondition = "stuck"
)

// HandoffIntent describes how the next role should treat the previous turn.
type HandoffIntent string

// Handoff intent values.
const (
	HandoffIntentContinue   HandoffIntent = "continue"
	HandoffIntentCrossCheck HandoffIntent = "cross_check"
)

// SelfEval describes an agent's own assessment of its turn quality.
type SelfEval string

// Self-evaluation values.
const (
	SelfEvalConfident SelfEval = "confident"
	SelfEvalUncertain SelfEval = "uncertain"
	SelfEvalBlocked   SelfEval = "blocked"
)

// Budget tracks the resources and policy limits for a session.
type Budget struct {
	// TokensLeft is the remaining token budget.
	TokensLeft int64 `json:"tokens_left"`
	// TurnsLeft is the remaining turn budget.
	TurnsLeft int `json:"turns_left"`
	// DeadlineMS is the remaining wall-clock budget in milliseconds.
	DeadlineMS int64 `json:"deadline_ms"`
	// DryRoundsThreshold is the number of consecutive dry turns that terminates
	// a preset session. Zero disables the dry-rounds exit condition.
	DryRoundsThreshold int `json:"dry_rounds_threshold,omitempty"`
}

// LastTurn carries the minimal state from the previous turn.
type LastTurn struct {
	// From is the role ID of the previous turn.
	From string `json:"from"`
	// Runtime is the runtime that produced the previous turn.
	Runtime string `json:"runtime"`
	// Summary is a compact recap of the previous turn (≤ ~400 tokens).
	Summary string `json:"summary"`
	// Artifacts are file paths touched in the previous turn.
	Artifacts []string `json:"artifacts,omitempty"`
	// SelfEval is the previous turn's self-assessment.
	SelfEval SelfEval `json:"self_eval"`
	// Intent hints how the next role should treat this turn.
	Intent HandoffIntent `json:"intent,omitempty"`
	// Claims are verifiable issues raised during this turn.
	Claims []Violation `json:"claims,omitempty"`
}

// Task describes the work assigned to the session.
type Task struct {
	// Title is a short name for the task.
	Title string `json:"title"`
	// Body is the full task description.
	Body string `json:"body"`
	// RepoRoot is the absolute path to the repository being worked on.
	RepoRoot string `json:"repo_root"`
}

// Usage reports token and timing consumption for a single turn.
type Usage struct {
	// TokensIn is the number of tokens consumed by the prompt.
	TokensIn int64 `json:"tokens_in"`
	// TokensOut is the number of tokens generated in the response.
	TokensOut int64 `json:"tokens_out"`
	// Ms is the wall-clock time taken for the turn in milliseconds.
	Ms int64 `json:"ms"`
}

// TurnRequest is sent from the broker to an adapter for each turn.
type TurnRequest struct {
	// Session is the unique session identifier.
	Session string `json:"session"`
	// Turn is the monotonic turn number.
	Turn int `json:"turn"`
	// Role is the role ID assigned to this turn (e.g. planner, executor).
	Role string `json:"role"`
	// RuntimeHint suggests which runtime should execute this turn.
	RuntimeHint string `json:"runtime_hint"`
	// ModelHint optionally suggests a specific model.
	ModelHint string `json:"model_hint,omitempty"`
	// Budget is the remaining session budget.
	Budget Budget `json:"budget"`
	// ScratchPath is the path to the shared scratchpad file.
	ScratchPath string `json:"shared_scratch_path"`
	// Scratch is the current contents of the shared scratchpad.
	Scratch string `json:"scratch,omitempty"`
	// LastTurn is a pointer to the previous turn's summary state.
	LastTurn *LastTurn `json:"last_turn,omitempty"`
	// Task is the work description for the session.
	Task Task `json:"task"`
	// WorkContract is the optional harness contract for agent-neutral autonomous work.
	WorkContract *WorkContract `json:"work_contract,omitempty"`
	// ExitWhen lists the conditions that can terminate the session.
	ExitWhen []ExitCondition `json:"exit_when"`
}

// TurnResponse is returned from an adapter to the broker after a turn.
type TurnResponse struct {
	// Done indicates whether the adapter believes the task is complete.
	Done bool `json:"done"`
	// HandoffTo optionally names the next role to act.
	HandoffTo string `json:"handoff_to,omitempty"`
	// HandoffIntent hints how the next role should treat this turn.
	HandoffIntent HandoffIntent `json:"handoff_intent,omitempty"`
	// Summary is a compact recap of this turn (≤ ~400 tokens).
	Summary string `json:"summary"`
	// Artifacts are file paths touched in this turn.
	Artifacts []string `json:"artifacts,omitempty"`
	// SelfEval is the adapter's self-assessment of this turn.
	SelfEval SelfEval `json:"self_eval"`
	// NotesForHuman carries optional human-readable commentary.
	NotesForHuman string `json:"notes_for_human,omitempty"`
	// Usage reports token and timing consumption for this turn.
	Usage *Usage `json:"usage,omitempty"`
	// Claims are verifiable issues raised during this turn.
	Claims []Violation `json:"claims,omitempty"`
}

// Compact returns a one-line summary of the turn suitable for scratchpad
// compaction. It omits empty fields to keep the output short.
func (r TurnResponse) Compact() string {
	var b strings.Builder
	if r.Done {
		b.WriteString("[done] ")
	}
	if r.HandoffTo != "" {
		b.WriteString("→ ")
		b.WriteString(r.HandoffTo)
		b.WriteString(" ")
	}
	b.WriteString("eval=")
	b.WriteString(string(r.SelfEval))
	if r.Summary != "" {
		b.WriteString(" summary=\"")
		b.WriteString(r.Summary)
		b.WriteString("\"")
	}
	if len(r.Artifacts) > 0 {
		b.WriteString(" artifacts=[")
		for i, a := range r.Artifacts {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(a)
		}
		b.WriteString("]")
	}
	if r.Usage != nil {
		fmt.Fprintf(&b, " usage=%d/%d", r.Usage.TokensIn, r.Usage.TokensOut)
	}
	return b.String()
}

// Role defines a single participant in a preset.
type Role struct {
	// ID is the unique role identifier.
	ID string `json:"id"`
	// Runtime is the agent runtime name (e.g. claude, kimi).
	Runtime string `json:"runtime"`
	// Model is the model hint for this role.
	Model string `json:"model"`
}

// ScratchConfig controls the rolling scratchpad limits.
type ScratchConfig struct {
	// MaxKB is the maximum size of the scratchpad in kilobytes.
	MaxKB int64 `json:"max_kb"`
	// SummarizeWith is the runtime/model used to compact the scratchpad.
	SummarizeWith string `json:"summarize_with"`
}

// Preset is a reusable session template.
type Preset struct {
	// Name is the preset identifier.
	Name string `json:"name"`
	// Description is a human-readable summary of the preset.
	Description string `json:"description"`
	// Roles are the participants defined by this preset.
	Roles []Role `json:"roles"`
	// Routing is the turn-routing strategy name.
	Routing string `json:"routing"`
	// Budget is the initial session budget.
	Budget Budget `json:"budget"`
	// ExitWhen lists the conditions that can terminate the session.
	ExitWhen []ExitCondition `json:"exit_when"`
	// Scratch configures the shared scratchpad.
	Scratch ScratchConfig `json:"scratch"`
}
