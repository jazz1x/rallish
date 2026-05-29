package contract

// secret_containment.go — G6 secret-containment policy (decision + record layer).
//
// BOUNDARY (north-star G6, line 128-129): rallish DECLARES policy and RECORDS the
// decision; the runtime hook / sandbox ENFORCES. Nothing here reads, redacts, or
// blocks a secret. SecretDecisionFor is a pure classifier over the PENDING action
// edge (a command string) that flags two containment concerns:
//
//  1. Access to a sensitive credential path (~/.aws, ~/.ssh, ~/.netrc, .env,
//     credentials files, private keys).
//  2. An obviously-broad token grant (chmod 777 on a secret, a wildcard IAM
//     grant, exporting a credential into the environment for exfiltration).
//
// Like action_gate.go these are cheap O(len(command)) lowercase substring checks,
// never a parser. The verdict reuses ActionVerdict so a runtime hook has ONE
// decision shape across both G6 policies. The default is ActionAllow (fail-open):
// rallish is not the enforcer, so a false flag that blocks legitimate work (a
// deploy that legitimately reads ~/.aws/credentials) is the costlier error and is
// avoided by scoping rules narrowly and preferring needs-hitl over deny for reads.

import (
	"fmt"
	"strings"
)

// SecretDecision is the sealed result of a secret-containment check: a verdict,
// the matched rule, the sensitive marker that triggered it, and a reason. It
// carries no behaviour — a runtime hook honours it and rallish audits it.
type SecretDecision struct {
	// Verdict is allow / deny / needs-hitl (reuses the action-gate verdict set).
	Verdict ActionVerdict `json:"verdict"`
	// Rule is a short stable identifier for the matched policy rule
	// (empty when Verdict is ActionAllow).
	Rule string `json:"rule,omitempty"`
	// Marker is the sensitive path or token fragment that triggered the rule.
	Marker string `json:"marker,omitempty"`
	// Reason is a human-readable explanation of the decision.
	Reason string `json:"reason,omitempty"`
}

// sensitivePath pairs a credential-bearing path fragment to match against the
// normalised command with the canonical marker reported in the decision. Several
// fragments (e.g. "/.aws/" and "~/.aws") map to one canonical marker so the audit
// record is stable regardless of how the path was spelled. Fragments are
// deliberately specific (a leading dot or a known credential filename) so an
// unrelated path such as "environment.md" does not trip ".env"/".ssh".
type sensitivePath struct{ fragment, canonical string }

var sensitivePaths = []sensitivePath{
	{"/.aws/", "~/.aws"}, {"/.aws ", "~/.aws"}, {"~/.aws", "~/.aws"},
	{"/.ssh/", "~/.ssh"}, {"/.ssh ", "~/.ssh"}, {"~/.ssh", "~/.ssh"},
	{"/.netrc", "~/.netrc"}, {"~/.netrc", "~/.netrc"},
	{"/.gnupg/", "~/.gnupg"}, {"~/.gnupg", "~/.gnupg"},
	{"/.kube/config", "~/.kube/config"}, {"~/.kube/config", "~/.kube/config"},
	{"/.docker/config.json", "~/.docker/config.json"},
	{"/.npmrc", "~/.npmrc"}, {"~/.npmrc", "~/.npmrc"},
	{"/.pypirc", "~/.pypirc"}, {"~/.pypirc", "~/.pypirc"},
	{".env.production", ".env"}, {".env.local", ".env"}, {".env", ".env"},
	{"id_rsa", "id_rsa"}, {"id_ed25519", "id_ed25519"}, {"id_ecdsa", "id_ecdsa"},
	{"credentials.json", "credentials.json"}, {"service-account", "service-account"},
	{".pem", ".pem"}, {".p12", ".p12"}, {".pfx", ".pfx"}, {".keystore", ".keystore"},
}

// benignSecretVerbs are commands that may name a sensitive path but only set
// permissions, list, or create it — not read or transmit its contents. These do
// not warrant a HITL flag: tightening perms on ~/.ssh (`chmod 600 ~/.ssh/id_rsa`)
// or listing it is routine setup. World-permissive chmod is handled separately as
// a deny rule, so excluding chmod here does not weaken that protection.
var benignSecretVerbs = []string{"ls ", "ll ", "mkdir ", "chmod ", "chown ", "stat ", "test -", "touch "}

// secretWriteVerbs indicate a command is exfiltrating or transmitting a path
// rather than merely reading it locally; a sensitive path under these verbs is
// denied outright rather than sent for HITL.
var secretWriteVerbs = []string{"curl", "wget", "scp", "rsync", "nc ", "netcat", "base64", "| mail", "ftp "}

