package contract

// tooluse_gate.go — G6 combined pre-execution tool-use decision (the
// enforcement-callable surface a runtime PreToolUse hook invokes).
//
// BOUNDARY (north-star §G6, lines 128-129): rallish DECLARES policy and RECORDS
// the decision; the runtime hook / sandbox ENFORCES. Nothing in this file
// executes, intercepts, or blocks a command. DecideToolUse is a pure combinator
// over the two existing G6 decision layers — ActionDecisionFor (destructive-action
// deny-list) and SecretDecisionFor (secret containment) — collapsing their two
// verdicts into the SINGLE most-severe verdict a hook acts on. It is the
// "match a dangerous-action pattern on the pending edge before it is added"
// predicate from the Work-Graph model, evaluated once per pending tool call.
//
// rallish RETURNS the verdict and RECORDS it (NewToolUseDecisionLedgerEntry); it
// does NOT run or block the command. A runtime's PreToolUse hook calls this
// (directly in-process, or via the `rallish gate` CLI's exit code) and is itself
// responsible for honouring the verdict. This is not a sandbox and does not
// intercept process exec.
//
// It is pure and cheap: O(len(command)) — it simply calls the two existing
// matchers (no new pattern logic, no shell parser) and picks the worse verdict.

import "fmt"

// actionVerdictSeverity orders the three verdicts so the combined decision can
// pick the strictest one when the two G6 checks disagree. Higher is more severe:
// deny > needs-hitl > allow. An unrecognised verdict sorts as least severe (-1)
// so it can never silently outrank a real deny.
func actionVerdictSeverity(v ActionVerdict) int {
	switch v {
	case ActionDeny:
		return 2
	case ActionNeedsHITL:
		return 1
	case ActionAllow:
		return 0
	default:
		return -1
	}
}

// ToolUseCheck names which G6 decision layer produced the governing verdict in a
// combined ToolUseDecision, so the audit record is unambiguous about what fired.
type ToolUseCheck string

// Known tool-use check sources.
const (
	// ToolUseCheckNone is the source when no check matched (verdict allow).
	ToolUseCheckNone ToolUseCheck = ""
	// ToolUseCheckAction is the source when the destructive-action deny-list
	// (ActionDecisionFor) produced the governing verdict.
	ToolUseCheckAction ToolUseCheck = "action"
	// ToolUseCheckSecret is the source when the secret-containment policy
	// (SecretDecisionFor) produced the governing verdict.
	ToolUseCheckSecret ToolUseCheck = "secret"
)

// ToolUseDecision is the sealed result of the combined G6 pre-execution check on
// a single PENDING tool/command invocation: the most-severe verdict across the
// action-gate and secret-containment policies, the rule that fired, a
// human-readable reason, and which check produced it. It carries no behaviour —
// a runtime hook honours it and rallish audits it (NewToolUseDecisionLedgerEntry).
type ToolUseDecision struct {
	// Verdict is the most-severe outcome across both G6 checks
	// (deny > needs-hitl > allow).
	Verdict ActionVerdict `json:"verdict"`
	// Check names which decision layer produced the governing verdict
	// (empty when Verdict is ActionAllow).
	Check ToolUseCheck `json:"check,omitempty"`
	// Rule is the short stable identifier of the matched policy rule from the
	// governing check (empty when Verdict is ActionAllow).
	Rule string `json:"rule,omitempty"`
	// Reason is a human-readable explanation of the governing decision.
	Reason string `json:"reason,omitempty"`
}

// IsBlocking reports whether the verdict means the runtime hook must not proceed
// without intervention: a deny (refuse) or a needs-hitl (require human gate).
// An allow is non-blocking. This is the single predicate a hook keys off; rallish
// does not itself act on it.
func (d ToolUseDecision) IsBlocking() bool {
	return d.Verdict == ActionDeny || d.Verdict == ActionNeedsHITL
}

// DecideToolUse is the combined G6 pre-execution decision for one PENDING command
// string: it runs BOTH ActionDecisionFor (destructive-action deny-list) and
// SecretDecisionFor (secret containment) and returns the MOST-SEVERE verdict
// (deny > needs-hitl > allow), tagged with the rule, reason, and which check
// fired. When both checks return the same severity, the action-gate result wins
// (it is the destructive-action layer and is evaluated first), keeping the choice
// deterministic.
//
// It is pure and cheap (O(len(command))): it composes the two existing matchers
// and selects between their results — no new pattern logic, no shell parser.
// When nothing matches, the verdict is allow with no rule (fail-open — see file
// header; rallish is the decision layer, not the enforcer).
//
// This is the enforcement-callable surface a runtime PreToolUse hook invokes
// (in-process, or via the `rallish gate` CLI exit code). rallish RETURNS the
// verdict and RECORDS it; the hook honours it. rallish never runs or blocks the
// command.
func DecideToolUse(command string) ToolUseDecision {
	action := ActionDecisionFor(command)
	secret := SecretDecisionFor(command)

	actionSeverity := actionVerdictSeverity(action.Verdict)
	secretSeverity := actionVerdictSeverity(secret.Verdict)

	// Most-severe-wins; ties go to the action gate (evaluated first, the
	// destructive-action layer). Both allow ⇒ a clean allow with no source tag.
	if secretSeverity > actionSeverity {
		return ToolUseDecision{
			Verdict: secret.Verdict,
			Check:   ToolUseCheckSecret,
			Rule:    secret.Rule,
			Reason:  secret.Reason,
		}
	}
	if action.Verdict == ActionAllow {
		return ToolUseDecision{Verdict: ActionAllow}
	}
	return ToolUseDecision{
		Verdict: action.Verdict,
		Check:   ToolUseCheckAction,
		Rule:    action.Rule,
		Reason:  action.Reason,
	}
}

// NewToolUseDecisionLedgerEntry records a non-allow combined ToolUseDecision as an
// append-only audit event (LedgerEventToolUseDecision). It is a DECISION RECORD:
// the Summary captures the most-severe verdict, which check fired, the matched
// rule, and the reason for the denied/flagged pending command. It does not imply
// rallish blocked execution — the runtime hook owns that. Callers should only
// record blocking decisions (deny / needs-hitl); an allow is not noise-recorded
// (false-positive guard — a safe command leaves no deny/HITL trace).
func NewToolUseDecisionLedgerEntry(at int64, cycleID, command string, decision ToolUseDecision) HarnessLedgerEntry {
	summary := fmt.Sprintf("%s [%s:%s]: %s", decision.Verdict, decision.Check, decision.Rule, decision.Reason)
	return NewHarnessLedgerEntry(at, cycleID, LedgerEventToolUseDecision, summary, []string{command})
}
