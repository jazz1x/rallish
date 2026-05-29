package contract

// action_gate.go — G6 pre-execution action-gate policy (decision + record layer).
//
// BOUNDARY (north-star G6, line 129): rallish DECLARES policy and RECORDS the
// decision; the runtime hook / sandbox ENFORCES. Nothing in this file executes,
// intercepts, or blocks a command. ActionDecision is a pure classifier over the
// PENDING action edge (a command string a runtime is about to run) — the
// "match a dangerous-action pattern on the pending edge before it is added"
// predicate from the Work-Graph model. The future PreToolUse hook calls this and
// is itself responsible for honouring the verdict; rallish only audits it.
//
// The matchers are cheap O(len(command)) lowercase substring / prefix / token
// checks — never subgraph isomorphism, never a shell parser (a parser would be a
// new attack surface). Parse-don't-validate: a command we cannot classify as
// dangerous is allowed (fail-open is correct here because rallish is NOT the
// enforcement layer — a missed deny is a hook concern, a false deny blocks
// legitimate work, which is the costlier error for a decision layer).

import (
	"fmt"
	"strings"
)

// ActionVerdict is the outcome of a pre-execution action-gate check.
// Construct via ParseActionVerdict; the zero value is invalid.
type ActionVerdict string

// Known action verdicts.
const (
	// ActionAllow means no dangerous pattern matched; the runtime may proceed.
	ActionAllow ActionVerdict = "allow"
	// ActionDeny means a catastrophic, non-recoverable pattern matched; the
	// runtime hook should refuse to run the command.
	ActionDeny ActionVerdict = "deny"
	// ActionNeedsHITL means the action is risky but may be legitimate; the
	// runtime hook should require human-in-the-loop confirmation before running.
	ActionNeedsHITL ActionVerdict = "needs-hitl"
)

// ParseActionVerdict converts a string to a validated ActionVerdict.
// Returns ErrInvalidActionVerdict if s is not recognised.
func ParseActionVerdict(s string) (ActionVerdict, error) {
	switch ActionVerdict(s) {
	case ActionAllow, ActionDeny, ActionNeedsHITL:
		return ActionVerdict(s), nil
	default:
		return "", fmt.Errorf("action verdict %q: %w", s, ErrInvalidActionVerdict)
	}
}

// String returns the canonical string representation.
func (v ActionVerdict) String() string { return string(v) }

// ActionDecision is the sealed result of a pre-execution action-gate check:
// a verdict plus a human-readable reason and the rule that fired. It carries no
// behaviour — it is a record a runtime hook honours and rallish audits.
type ActionDecision struct {
	// Verdict is allow / deny / needs-hitl.
	Verdict ActionVerdict `json:"verdict"`
	// Rule is a short stable identifier for the matched policy rule
	// (empty when Verdict is ActionAllow).
	Rule string `json:"rule,omitempty"`
	// Reason is a human-readable explanation of the decision.
	Reason string `json:"reason,omitempty"`
}

// actionRule is one entry in the deny-list policy table. Each rule owns a single
// predicate over the normalised command (SRP); the table is scanned in order and
// the first match wins, so more-severe rules are listed before softer ones.
type actionRule struct {
	id      string
	verdict ActionVerdict
	reason  string
	match   func(norm string) bool
}

// protectedPushRefs are the git refs a force-push must never rewrite. Force-push
// to any other ref (a feature branch) is legitimate and intentionally allowed.
var protectedPushRefs = []string{"main", "master", "release/", "release ", "origin/main", "origin/master"}

// destructiveActionRules is the ordered G6 deny-list. Patterns are matched
// against a normalised (lower-cased, whitespace-collapsed) command string.
//
// Each predicate is intentionally narrow to avoid false-positives that would
// block legitimate work: e.g. we deny `rm -rf /` and `rm -rf ~` but allow
// `rm -rf ./build` or `rm -rf node_modules`; we deny force-push only to
// protected refs, not to feature branches.
var destructiveActionRules = []actionRule{
	{
		id:      "rm-rf-root",
		verdict: ActionDeny,
		reason:  "recursive force-remove targeting a filesystem or home root is non-recoverable",
		match:   matchRecursiveRemoveRoot,
	},
	{
		id:      "fork-bomb",
		verdict: ActionDeny,
		reason:  "shell fork bomb exhausts process table",
		match:   matchForkBomb,
	},
	{
		id:      "disk-overwrite",
		verdict: ActionDeny,
		reason:  "raw write to a block device destroys data irrecoverably",
		match:   matchDiskOverwrite,
	},
	{
		id:      "git-force-push-protected",
		verdict: ActionDeny,
		reason:  "force-push to a protected ref (main/master/release) rewrites shared history",
		match:   matchForcePushProtected,
	},
	{
		id:      "git-hard-reset-remote",
		verdict: ActionNeedsHITL,
		reason:  "hard reset to a remote ref discards uncommitted and local work",
		match:   matchGitHardResetRemote,
	},
	{
		id:      "sql-drop-truncate",
		verdict: ActionNeedsHITL,
		reason:  "DROP/TRUNCATE destroys table data; confirm target is not production",
		match:   matchSQLDrop,
	},
}

// ActionDecisionFor classifies a single PENDING command string against the G6
// deny-list. It is pure and cheap: O(len(command)). The first matching rule wins;
// when nothing matches the action is allowed (fail-open — see file header).
//
// This is the callable surface a future runtime PreToolUse hook invokes. rallish
// neither runs nor blocks the command; the hook honours the verdict and rallish
// records it (see NewActionDeniedLedgerEntry).
func ActionDecisionFor(command string) ActionDecision {
	norm := normalizeCommand(command)
	if norm == "" {
		return ActionDecision{Verdict: ActionAllow}
	}
	for _, rule := range destructiveActionRules {
		if rule.match(norm) {
			return ActionDecision{Verdict: rule.verdict, Rule: rule.id, Reason: rule.reason}
		}
	}
	return ActionDecision{Verdict: ActionAllow}
}

