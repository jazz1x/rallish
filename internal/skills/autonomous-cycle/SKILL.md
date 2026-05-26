---
name: autonomous-cycle
description: >
  Vendor-agnostic autonomous refactor runner powered by rallish.
  Drives N cycles via the rallish cycle subsystem, gated by Preflight → Audit → Philosophy → Polish → Commit,
  with 3-cycle fresh-agent reset and graceful halt on violations.
  Supports long-run mode (4–5 hours) with 20-minute watch rounds for guardrail hardening and philosophy compliance.
  Triggers: "autonomous cycle", "자율 사이클", "cycle start", "nightly run", "오토 리팩터",
            "long run", "20분 감시", "가드레일 보완", "철학 보완"
version: 0.2.0
ssl:
  scheduling:
    when_to_use:
      - "Long mechanical refactor with a well-defined, narrow goal"
      - "Goal can be verified by gate pipeline (no human judgment per cycle)"
      - "Baseline SHA + pending_files enumerable in advance"
      - "Overnight batch (4–5h) with periodic human check-ins every 20 min"
      - "Guardrail hardening: self-audit → fix → re-verify → philosophy sweep"
    anti_triggers:
      - "Ambiguous scope or evolving goal — use rallish rally interactive"
      - "Work requiring per-cycle human decisions — use harnish:forki"
      - "First-time pattern with no baseline — write the pattern manually first"
      - "MAX_CYCLES > 10 without explicit override — diminishing returns"
  structural:
    scenes: [Preflight, CycleLoop, GateCheck, GuardrailHarden, PhilosophySweep, HandoffOrCommit, Reset, WatchRound]
    resumable: true
    branches:
      - "state file missing → Preflight initializes it"
      - "completed_cycles % 3 == 0 (and > 0) → fresh-agent reset signal"
      - "self-audit violations > 0 → halt=true, surface to user"
      - "SSH auth fails preflight → warning, retry next cycle"
      - "completed_cycles >= MAX_CYCLES → graceful exit"
      - "20 min elapsed since last watch → human check-in round"
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
- Log stream: `rallish daemon` logs (SSE via `cycle watch`)

## Cycle workflow

```
┌─ Cycle Start ──────────────────────────────────────┐
│  1. Read tmp/cycle-<id>.json (resume point)        │
│  2. Preflight gate (branch, clean, goal, SSH)      │
│  3. Audit gate (make check-all)                    │
│  4. Local gates (--local-gate, if configured)      │
│  5. Philosophy gate (ROP / SSOT / SRP sweep)       │
│  6. Polish gate (tests, lint, no-raw-ansi)         │
│  7. Commit gate (conventional message, never amend)│
│  8. Update tmp/cycle-<id>.json                     │
│  9. Fresh-agent reset every 3 cycles               │
└────────────────────────────────────────────────────┘
       ↓ rate-limit / token exhaust
   write handoff notes → graceful exit
```

## Trigger flows

### A — One-shot start (recommended)
```
User: "autonomous cycle" / "자율 사이클 시작"
Agent:
  1. Ensure daemon is running:  rallish doctor
  2. One-shot start (blocks and streams events):
       rallish cycle start --goal "feat: refactor adapter package" --agents claude,kimi --local-gate "make check-all"
     - This creates the cycle, starts orchestration, and watches SSE in one command.
     - Ctrl+C detaches; the cycle continues in the background.
  3. To resume watching later:  rallish cycle watch --cycle-id <id>
  4. To check status:            rallish cycle status --cycle-id <id>
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

## Long-run mode (4–5 hours)

For overnight or extended autonomous runs:

1. **Set `--max-cycles` to 8–10** (safe default for one session).
2. **Use `--agents claude,kimi`** for multi-agent ping-pong.
3. **Use `--local-gate "<command>"`** for project-specific validation beyond the built-in Go gate.
4. **Start in a terminal multiplexer** (`tmux`, `screen`) so the daemon survives SSH disconnect.
5. **Redirect logs to a file**:
   ```bash
   rallish daemon > tmp/autonomous-$(date +%Y%m%d-%H%M).log 2>&1 &
   ```
6. **Graceful degradation**: if any gate fails, the cycle halts, writes `halt_reason`, and the daemon keeps serving other requests.

## 20-minute watch round

For long runs, perform a human check-in every 20 minutes:

```bash
# Quick health check
rallish cycle status --cycle-id <id> | jq '.completed_cycles, .halted, .last_failed_gate'

# Tail the latest events
rallish cycle watch --cycle-id <id> --since 20m
```

What to look for:
- `completed_cycles` increasing steadily
- `last_failed_gate` empty (no gate failures)
- `violations_found` not growing
- `halted` false

If any of these are off, run `rallish cycle halt` and inspect the state file before resuming.

## Guardrail hardening workflow

When the user asks for "guardrail hardening" or "가드레일 보완":

```
┌─ Guardrail Hardening ──────────────────────────────┐
│  1. /self-audit  → list current violations         │
│  2. Fix violations → code or configuration changes │
│  3. /polish      → re-run gates locally            │
│  4. /ralphi      → token budget + philosophy sweep │
│  5. Commit       → one commit per hardening round  │
│  6. Verify       → make check-all green            │
└────────────────────────────────────────────────────┘
```

In rallish terms:
- **Self-audit** → `AuditGate` (make check-all) + manual review of `violations_found`
- **Fix** → adapter turn that returns `violations_found: []` in the response
- **Polish** → `PolishGate` (tests, lint, no-raw-ansi)
- **Ralphi** → `PhilosophyGate` (ROP, SSOT, SRP, version hardcoding sweep)
- **Commit** → `CommitGate` (conventional message, never amend)

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
- ❌ MAX_CYCLES > 10 in one night without explicit override (diminishing returns, harder to review).
- ❌ Skipping Philosophy gate to push throughput (defeats the safety mechanism).
- ❌ `sleep` < 30s between cycles (rate limit risk).
- ❌ No 20-minute watch rounds on a 4–5 hour run (violations accumulate unnoticed).
- ❌ Ignoring `last_failed_gate` in SSE events (misses early warning signals).
