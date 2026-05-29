package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/jazz1x/rallish/internal/cycle"
	"github.com/jazz1x/rallish/pkg/contract"
	"github.com/spf13/cobra"
)

// gate.go implements `rallish gate tooluse`: the thin INVOCABLE surface a runtime
// PreToolUse hook calls to gate a pending tool/command invocation against the G6
// policy (DecideToolUse: the combined destructive-action deny-list + secret
// containment, most-severe-wins).
//
// ENFORCEMENT-CALLABLE, NOT THE ENFORCER. Per north-star §G6, rallish DECLARES the
// policy, RETURNS the verdict, and RECORDS the decision; the runtime hook / sandbox
// ENFORCES. This command does NOT execute, intercept, or block the command — it
// prints the decision as JSON and exits with a verdict-derived code so a shell
// PreToolUse hook can gate on it:
//
//	decision=$(rallish gate tooluse --command "$TOOL_CMD") || {
//	  code=$?                       # 13 = deny, 14 = needs-hitl
//	  echo "$decision"; exit "$code"   # the HOOK refuses / escalates
//	}
//
// rallish never refuses the command itself; the hook honours the exit code/JSON.
// When a cycle context is supplied (--cycle-id), a blocking decision (deny /
// needs-hitl) is also appended to that cycle's append-only ledger as a
// tooluse_decision audit record. A safe (allow) command is never recorded
// (false-positive guard) and exits 0.

// gateTooluseOptions bundles the inputs to one `gate tooluse` invocation.
// recordLedger is the seam tests inject to assert a blocking decision is recorded
// and a safe one is not, without writing real files.
type gateTooluseOptions struct {
	command  string
	cycleID  string
	stateDir string
	// recordLedger appends a tooluse_decision entry for a blocking decision. When
	// nil it defaults to appending to the on-disk cycle ledger under stateDir; it
	// is only consulted when cycleID is non-empty and the decision is blocking.
	recordLedger func(opts gateTooluseOptions, entry contract.HarnessLedgerEntry) error
}

// GateCmd returns the parent `gate` cobra command. It is the runtime-hook-facing
// surface for the G6 pre-execution policy; today it carries the single `tooluse`
// subcommand (the combined action + secret decision a PreToolUse hook gates on).
func GateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gate",
		Short: "Pre-execution policy gate for a runtime PreToolUse hook (decide + record; the hook enforces)",
		Long: strings.TrimSpace(`
Evaluate a pending tool/command invocation against the G6 pre-execution policy
and return a verdict a runtime's PreToolUse hook can gate on.

BOUNDARY: rallish DECLARES the policy, RETURNS the verdict, and RECORDS the
decision; the runtime hook / sandbox ENFORCES. These commands never execute,
intercept, or block the command — they print the decision and set an exit code
the calling hook honours.`),
	}
	cmd.AddCommand(GateTooluseCmd())
	return cmd
}

