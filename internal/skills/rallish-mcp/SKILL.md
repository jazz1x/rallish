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
  structural:
    scenes: [DaemonCheck, Create, Join, Work, Done, Status]
    resumable: true
    branches:
      - "DaemonCheck: daemon not running → run `rallish daemon &` in background"
      - "Create: parse JSON stdout of `rally mcp-agent --mode create` for session id"
      - "Join: `rally mcp-agent --mode join` blocks until baton arrives or timeout; exit 2 means timeout"
      - "Done: holder mismatch → tool returns error; report actual holder and yield"
      - "Status: use `--mode status` to poll when not blocking"
  logical:
    tools: [Bash, Read]
    side_effects:
      reads: ["~/.rallish/socket", "~/.rallish/port"]
      writes:
        - "~/.rallish/rallish.sock (mode 0600, owned by daemon)"
      network: []
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

```sh
$RALLISH doctor || ($RALLISH daemon &)
```

Wait a moment for the daemon to write `~/.rallish/port`.

### 2. Create a rally session

```sh
SID=$($RALLISH rally mcp-agent --mode create \
  --participants alice,bob \
  --first alice \
  --task "[pattern:cycle] refactor auth")
```

The command prints a `RallySession` JSON object. Extract `id` and store it in
`SID`.

### 3. When it is your turn, wait for the baton

```sh
$RALLISH rally mcp-agent --mode join --session-id "$SID" --as "$ROLE"
```

This blocks until the baton arrives. The output is a `BatonEvent` JSON object:

```json
{"turn_n": 2, "from": "alice", "note": "implemented login"}
```

If the command exits with code 2, the timeout fired and you should ask the
user whether to keep waiting or stop.

### 4. Do your work

Perform the task described in the baton note or in the session task.

### 5. Hand off the baton

```sh
$RALLISH rally mcp-agent --mode done \
  --session-id "$SID" \
  --as "$ROLE" \
  --note "summary of what I did"
```

The output is the updated `RallySession` JSON. If you are not the holder, the
command fails with a "not the current baton holder" message.

### 6. Poll with status (optional)

When not blocking on `join`, you can check the session snapshot:

```sh
$RALLISH rally mcp-agent --mode status --session-id "$SID"
```

## Coexistence with the HTTP/SSE rally skill

A rally session created via `rally mcp-agent --mode create` can be joined by a
participant using `rallish rally join` (HTTP/SSE), and vice versa. The MCP and
HTTP surfaces share the same in-memory rally state.
