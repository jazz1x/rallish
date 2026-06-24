# Runbook: G6 Action-Gate — make destructive commands actually blocked

> **Prerequisites:** `rallish` on `PATH`, `jq` installed, a skill-aware coding CLI
> with a PreToolUse hook (this runbook targets **Claude Code**). [한국어](./runbook-action-gate.ko.md)

---

## 1. What this gives you

rallish ships a **pre-execution policy** (G6): a destructive-command deny-list plus
secret containment (`pkg/contract` → `DecideToolUse`). On its own rallish only
**declares + records** a verdict — it never executes or blocks a command. The
enforcement boundary lives in the runtime: a **PreToolUse hook** calls the gate
and honours its verdict.

This runbook wires the bundled hook so that, for example, `rm -rf /` is **refused**
before it runs and `git reset --hard origin/main` **asks you first**.

| rallish verdict | gate exit code | hook → Claude Code decision |
|---|---|---|
| `allow` | 0 | proceed (no prompt) |
| `needs-hitl` | 14 | `ask` — Claude Code prompts you to confirm |
| `deny` | 13 | `deny` — the tool call is refused, reason shown to the model |

The wrapper **fails safe**: if `jq` or `rallish` is missing, or the gate errors, it
escalates to `ask` (human confirmation) rather than silently allowing the command.

## 2. The bundled hook

`rallish bootstrap` (or `rallish skill install`) installs the wrapper to:

```
~/.claude/skills/rallish/scripts/gate-pretooluse.sh
```

It reads the PreToolUse JSON on stdin, extracts the Bash command, runs
`rallish gate tooluse --command "<cmd>"`, and maps the exit code onto Claude
Code's `permissionDecision` contract. Non-Bash tools pass through untouched.

## 3. Wire it in (one time)

Add a PreToolUse hook to your settings. Use `~/.claude/settings.json` to protect
**every** project, or a project `.claude/settings.json` to scope it to one repo:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "$HOME/.claude/skills/rallish/scripts/gate-pretooluse.sh"
          }
        ]
      }
    ]
  }
}
```

The `"matcher": "Bash"` scopes the hook to shell commands — the only thing the G6
deny-list governs. Restart Claude Code (or start a new session) so it reloads the
settings.

## 4. Verify it works

From a shell, feed the hook a sample PreToolUse payload and check the decision:

```bash
HOOK="$HOME/.claude/skills/rallish/scripts/gate-pretooluse.sh"

# deny — catastrophic
printf '{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}' | "$HOOK"
# → permissionDecision: "deny"   (reason: rm-rf-root)

# ask — risky-but-maybe-legit
printf '{"tool_name":"Bash","tool_input":{"command":"git reset --hard origin/main"}}' | "$HOOK"
# → permissionDecision: "ask"    (reason: git-hard-reset-remote)

# allow — ordinary command prints nothing and proceeds
printf '{"tool_name":"Bash","tool_input":{"command":"ls -la"}}' | "$HOOK"
```

Inside Claude Code, ask it to run `rm -rf /` in a throwaway directory: the call
should be refused with the gate's reason, never executed.

## 5. Record decisions to a cycle ledger (optional)

During an autonomous cycle, pass `--cycle-id` so every **blocking** decision is
appended to that cycle's append-only ledger as a `tooluse_decision` audit record
(safe `allow` decisions are never recorded — a false-positive guard). To do this
from the hook, set `RALLISH_CYCLE_ID` in the session and extend the wrapper's
`gate tooluse` call with `--cycle-id "$RALLISH_CYCLE_ID" --state-dir ~/.rallish/cycles`.

## 6. Boundary & limitations

- **rallish is the decision layer, not the executor.** Without this hook (or an
  equivalent one in your runtime) the policy is inert — it cannot block anything.
- The deny-list is intentionally narrow and high-precision (catastrophic patterns
  only) to avoid false positives; it is not a sandbox. Pair it with OS-level
  controls for untrusted input.
- The hook governs the **Bash** tool. File-editing tools are out of scope by
  design (covered by Claude Code's own permission model).
