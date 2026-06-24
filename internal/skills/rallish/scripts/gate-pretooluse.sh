#!/bin/sh
# rallish G6 action-gate — Claude Code PreToolUse hook wrapper.
#
# Reads the PreToolUse JSON payload on stdin, runs `rallish gate tooluse` over the
# pending Bash command, and maps rallish's verdict onto Claude Code's
# permissionDecision contract so a destructive command is actually refused.
#
# BOUNDARY (north-star §G6): rallish DECIDES (returns a verdict + records it);
# THIS HOOK enforces. rallish never executes or blocks the command itself — this
# wrapper turns its verdict into a Claude Code allow / deny / ask decision.
#
# Wire it once in ~/.claude/settings.json (user-wide) or a project
# .claude/settings.json — see docs/runbook-action-gate.md for the exact snippet:
#
#   "hooks": { "PreToolUse": [ { "matcher": "Bash", "hooks": [ { "type": "command",
#     "command": "$HOME/.claude/skills/rallish/scripts/gate-pretooluse.sh" } ] } ] }
#
# Requires: `jq` and `rallish` on PATH. If either is missing, or rallish errors,
# the hook FAILS SAFE by escalating to a human ("ask") rather than silently
# allowing or hard-blocking every command.

set -u

# emit DECISION REASON — print the Claude Code PreToolUse decision JSON on stdout.
# DECISION is allow | deny | ask.
emit() {
	jq -n --arg d "$1" --arg r "$2" '{
		hookSpecificOutput: {
			hookEventName: "PreToolUse",
			permissionDecision: $d,
			permissionDecisionReason: $r
		}
	}'
}

# emit_raw is the jq-free fallback used only when jq itself is unavailable.
emit_raw() {
	printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"%s","permissionDecisionReason":"%s"}}\n' "$1" "$2"
}

input=$(cat)

if ! command -v jq >/dev/null 2>&1; then
	emit_raw ask "rallish action gate: jq not found — cannot evaluate; asking for confirmation"
	exit 0
fi

tool=$(printf '%s' "$input" | jq -r '.tool_name // empty')

# The gate only governs shell commands; anything else proceeds untouched.
if [ "$tool" != "Bash" ]; then
	exit 0
fi

cmd=$(printf '%s' "$input" | jq -r '.tool_input.command // empty')
if [ -z "$cmd" ]; then
	exit 0
fi

if ! command -v rallish >/dev/null 2>&1; then
	emit ask "rallish action gate: rallish not on PATH — cannot evaluate; asking for confirmation"
	exit 0
fi

# rallish prints the decision JSON on stdout and exits 0 (allow) / 13 (deny) /
# 14 (needs-hitl). Capture both without tripping into an unhandled error.
if decision=$(rallish gate tooluse --command "$cmd" 2>/dev/null); then
	code=0
else
	code=$?
fi

reason=$(printf '%s' "$decision" | jq -r '(.reason // "matched G6 policy") + " [rule: " + (.rule // "?") + ", check: " + (.check // "?") + "]"' 2>/dev/null)
[ -n "$reason" ] || reason="matched rallish G6 policy"

case "$code" in
	0)
		# allow — proceed untouched (no output ⇒ Claude Code's normal flow).
		exit 0
		;;
	13)
		emit deny "rallish G6 deny: $reason"
		exit 0
		;;
	14)
		emit ask "rallish G6 needs human review: $reason"
		exit 0
		;;
	*)
		emit ask "rallish action gate errored (exit $code) — asking for confirmation"
		exit 0
		;;
esac
