# Runbook: `rallish rally mcp-agent`

End-to-end verification of the MCP 2025-03-26 client subcommand and the
`rallish-mcp` bundled skill.

## Prerequisites

- A built binary: `make build` produces `dist/rallish`.
- `rallish bootstrap` has been run at least once so `~/.rallish/config.yaml`
  exists and the `rallish-mcp` skill is installed under `~/.claude/skills/`.

## 1. Start the daemon

```bash
rallish doctor || rallish daemon &
```

`rallish doctor` should report the daemon as reachable via the Unix socket.

## 2. Create a session

```bash
rallish rally mcp-agent --mode create \
  --participants alice,bob \
  --task "dogfood mcp client" \
  --repo "github.com/jazz1x/rallish"
```

Expected output: a `RallySession` JSON object. Extract `id` and export it:

```bash
SID=rly_...
```

## 3. First participant takes the baton

Without `--first`, the session starts `idle`; the first `join` grants the
baton immediately:

```bash
rallish rally mcp-agent --mode join --session-id "$SID" --as alice --timeout 10s
```

Expected output: `{"turn_n":1}`.

## 4. Second participant waits in a long-poll

Run in a second terminal or background:

```bash
rallish rally mcp-agent --mode join --session-id "$SID" --as bob --timeout 30s
```

This blocks until the baton is handed to `bob`.

## 5. First participant hands off

```bash
rallish rally mcp-agent --mode done --session-id "$SID" --as alice \
  --handoff-to bob --note "alice done, hand to bob"
```

Expected output: updated `RallySession` with `holder:"bob"`, `turn_n:2`, and a
new history entry. The blocked `bob join` from step 4 should now return:
`{"turn_n":2,"from":"alice","note":"alice done, hand to bob"}`.

## 6. Second participant finishes

```bash
rallish rally mcp-agent --mode done --session-id "$SID" --as bob --note "bob done"
```

Expected output: `RallySession` with `holder:"alice"`, `turn_n:3`, and a
second history entry.

## 7. Inspect the session

```bash
rallish rally mcp-agent --mode status --session-id "$SID"
```

Expected output: a `RallySession` snapshot matching the current state.

## 8. Timeout behavior

If `join` waits longer than `--timeout` without receiving the baton, it prints
`{"timeout":true}` and exits with code `2`:

```bash
rallish rally mcp-agent --mode join --session-id "$SID" --as alice --timeout 1s
# exit code 2
```

## Cleanup

Stop the daemon when done:

```bash
pkill -f "rallish daemon"
```

## What success looks like

- All `mcp-agent` commands exit `0` except the intentional timeout test.
- The daemon logs show one MCP SSE transport open/close per command:
  `msg=mcp_transport_open` and `msg=mcp_transport_close`.
- `rallish bootstrap --skip-skill` (or a full bootstrap) lists the
  `rallish-mcp` skill in step 1.
