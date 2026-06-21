---
name: rallish-mcp
description: >
  Participate in a rallish rally via the MCP 2025-03-26 server surface.
  This skill uses `rallish rally mcp-agent` under the hood, so the agent does
  not need to hand-roll SSE transport or JSON-RPC envelopes. Works alongside
  the existing `rallish` skill (HTTP/SSE rally) and shares the same daemon.
  Triggers: "rally mcp", "mcp rally", "MCP로 랠리", "랠리 MCP"
version: 0.1.0
ssl:
  scheduling:
    anti_triggers:
      - "Modifying the broker MCP surface — see docs/prd-rally-mcp.md and internal/broker/mcp.go"
      - "Generic Go coding conventions — see AGENTS.md"
      - "Plain HTTP/SSE rally (no MCP cue) — use the rallish skill"
      - "Autonomous cycle, squash, or non-rally tasks — use the appropriate skill"
      - "Modifying daemon lifecycle or CLI flags — see internal/cli/daemon.go and internal/cli/rally_mcp_agent.go"
      - "Skill audit / SSL inspection — use skill-audit"
  structural:
    scenes: [DaemonCheck, Create, Join, Work, Done, Status]
    resumable: false
    branches:
      - "Resolve binary: try `command -v rallish`, then `$PWD/dist/rallish`, then `make build`"
      - "DaemonCheck: if `~/.rallish/port` is missing, start `rallish daemon` in the background and wait for the port file"
      - "Create: requires `--participants` with ≥2 names; optional `--first`, `--task`, `--repo`"
      - "Create: parse `id` from the RallySession JSON stdout"
      - "Join: blocks until baton or `--timeout`; exit 2 = timeout; exit 1 = not found / not member / already connected"
      - "Done: requires `--as` current holder; optional `--handoff-to`; error message includes actual holder"
      - "Status: requires `--session-id`; prints RallySession JSON"
      - "Interrupt: `rally mcp-agent --mode interrupt --session-id <SID>` stops the rally"
  logical:
    tools: [Bash]
    side_effects:
      reads: ["~/.rallish/socket", "~/.rallish/port"]
      writes:
        - "~/.rallish/port — written by the daemon on startup"
        - "~/.rallish/rallish.sock — created by the daemon (mode 0600)"
        - "~/.rallish/sessions/<id>/log.jsonl — written by the daemon session store"
        - "~/.rallish/daemon.log — when daemon output is redirected"
      network: ["local HTTP/SSE loopback to the rallish daemon via the rallish binary"]
    idempotent: false
    rollback: "Interrupt the rally with `rallish rally mcp-agent --mode interrupt --session-id <SID>`"
---

# rallish-mcp — Rally via MCP

This skill lets an agent join a rallish rally through the daemon's MCP
2025-03-26 surface. The agent only invokes `rallish rally mcp-agent`; the
subcommand handles SSE transport, JSON-RPC, and tool dispatch internally.

## Resolve the `rallish` binary

Before any command, pick the runnable path:

1. Try `command -v rallish`. If found, use bare `rallish`.
2. Else look at `$PWD/dist/rallish`. If present, use that absolute path.
3. Else build it: `make build` in the repo root, then use `$PWD/dist/rallish`.

Refer to the chosen path as `$RALLISH` below.

## Conversation state

- `SID`: rally session id
- `ROLE`: participant name for this agent (e.g. `alice`)
- `PARTNERS`: comma-separated list of all participants

## Trigger — "rally mcp" / "MCP로 랠리"

### 1. Ensure daemon is running

`rallish doctor` renders a status table and exits 0 even when the daemon is
absent, so do not rely on `doctor || daemon`. Instead probe the daemon port
file directly:

```sh
if [ ! -f ~/.rallish/port ]; then
    $RALLISH daemon > ~/.rallish/daemon.log 2>&1 &
    while [ ! -f ~/.rallish/port ]; do sleep 0.5; done
fi
```

Wait until `~/.rallish/port` exists before calling any `mcp-agent` command.

### 2. Create a rally session

```sh
SID=$($RALLISH rally mcp-agent --mode create \
  --participants alice,bob \
  --first alice \
  --task "[pattern:cycle] refactor auth")
```

`--participants` requires at least two comma-separated names. `--first`,
`--task`, and `--repo` are optional. The command prints a `RallySession` JSON
object. Extract `id` and store it in `SID`.

### 3. When it is your turn, wait for the baton

```sh
$RALLISH rally mcp-agent --mode join --session-id "$SID" --as "$ROLE"
```

This blocks until the baton arrives. The output is a `BatonEvent` JSON object:

```json
{"turn_n": 2, "from": "alice", "note": "implemented login"}
```

If the command exits with code 2, the timeout fired and you should ask the
user whether to keep waiting or stop. Exit 1 means the session is missing,
the participant is not a member, or the participant already has an active
connection.

### 4. Do your work

Perform the task described in the baton note or in the session task.

### 5. Hand off the baton

```sh
$RALLISH rally mcp-agent --mode done \
  --session-id "$SID" \
  --as "$ROLE" \
  --note "summary of what I did" \
  --handoff-to bob
```

The output is the updated `RallySession` JSON. If you are not the holder, the
command fails with a "not the current baton holder" message that includes the
actual holder. Use `--handoff-to` to pass the baton to a specific participant;
otherwise the next holder is determined by the rally order.

### 6. Poll with status (optional)

When not blocking on `join`, you can check the session snapshot:

```sh
$RALLISH rally mcp-agent --mode status --session-id "$SID"
```

### 7. Interrupt a stuck rally

If a session is stuck and you need to stop it, run:

```sh
$RALLISH rally mcp-agent --mode interrupt --session-id "$SID"
```

## Coexistence with the HTTP/SSE rally skill

A rally session created via `rally mcp-agent --mode create` can be joined by a
participant using `rallish rally join` (HTTP/SSE), and vice versa. The MCP and
HTTP surfaces share the same in-memory rally state.
