# Runbook: Autonomous Work Harness

This runbook verifies the agent-neutral harness surface: work contracts,
cycle gates, and append-only ledger readback.

## Local Validation

Run the focused broker and cycle tests:

```bash
go test ./internal/broker ./internal/cycle ./pkg/contract
```

Run the CI-parity gate before shipping:

```bash
make check-all
```

## Ledger Readback Smoke Test

Start the broker in one terminal, then create a cycle from another terminal:

```bash
curl -s -X POST http://127.0.0.1:8765/cycles \
  -H 'Content-Type: application/json' \
  -d '{"goal":"feat: verify harness ledger","local_gates":["make check-all"]}'
```

Copy the returned `id`, then read the append-only harness ledger:

```bash
curl -s http://127.0.0.1:8765/cycles/<cycle-id>/ledger
```

Or use the CLI readback, which prints the same ledger contract as pretty JSON:

```bash
rallish cycle ledger --cycle-id <cycle-id>
```

For a compact health check, inspect status:

```bash
rallish cycle status --cycle-id <cycle-id>
```

Expected result:

- The response is JSON.
- The first entry has `"type":"cycle_created"`.
- Later step or halt actions append new entries instead of rewriting history.
- Completed adapter turns append `"type":"agent_turn"` entries.
- Adapter responses with `handoff_to` append `"type":"handoff_created"` entries.
- `cycle status` shows the ledger entry count and last event when available.
- After `POST /cycles/<cycle-id>/halt`, `GET /cycles/<cycle-id>` may return 404,
  but `GET /cycles/<cycle-id>/ledger` should still return the audit entries.

## Bounded One-Shot: `cycle run --once` (reference driver)

`cycle run --once` runs a single bounded, non-watching cycle pass and exits with
a code derived from the terminal halt reason. It is the clean invocation a cron
job, scheduler, or CI step calls — the documented **reference driver, NOT
rallish's product loop**. The harness owns the *guarded graph* (gates, state,
breakers); the traversal is delegated. A pure harness can only halt, so an
external driver decides whether to re-trigger based on the exit code.

```bash
# Advance a persisted cycle one guarded step, then exit.
rallish cycle run --once --cycle-id <cycle-id>
echo "exit: $?"
```

What one pass does, in order (all via reused public machinery — no new loop):

1. Resumes state from `tmp/cycle-<id>.json` (atomic write + `.bak` recovery).
2. Runs the same G5 anti-spin guards the orchestrator runs, against the
   append-only ledger that persists across revivals:
   - sticky-halt **reviver guard** (`LedgerSealsResume`) — a cycle already halted
     with no later `validation_green` refuses to revive;
   - **stuck-breaker** (`Stuck`);
   - **hard lifetime cost ceiling** (`--max-lifetime-turns`).
3. Runs exactly one gated cycle step (preflight → audit → local gates →
   philosophy → polish → commit), then exits. It never watches the event stream.

On any halt it persists the sealed state and appends one `cycle_halted` ledger
entry, so the next invocation's reviver guard stays sticky — this is what makes
an unattended cron-resume loop safe.

### Exit-code contract (single source of truth)

The mapping lives in one switch (`exitCodeForHalt`); cron/CI keys off these:

| Code | Meaning |
|---|---|
| 0  | clean pass (advanced), nothing to do (not advanceable / not halted), or `success` |
| 10 | `stuck` |
| 11 | `budget-exceeded` (lifetime turn ceiling) |
| 12 | `preflight-failed` |
| 13 | `gate-failure` |
| 14 | `unparseable-turn` |
| 15 | `user-requested` halt |
| 16 | `self-audit-violation` |
| 17 | `ssh-auth-failed` |
| 18 | `max-cycles-reached` (terminal-but-not-failure; distinct so a scheduler can tell "done" from "needs a new objective") |
| 19 | halt with no explicit mapping (forward-compat) |
| 1  | operational error (e.g. unreadable state) — from the root command |

A scheduler treats `0` and `18` as "done / do not re-trigger", `10`/`11`/`16`
as "sealed — needs human attention", and re-runs only after recording measurable
progress (a `validation_green`) that lifts the reviver seal.

## Ledger Failure Policy

- Broker request handlers use best-effort ledger append. If the ledger write
  fails during create, step, or halt, the handler should warn and preserve the
  safer cycle operation.
- Orchestrator turn and handoff ledger writes are fail-fast. If an adapter turn
  cannot be audited, the orchestrator should stop rather than continue a
  misleading multi-agent handoff trail.
- Readback failures should be explicit errors; an empty/missing ledger for an
  unknown cycle should return not found.