// normalizeCommand lower-cases the command and collapses runs of whitespace to a
// single space so patterns are insensitive to tabs, newlines, and indentation.
func normalizeCommand(command string) string {
	return strings.ToLower(strings.Join(strings.Fields(command), " "))
}

// hasRecursiveForceFlags reports whether a normalised `rm` invocation carries
// both recursive and force semantics, tolerating combined (`-rf`, `-fr`) and
// split (`-r -f`, `--recursive --force`) flag spellings. Combined short clusters
// are handled by inspecting the letters inside each single-dash token (so `-rf`
// yields both r and f), which a naive `Contains("-f")` substring check misses.
func hasRecursiveForceFlags(norm string) bool {
	recursive := strings.Contains(norm, "--recursive")
	force := strings.Contains(norm, "--force")
	for _, tok := range strings.Fields(norm) {
		if len(tok) < 2 || tok[0] != '-' || tok[1] == '-' {
			continue // not a short flag cluster
		}
		letters := tok[1:]
		if strings.ContainsRune(letters, 'r') || strings.ContainsRune(letters, 'R') {
			recursive = true
		}
		if strings.ContainsRune(letters, 'f') {
			force = true
		}
	}
	return recursive && force
}

// matchRecursiveRemoveRoot denies `rm` (recursive+force) whose target is the
// filesystem root, a home root, or an unscoped glob of one — but allows a
// recursive remove of a scoped relative or named subdirectory.
func matchRecursiveRemoveRoot(norm string) bool {
	if !strings.HasPrefix(norm, "rm ") && norm != "rm" {
		return false
	}
	if !hasRecursiveForceFlags(norm) {
		return false
	}
	// Dangerous targets: the literal roots and their immediate wildcards.
	dangerousTargets := []string{" /", " /*", " ~", " ~/", " ~/*", " $home", " $home/", " $home/*", " /*"}
	for _, t := range dangerousTargets {
		if strings.HasSuffix(norm, t) || strings.Contains(norm, t+" ") {
			return true
		}
	}
	return false
}

// matchForkBomb denies the classic `:(){ :|:& };:` bash fork bomb and the
// function-named variant, tolerating whitespace differences.
func matchForkBomb(norm string) bool {
	// After normalisation the canonical bomb collapses to ":(){ :|:& };:".
	collapsed := strings.ReplaceAll(norm, " ", "")
	return strings.Contains(collapsed, ":(){:|:&};:") ||
		strings.Contains(collapsed, ":(){:|:&}:") ||
		strings.Contains(collapsed, "(){|&};")
}

// matchDiskOverwrite denies `dd` / `mkfs` / redirection writing to a raw block
// device under /dev/ (disk, sd*, nvme*, hd*), which is unrecoverable.
func matchDiskOverwrite(norm string) bool {
	if strings.HasPrefix(norm, "dd ") && strings.Contains(norm, "of=/dev/") {
		return true
	}
	if strings.HasPrefix(norm, "mkfs") && strings.Contains(norm, "/dev/") {
		return true
	}
	if strings.Contains(norm, "> /dev/sd") || strings.Contains(norm, ">/dev/sd") ||
		strings.Contains(norm, "> /dev/nvme") || strings.Contains(norm, ">/dev/nvme") {
		return true
	}
	return false
}

// matchForcePushProtected denies a git force-push whose command targets a
// protected ref. A force-push to a feature branch carries no protected token and
// is allowed.
func matchForcePushProtected(norm string) bool {
	if !strings.HasPrefix(norm, "git push") && !strings.Contains(norm, " git push") {
		return false
	}
	forced := strings.Contains(norm, "--force") || strings.Contains(norm, "-f ") ||
		strings.HasSuffix(norm, " -f") || strings.Contains(norm, "--force-with-lease")
	if !forced {
		return false
	}
	for _, ref := range protectedPushRefs {
		if strings.Contains(norm, ref) {
			return true
		}
	}
	return false
}

// matchGitHardResetRemote flags `git reset --hard` to a remote-tracking ref for
// HITL: it silently discards local work but is sometimes intentional.
func matchGitHardResetRemote(norm string) bool {
	if !strings.Contains(norm, "git reset") || !strings.Contains(norm, "--hard") {
		return false
	}
	return strings.Contains(norm, "origin/") || strings.Contains(norm, "upstream/")
}

// matchSQLDrop flags DROP TABLE / DROP DATABASE / TRUNCATE TABLE for HITL.
// It deliberately requires the SQL object keyword (TABLE/DATABASE) so the GNU
// coreutils `truncate` file utility (`truncate -s 0 file`) is NOT flagged.
func matchSQLDrop(norm string) bool {
	return strings.Contains(norm, "drop table") ||
		strings.Contains(norm, "drop database") ||
		strings.Contains(norm, "truncate table")
}

// NewActionDeniedLedgerEntry records a non-allow ActionDecision as an append-only
// audit event (LedgerEventActionDenied). It is a DECISION RECORD: the Summary
// captures the verdict, rule, and reason for the denied/flagged pending command.
// It does not imply rallish blocked execution — the runtime hook owns that.
func NewActionDeniedLedgerEntry(at int64, cycleID, command string, decision ActionDecision) HarnessLedgerEntry {
	summary := fmt.Sprintf("%s [%s]: %s", decision.Verdict, decision.Rule, decision.Reason)
	entry := NewHarnessLedgerEntry(at, cycleID, LedgerEventActionDenied, summary, []string{command})
	return entry
}
