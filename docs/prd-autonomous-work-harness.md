# PRD: Autonomous Work Harness

## Problem

Agent runtimes can already execute increasingly long autonomous loops. Rallish should not compete as another loop engine; it should define the repo-local harness that makes any runtime safe to use for long work: scope, gates, handoff, state, and auditability.

## Decision

Rallish's product direction is an **agent-neutral autonomous work harness**. The first public artifact is `contract.WorkContract`, a stable wire shape that projects cycle requests and active cycle state into the work contract any adapter can understand.

## Spec

- `WorkContract.objective` carries the current bounded goal.
- `repo_root`, `branch`, and `pending_files` describe where work should happen.
- `local_gates` declares project-specific verification commands.
- `max_cycles`, `max_duration_minutes`, and `auto_goal` bound autonomous behavior.
- `orchestrator` optionally describes multi-agent rotation.
- `TurnRequest.work_contract` carries the contract directly to adapters so they do not need to parse cycle JSON from `Task.Body`.
- `HarnessLedgerEntry` is the append-only audit event shape for future cycle ledgers.
- `internal/cycle.LedgerFileSync` persists ledger entries as JSONL in append order.
- Gate reports can be projected into ledger events before persistence.
- Turn-response handoffs can be projected into ledger entries without losing the
  requested next agent or touched artifact list.
- Completed agent turns can be projected into ledger entries independently from
  handoff events.
- The multi-agent orchestrator appends handoff ledger events when an adapter
  returns `handoff_to`.
- `GET /cycles/{id}/ledger` returns the append-only ledger entries for a cycle,
  including halted cycles whose mutable state file has already been removed.
- `rallish cycle ledger --cycle-id <id>` prints the same ledger contract as
  pretty JSON for humans and downstream agents.
- `rallish cycle status --cycle-id <id>` includes a compact ledger health
  summary when ledger readback is available.

## Guardrails

- Keep the contract runtime-neutral.
- Do not couple the core to any workflow engine.
- Preserve existing `CycleState` compatibility.
- Add fields additively and keep JSON names stable.
- Ledger entries are append-only audit facts, not mutable session state.
- Broker request handlers treat ledger append as best-effort: a failed audit
  write must warn but must not make create, step, or halt less safe.
- Orchestrator turn and handoff ledger writes are fatal within that orchestration
  loop, because silently dropping multi-agent audit facts would make later
  handoff review misleading.

## Test Plan

- Unit test request-to-contract and state-to-contract projection.
- Unit test orchestrator turn requests include `work_contract`.
- Unit test ledger entry constructors copy mutable slices.
- Unit test gate-report-to-ledger projection.
- Unit test turn-response-to-handoff-ledger projection.
- Unit test turn-response-to-agent-turn-ledger projection.
- Unit test orchestrator appends agent turn and handoff ledger events.
- Unit test ledger append/read behavior.
- Policy review: broker ledger writes are best-effort, orchestrator turn ledger
  writes are fail-fast.
- Broker test ledger readback while a cycle is active.
- Broker test ledger readback after halt removes mutable state.
- CLI test ledger readback preserves parseable JSON output.
- CLI test status output includes the ledger entry count and last event.
- Continue running `make check-all` for CI parity.

## Acceptance Criteria

- Public contract package exposes `WorkContract`.
- `NewCycleRequest.WorkContract()` produces a copy-safe projection.
- `CycleState.WorkContract()` produces a copy-safe projection.
- Cycle orchestrator includes `work_contract` in adapter `TurnRequest`.
- Public contract package exposes append-only `HarnessLedgerEntry`.
- Public contract package can project gate reports into ledger entries.
- Public contract package can project handoff responses into ledger entries.
- Public contract package can project agent turns into ledger entries.
- Multi-agent orchestration records agent turn and handoff events in the cycle ledger.
- Cycle package can append and read ledger entries from JSONL.
- Ledger failure policy is documented for broker and orchestrator paths.
- Broker exposes cycle ledger readback without requiring live cycle state.
- CLI exposes cycle ledger readback without transforming the contract shape.
- CLI status surfaces compact ledger health without dumping the full audit log.
- Changelogs mention the harness direction in all maintained languages.
