---
name: autonomous-cycle
description: >
  Vendor-agnostic autonomous refactor runner powered by rallish.
  Drives N cycles via the rallish cycle subsystem, gated by Preflight → Audit → Philosophy → Polish → Commit,
  with 3-cycle fresh-agent reset and graceful halt on violations.
  Triggers: "autonomous cycle", "자율 사이클", "cycle start", "nightly run", "오토 리팩터"
version: 0.1.0
ssl:
  scheduling:
    when_to_use:
      - "Long mechanical refactor with a well-defined, narrow goal"
      - "Goal can be verified by gate pipeline (no human judgment per cycle)"
      - "Baseline SHA + pending_files enumerable in advance"
    anti_triggers:
      - "Ambiguous scope or evolving goal — use rallish rally interactive"
      - "Work requiring per-cycle human decisions — use harnish:forki"
      - "First-time pattern with no baseline — write the pattern manually first"
  structural:
    scenes: [Preflight, CycleLoop, GateCheck, HandoffOrCommit, Reset]
    resumable: true
    branches:
      - "state file missing → Preflight initializes it"
      - "completed_cycles % 3 == 0 (and > 0) → fresh-agent reset signal"
      - "self-audit violations > 0 → halt=true, surface to user"
      - "SSH auth fails preflight → warning, not hard halt"
      - "completed_cycles >= MAX_CYCLES → graceful exit"
  logical:
    tools: [Bash, Read, Write]
    side_effects:
      reads: ["tmp/cycle-*.json", "git HEAD", "ssh git@github.com"]
      writes:
        - "tmp/cycle-*.json (updated each cycle)"
        - "git commits (one per cycle, conventional message)"
      deletes: []
---

# autonomous-cycle

Overnight autonomous refactor loop that runs inside rallish instead of a single-vendor CLI.
Multi-agent ping-pong is supported: rallish rotates adapters every 3 cycles.

## Companion files
- State schema: `tmp/cycle-<id>.json`
- Broker events: `rallish cycle watch --cycle-id <id>`

## Cycle workflow

```
┌─ Cycle Start ──────────────────────────────────────┐
│  1. Read tmp/cycle-<id>.json (resume point)        │
│  2. Preflight gate (branch, clean, goal, SSH)      │
│  3. Audit gate (make check-all)                    │
│  4. Philosophy gate (ROP / SSOT / SRP sweep)       │
│  5. Polish gate (tests, lint, no-raw-ansi, commit) │
│  6. Commit gate (conventional message, never amend)│
│  7. Update tmp/cycle-<id>.json                     │
│  8. Fresh-agent reset every 3 cycles               │
└────────────────────────────────────────────────────┘
       ↓ rate-limit / token exhaust
   write handoff notes → graceful exit
```

## Trigger flows

### A — Start a new cycle (server)
```
User: "autonomous cycle" / "자율 사이클 시작"
Agent:
  1. Ensure rallish daemon is running:  rallish doctor
  2. Create cycle:  rallish cycle new --goal "feat: refactor adapter package" --max-cycles 5
  3. If --agents set, start orchestration:  curl -X POST /cycles/<id>/orchestrate
  4. Report cycle-id and initial state to user.
```

### B — Check status (any time)
```
User: "cycle status" / "사이클 상태"
Agent:
  1. rallish cycle status --cycle-id <id>
  2. Summarise phase, completed_cycles, violations, halt_reason.
```

### C — Manual step (debug / human-in-the-loop)
```
User: "cycle next" / "다음 사이클"
Agent:
  1. rallish cycle next --cycle-id <id> --goal "feat: extract helper function"
  2. Report new state.
```

### D — Halt
```
User: "cycle halt" / "사이클 중단"
Agent:
  1. rallish cycle halt --cycle-id <id> --reason user-requested
  2. Confirm halt_reason written to state file.
```

### E — Watch events
```
User: "cycle watch" / "사이클 감시"
Agent:
  1. rallish cycle watch --cycle-id <id>
  2. Stream SSE events until halted or user interrupts.
```

## Multi-agent orchestration

When `--agents claude,kimi` is passed to `cycle new`, the broker rotates adapters every 3 cycles:

```
cycle 1-3 → claude
cycle 4-6 → kimi
cycle 7-9 → claude
...
```

Each agent receives the full `CycleState` as a `TurnRequest` payload and returns a `TurnResponse` with:
- `next_goal` (string)
- `violations_found` ([]Violation)
- `halt_requested` (bool)

Cross-repo orchestration is supported via `OrchestratorConfig.RepoURL`.

## Cross-vendor compatibility

This skill uses the `rallish` CLI, not vendor-specific APIs. It works with:
- Claude Code
- Kimi Code CLI
- Codex CLI
- Cursor (via terminal)

All agents invoke the same broker endpoints, so state is vendor-neutral.

## Halt conditions (graceful exit)

- `self-audit` violation count > 0
- SSH auth failure (warning on preflight, escalation if persistent)
- `completed_cycles >= MAX_CYCLES`
- Any gate exits non-zero
- User requests halt

Halt always writes `halted=true` + `halt_reason` to `tmp/cycle-<id>.json`.

## Anti-patterns

- ❌ Running on `main` (Preflight gate rejects).
- ❌ `git commit --amend` mid-loop (Commit gate never amends).
- ❌ `--no-verify` to push past a failing hook (Polish gate catches this).
- ❌ Running without `next_cycle_goal` set (Preflight gate rejects).
- ❌ MAX_CYCLES > 10 in one night (diminishing returns).
- ❌ Skipping Philosophy gate to push throughput (defeats the safety mechanism).
- ❌ `sleep` < 30s between cycles (rate limit risk).