// GateTooluseCmd returns the `gate tooluse` subcommand.
func GateTooluseCmd() *cobra.Command {
	opts := gateTooluseOptions{}
	cmd := &cobra.Command{
		Use:   "tooluse",
		Short: "Decide a pending command against the combined G6 action + secret policy",
		Long: strings.TrimSpace(`
Run the combined G6 pre-execution check (DecideToolUse) over a pending command:
both the destructive-action deny-list and the secret-containment policy, returning
the MOST-SEVERE verdict (deny > needs-hitl > allow) with the matched rule, reason,
and which check fired. The decision is printed as JSON on stdout.

This is the invocable surface a runtime PreToolUse hook calls. rallish does NOT
run or block the command; it returns the verdict + (optionally) records it, and
the hook honours the exit code:

  0   allow      (no dangerous/secret pattern matched; the hook may proceed)
  13  deny       (a catastrophic pattern matched; the hook must refuse)
  14  needs-hitl (risky-but-maybe-legitimate; the hook must escalate to a human)
  (1  operational error, e.g. unreadable ledger — from the root command)

When --cycle-id is given, a BLOCKING decision (deny / needs-hitl) is also appended
to that cycle's append-only ledger as a tooluse_decision audit record. A safe
(allow) command is never recorded.`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGateTooluse(opts, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&opts.command, "command", "", "The pending tool/command invocation to decide (required)")
	cmd.Flags().StringVar(&opts.cycleID, "cycle-id", "", "Record a blocking decision to this cycle's ledger (optional)")
	cmd.Flags().StringVar(&opts.stateDir, "state-dir", "tmp", "Directory holding cycle-<id>-ledger.jsonl (used with --cycle-id)")
	_ = cmd.MarkFlagRequired("command")
	return cmd
}

// Exit codes for `gate tooluse`. SINGLE SOURCE OF TRUTH for the verdict→code
// mapping. deny / needs-hitl reuse the same numeric codes as the cycle-run halt
// classes (13 gate-failure, 14 unparseable) only by coincidence of intent — they
// are defined here independently so this command's contract is self-contained and
// a hook keys off these documented values, not a shared literal.
const (
	exitToolUseAllow     = 0  // no dangerous/secret pattern matched; hook may proceed
	exitToolUseDeny      = 13 // catastrophic pattern matched; hook must refuse
	exitToolUseNeedsHITL = 14 // risky-but-maybe-legit; hook must escalate to a human
)

// exitCodeForToolUseVerdict maps a combined verdict to a process exit code. This
// is the ONE switch; the command and its docs reference it, never a scattered
// literal. An unrecognised verdict is treated as blocking (deny code) so a future
// or malformed verdict fails safe for the calling hook rather than passing as 0.
func exitCodeForToolUseVerdict(v contract.ActionVerdict) int {
	switch v {
	case contract.ActionAllow:
		return exitToolUseAllow
	case contract.ActionDeny:
		return exitToolUseDeny
	case contract.ActionNeedsHITL:
		return exitToolUseNeedsHITL
	default:
		return exitToolUseDeny
	}
}

// runGateTooluse is the body of `gate tooluse`, factored out for testing. It
// decides the command (pure DecideToolUse), prints the decision as JSON, records
// a blocking decision to the cycle ledger when a cycle context is supplied, and
// returns an *exitCodeError carrying the verdict-derived exit code for a blocking
// verdict (allow returns nil ⇒ exit 0). The print happens before any record so a
// hook always receives the JSON even if the optional ledger write fails.
func runGateTooluse(opts gateTooluseOptions, out io.Writer) error {
	decision := contract.DecideToolUse(opts.command)

	encoded, err := json.Marshal(decision)
	if err != nil {
		return fmt.Errorf("encode tooluse decision: %w", err)
	}
	if _, err := fmt.Fprintln(out, string(encoded)); err != nil {
		return fmt.Errorf("write tooluse decision: %w", err)
	}

	// Record only a BLOCKING decision, and only when a cycle context is supplied.
	// A safe (allow) command is never recorded — false-positive guard: rallish is
	// the decision layer, not the enforcer, and must not noise-record legitimate
	// work as a deny/HITL event.
	if opts.cycleID != "" && decision.IsBlocking() {
		entry := contract.NewToolUseDecisionLedgerEntry(
			time.Now().UnixMilli(), opts.cycleID, opts.command, decision)
		record := opts.recordLedger
		if record == nil {
			record = appendToolUseLedgerEntry
		}
		if err := record(opts, entry); err != nil {
			return fmt.Errorf("record tooluse decision: %w", err)
		}
	}

	code := exitCodeForToolUseVerdict(decision.Verdict)
	if code == exitToolUseAllow {
		return nil
	}
	// Carry the verdict-derived exit code out to the root command, which honours
	// any error exposing ExitCode() int (see cmd/rallish/main.go) — the same
	// exit-code-carrier pattern `cycle run --once` uses. A dedicated type (not the
	// cycle-halt exitCodeError) keeps the message honest: this is a gate VERDICT a
	// hook must honour, not a halted cycle.
	return &gateBlockError{code: code, verdict: decision.Verdict, check: decision.Check}
}

// gateBlockError carries a blocking gate verdict's exit code out of a cobra RunE.
// It implements the same ExitCode() int contract main.go's root branch honours,
// so a calling PreToolUse hook receives the documented verdict code. The message
// names the verdict and check honestly (rallish returns a verdict; it does not
// itself block — the hook does).
type gateBlockError struct {
	code    int
	verdict contract.ActionVerdict
	check   contract.ToolUseCheck
}

func (e *gateBlockError) Error() string {
	return fmt.Sprintf("tool-use gate verdict: %s [%s] (exit %d) — the runtime hook must honour it", e.verdict, e.check, e.code)
}

// ExitCode reports the process exit code this verdict should produce.
func (e *gateBlockError) ExitCode() int { return e.code }

// appendToolUseLedgerEntry is the default recordLedger: it appends the entry to
// the cycle's on-disk ledger (the same LedgerFileSync + tamper-evidence chain the
// orchestrator and `cycle run --once` use), so a gated decision is auditable in
// the same append-only log.
func appendToolUseLedgerEntry(opts gateTooluseOptions, entry contract.HarnessLedgerEntry) error {
	ledgerPath := filepath.Join(opts.stateDir, fmt.Sprintf("cycle-%s-ledger.jsonl", opts.cycleID))
	ledger := cycle.NewLedgerFileSync(ledgerPath)
	if err := ledger.Append(entry); err != nil {
		return fmt.Errorf("append to %q: %w", ledgerPath, err)
	}
	return nil
}
