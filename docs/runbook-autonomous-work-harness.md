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

## Ledger Failure Policy

- Broker request handlers use best-effort ledger append. If the ledger write
  fails during create, step, or halt, the handler should warn and preserve the
  safer cycle operation.
- Orchestrator turn and handoff ledger writes are fail-fast. If an adapter turn
  cannot be audited, the orchestrator should stop rather than continue a
  misleading multi-agent handoff trail.
- Readback failures should be explicit errors; an empty/missing ledger for an
  unknown cycle should return not found.