// broadTokenRules flag over-broad permission/credential grants regardless of a
// specific sensitive path.
var broadTokenRules = []struct {
	id     string
	marker string
	reason string
	match  func(norm string) bool
}{
	{
		id:     "chmod-world-on-secret",
		marker: "chmod 777",
		reason: "world-writable/readable permission on a credential exposes it to all local users",
		match: func(norm string) bool {
			return (strings.Contains(norm, "chmod 777") || strings.Contains(norm, "chmod -r 777") ||
				strings.Contains(norm, "chmod a+rwx")) && containsSensitivePath(norm)
		},
	},
	{
		id:     "iam-wildcard-grant",
		marker: "action:* / resource:*",
		reason: "wildcard IAM action/resource grant is an over-broad token",
		match: func(norm string) bool {
			return strings.Contains(norm, "\"action\": \"*\"") ||
				strings.Contains(norm, "\"action\":\"*\"") ||
				strings.Contains(norm, "\"resource\": \"*\"") ||
				strings.Contains(norm, "\"resource\":\"*\"")
		},
	},
}

// SecretDecisionFor classifies a single PENDING command string against the G6
// secret-containment policy. It is pure and cheap: O(len(command)). The first
// matching rule wins; when nothing matches the action is allowed (fail-open —
// see file header).
//
// Precedence: a broad-token grant (deny) > exfiltration of a sensitive path
// (deny) > local access to a sensitive path (needs-hitl).
//
// This is a callable surface a future runtime PreToolUse hook invokes. rallish
// neither reads nor blocks the secret; the hook honours the verdict and rallish
// records it (see NewSecretFlaggedLedgerEntry).
func SecretDecisionFor(command string) SecretDecision {
	norm := normalizeCommand(command)
	if norm == "" {
		return SecretDecision{Verdict: ActionAllow}
	}

	for _, rule := range broadTokenRules {
		if rule.match(norm) {
			return SecretDecision{Verdict: ActionDeny, Rule: rule.id, Marker: rule.marker, Reason: rule.reason}
		}
	}

	marker, found := firstSensitivePath(norm)
	if !found {
		return SecretDecision{Verdict: ActionAllow}
	}

	if hasExfilVerb(norm) {
		return SecretDecision{
			Verdict: ActionDeny,
			Rule:    "secret-exfiltration",
			Marker:  marker,
			Reason:  "transmitting a credential-bearing path off-host is exfiltration",
		}
	}

	// A command that only sets permissions, lists, or creates the path is routine
	// setup, not a read/exfil — do not raise a HITL flag (false-positive guard).
	if hasBenignSecretVerb(norm) {
		return SecretDecision{Verdict: ActionAllow}
	}

	return SecretDecision{
		Verdict: ActionNeedsHITL,
		Rule:    "sensitive-path-access",
		Marker:  marker,
		Reason:  "access to a credential-bearing path; confirm it is intended",
	}
}

// containsSensitivePath reports whether the normalised command references any
// known sensitive path fragment.
func containsSensitivePath(norm string) bool {
	_, found := firstSensitivePath(norm)
	return found
}

// firstSensitivePath returns the canonical marker of the first sensitive path
// fragment found in the normalised command, in sensitivePaths order, and whether
// one was found.
func firstSensitivePath(norm string) (string, bool) {
	for _, p := range sensitivePaths {
		if strings.Contains(norm, p.fragment) {
			return p.canonical, true
		}
	}
	return "", false
}

// hasExfilVerb reports whether the normalised command uses a verb that
// transmits data off-host.
func hasExfilVerb(norm string) bool {
	for _, v := range secretWriteVerbs {
		if strings.Contains(norm, v) {
			return true
		}
	}
	return false
}

// hasBenignSecretVerb reports whether the normalised command starts with a verb
// that only manages a path (permissions/listing/creation) rather than reading or
// transmitting its contents.
func hasBenignSecretVerb(norm string) bool {
	for _, v := range benignSecretVerbs {
		if strings.HasPrefix(norm, v) {
			return true
		}
	}
	return false
}

// NewSecretFlaggedLedgerEntry records a non-allow SecretDecision as an
// append-only audit event (LedgerEventSecretFlagged). It is a DECISION RECORD:
// the Summary captures the verdict, rule, marker, and reason for the flagged
// pending command. It does not imply rallish read or blocked the secret — the
// runtime hook owns that.
func NewSecretFlaggedLedgerEntry(at int64, cycleID, command string, decision SecretDecision) HarnessLedgerEntry {
	summary := fmt.Sprintf("%s [%s] %s: %s", decision.Verdict, decision.Rule, decision.Marker, decision.Reason)
	return NewHarnessLedgerEntry(at, cycleID, LedgerEventSecretFlagged, summary, []string{command})
}
