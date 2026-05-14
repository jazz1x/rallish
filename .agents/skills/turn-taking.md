# Turn-Taking Agent Guidelines

> Coordination rules, budget exhaustion recovery, and scratch compaction criteria for agents operating inside a rallish session.

## Coordination Rules

1. **Role-based sequencing** — Follow the preset routing order exactly. Do not skip turns or re-order agents.
2. **Single writer** — Only one agent may write to the scratchpad per turn. Concurrent writes are serialized by the broker.
3. **Idempotency** — If a turn is retried (e.g., network timeout), the adapter must detect duplicate turn IDs and return the cached response.
4. **Graceful degradation** — If an adapter fails (non-zero exit), the broker marks the session as `error` and stops routing. The last successful turn remains in the scratchpad.
5. **Human-in-the-loop** — If a turn returns `blocked` status, the session pauses. An operator may POST a `TurnRequest` with `role: human` to unblock.

## Budget Exhaustion Recovery

When `budget.IsExhausted()` returns `true` in `handleNextTurn`:

1. **Immediate stop** — The broker returns HTTP 429 with `X-Budget-Exhausted: true`.
2. **Session state** — The session status is set to `done` (not `error`).
3. **Scratchpad preservation** — The scratchpad is flushed to disk at `~/.rallish/sessions/<id>/scratch_final.txt`.
4. **Resume capability** — A new session may be started with `--resume-from <id>` to continue with a fresh budget. The previous scratchpad is copied as the initial state.
5. **Alert** — A warning is logged: `budget exhausted: turns=N, tokens=M`.

## Scratch Compaction Criteria

`internal/scratch/scratch.go` triggers compaction when:

1. **Size threshold** — `Append()` checks if `size > maxKB * 1024` after writing.
2. **Compaction strategy** — Keep the last 50% of bytes, prepend a user-provided or auto-generated summary.
3. **Summary generation** — If no summary is provided, the adapter uses the first 200 characters of the turn response as a placeholder summary.
4. **Frequency** — Compaction runs at most once per turn to avoid I/O thrashing.
5. **Safety** — If compaction fails (disk full), the scratchpad falls back to a new empty file and logs an error.
