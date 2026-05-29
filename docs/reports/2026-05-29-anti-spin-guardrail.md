# Guardrail Design — Liveness / Anti-Spin (G5)

**Date:** 2026-05-29 · feeds [`docs/north-star.md`](../north-star.md) (adds pillar G5)
**Trigger:** an observed production catastrophe — a `/autonomous-cycle` run kept
alive by a **crontab auto-resume** fell into a **no-progress loop, burning tokens
without producing real work**, and the reviver **amplified** it by repeatedly
resurrecting the spinning agent.

## The exact failure mechanism

> **Resume without a progress gate amplifies spin.** G1 (resume-after-limit) kept
> the agent alive; nothing checked whether it was *doing* anything. A cron reviver
> + a spinning loop = unbounded token bleed. **G1 is unsafe without G5.**

Named in the field: *doom loop / wheel-spinning / spinning*, *empty/no-op cycles*,
and — when the agent *reports* success it didn't earn — *reward hacking / spec
gaming*.

## Core principle (via-negativa — validated, not just chosen)

**Detect "stuck"; do not define "progress."** Every robust production system
converges here: OpenHands ships *only* stuck-patterns (no progress score);
Magentic-One's runtime guard is a *stall counter*. A 220-loop empirical study found
agents self-reported "fine" during **45%** of bad loops — so **only structural
ledger facts the agent did not author are trustworthy** (gate verdicts, diffs,
file deltas, fingerprint repetition). Defined-progress is Goodhart-gameable;
detected-stuck is structural and far harder to game.

## The breaker — a pure ledger-reader `Stuck(entries) → HaltReason`

No new loop. A function over the append-only JSONL, called before each step.

| Trigger | Threshold (sourced) | Ledger signal |
|---|---|---|
| Repeated turn (same work) | ≥ 4 (OpenHands) | hash(`agent_turn.Files` + `Summary`) repeats |
| Repeated error | ≥ 3 (OpenHands) | same `gate_failed` (`Gate` + `Stderr` hash) |
| Ping-pong oscillation (A,B,A,B) | ≥ 6 (OpenHands) | 2-cycle in turn-fingerprint sequence |
| No-delta (no real work) | K cycles | no new `validation_green` AND no new/changed `Files` |
| Lifetime cost without progress | budget | tokens since last `validation_green` > cap |
| Hard backstops | — | existing `MaxCycles` / `MaxDurationMinutes` |

- **Do NOT trust `resp.Summary` self-claims as progress.** (The 45% finding.)
- **No exponential backoff on no-progress** — backing off a spin just delays the
  same spin; **halt** instead. (Matches the repo's ROP / symptom-vs-root rule;
  backoff is only for transient/rate errors.)
- **Two-tier (Magentic-One):** stall detected → *fresh-agent reset* (rallish
  already has `ResetEvery=3`) → still stuck → **halt**.

## Reviver guard — co-designed with G1 (the part that stops the bleed)

The cron didn't cause the spin; it amplified it (a 220-loop study saw an automated
responder emit **13×** more signals than it suppressed). The reviver must consult
the ledger before resurrecting:

1. **Sticky halt tombstone** — extend the existing zombie-prevention: a terminal
   `cycle_halted` entry the reviver MUST read; if present, **refuse to resume.**
2. **Revive only on recent measurable progress** — resume iff a `validation_green`
   or new committed diff exists within the last N cycles / T minutes; else stay
   halted. (Progress = ledger fact, never self-report.)
3. **Global token budget across ALL revivals** of a `cycle_id` (summed from the
   ledger, not per-process) — so 10 revivals can't each spend a fresh budget.
4. **Fresh bounded objective on resume** — never blindly re-enter the same stuck
   goal; require a new `WorkContract.Objective` (rallish has `AutoGoal`).

## Implementation seam (ledger-reader + existing primitives — not a new loop)

- `internal/cycle/ledger.go` — add a `LedgerReader`/`Stuck()` breaker beside `ReadAll`.
- `pkg/contract/harness_ledger.go` — fingerprint source (`Type`/`Gate`/`Summary`/
  `Files`); add a `validation_green`-aware delta check.
- `internal/cycle/orchestrator.go` — call the breaker before the step; emit
  `HaltedError` via existing `state.Halt()` + tombstone.
- `pkg/contract/work_contract.go` — add `MaxNoProgressCycles` + lifetime-cost bound.

## Confidence

- **High / sourced:** the failure naming; detection thresholds (OpenHands 4/3/6/3;
  Magentic-One stall/reset; LangGraph `recursion_limit`); cost-circuit-breaker norm;
  the self-report-disconnect (45%); reward-hacking gameability (SpecBench).
- **Medium / reasoned (flag):** the entire reviver-guard design (sticky-halt-
  survives-revival, revive-only-on-progress, global cross-revival budget) — the
  *primitives* exist in the wild but there is **no canonical named convention** as
  of May 2026; treat as rallish's own contribution to validate.

### Sources
OpenHands Stuck Detector (Nov 2025) · Magentic-One (arXiv 2411.04468) · 220-loop
study (dev.to/boucle2026) · LangGraph GRAPH_RECURSION_LIMIT · SpecBench (arXiv
2605.21384) / Reward-Hacking-Benchmark (2605.02964) · Cost Circuit Breaker
(fountaincity.tech) · claude-code#38263 / openclaw#30043 (auto-resume open
problems).
