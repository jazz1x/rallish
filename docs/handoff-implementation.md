# rallish — Implementation Handoff

> For the engineer (or agent) who picks up these specs in Claude Code and turns them into shipped code.
> **Version:** tracks `VERSION` (0.3.0) · **Last updated:** 2026-06-24 · [한국어](./handoff-implementation.ko.md)

## BLUF

Everything you need to implement is already specified and **verified against the code**. Your job is to build, not to re-investigate. Read the four specs, pick the next item from the tier-ordered work plan below, implement it with the right model, verify it with a decorrelated Sonnet check, flip its maturity tag — and never touch the same question twice.

## The one rule: do each piece of work exactly once

**The highest efficiency is not doing the same work twice or three times.** This handoff exists so you don't re-derive what is already known. Concretely:

- **The research is done.** `feature-spec.md` already maps every feature to its wired/declared state with file:line evidence, and an adversarial Sonnet panel already cross-checked it against the code. **Trust the maturity tags and the audit tiers as the source of truth.** Do not re-audit the codebase to decide what's missing — the answer is in the specs.
- **Build from the spec, not from a fresh reading.** Each work item below points to a feature-spec ID and its acceptance criteria. Implement to those criteria. If the spec is wrong, fix the spec *once* and move on — don't keep re-litigating it.
- **Verify once, with the right reviewer, then close it.** When a Sonnet review passes and `make check-all` is green, the item is done. Flip the maturity tag (○/◑ → ✅) in the *same* change. Don't re-open it later "just to check."
- **Reuse artifacts.** The ledger, the contract types, the gate pipeline, and the `fake` adapter already exist — extend them, don't reinvent. The north-star explicitly forbids re-introducing a workflow-graph engine.
- **Avoid the loop rallish itself warns about.** rallish's own anti-spin doctrine (G5) is the working method here too: make real progress or stop. Repeating a turn, re-running a passed gate, or re-reviewing a closed item is exactly the "ping-pong / no-progress" waste the harness is built to detect.

If you find yourself reading the same file or asking the same question a second time, stop and write the answer into the spec so neither you nor the next agent pays for it again.

## Model strategy — put Sonnet where it pays (적재적소)

Match the model to the work. Sonnet is the workhorse; reserve the stronger model for genuinely hard reasoning; use a fast model for lookups. Running everything on the most expensive model is itself "doing work twice" — it wastes budget on tasks a cheaper model finishes correctly.

| Work | Model | Why |
|------|-------|-----|
| Bulk implementation (wiring, gates, CLI flags, adapters, tests, translations, mechanical refactors) | **Sonnet** | Strong enough for spec-driven coding; fast and economical for the 80 % that is well-specified. This is the default. |
| Parallel, decorrelated **verification** of a finished change (does the code match the spec? any regression?) | **Sonnet** (multiple, in parallel) | Cross-check by independent reviewers — the same adversarial-panel pattern used to verify these docs. Decorrelation > one expensive pass. |
| Ambiguous **design decisions**, security/threat-model reasoning (G6 action-gate enforcement, the PreToolUse hook, A2A auth), cross-cutting **contract changes** | **Opus** (the stronger model) | These are where a wrong call is expensive to unwind. Spend the deeper reasoning here, once, up front. |
| Trivial lookups (where is symbol X? what's this flag's default?) | **Haiku** (fast) | Answers already in the specs; don't burn a big model on a grep. |

Practical rule of thumb: **draft and build with Sonnet, decide and verify the hard edges with Opus, and run the verification panel as parallel Sonnet reviewers.** Escalate to Opus only when Sonnet is genuinely uncertain — and when you do, capture the decision in the spec so it's never re-decided.

## What's already in your hands

| Document | Use it for |
|----------|-----------|
| `docs/feature-spec.md` | The SSOT for *what exists and what to build*. Maturity tags + acceptance criteria + the open-items list (§5). |
| `docs/test-plan.md` | The tests each feature must have; coverage floors; the three highest-value test additions. |
| `docs/performance-spec.md` | Performance targets + the benchmark suite to grow; the confirmed `lastHash` O(n) risk. |
| `docs/user-manual.md` | The user-facing contract — keep it true as you change behavior. |
| `docs/north-star.md` | The *why* and the non-goals (no loop, no graph-DB). Don't drift from these. |
| `docs/reports/2026-06-23-production-readiness-gaps.md` | The tiering rationale and effort estimates behind the work plan. |
| `docs/prd-cross-check-ping-pong.md` | The full spec for the one planned feature (F22). |

## Work plan (tier-ordered; each item is "read once, build once")

Sequenced by the audit's tiers — ship usability before deepening the harness claims. Each item: feature-spec ID → acceptance criteria in that spec → suggested model.

**Tier 1 — first-run UX (a stranger succeeds):**
1. Adapter auth preflight + actionable error; `doctor` verifies auth — F4 / G-F4. *Sonnet.*
2. Ship a `fake-demo` preset (or `--fake`) for a zero-credential smoke test — F1 / G-F1. *Sonnet.*
3. `rally` daemon auto-spawn + default join timeout — F2 / G-F2 (and fix the contradictory `doctor`/`bootstrap` help strings). *Sonnet.*
4. Lead the docs with an install path that works today; confirm or drop the skills-registry one-liner — F-install. *Sonnet.*

**Tier 2 — make the harness claims true (the differentiator):**
5. G6 action-gate **enforcement**: ship the PreToolUse hook wiring + a runbook (or downgrade the README claim) — F13. **Opus for the threat-model + hook contract**, Sonnet for the wiring.
6. Wire the Merkle proofs into a real path (`gate verify` / ledger-audit) or stop tagging RFC 9162 as built — F12. *Sonnet* (library is complete; this is integration).
7. Implement log-time secret redaction in `internal/logx` (or drop the claim) — F21. **Opus to design the redaction boundary**, Sonnet to implement.
8. A2A v1.0 SSE conformance: emit named `event:` lines + populate `sessionId`, mirroring the MCP path — F16. *Sonnet.*

**Tier 3 — trust (test the real path + close distribution):**
9. One gated real-adapter integration test; raise gate-pipeline + auto-goal coverage — test-plan §6. *Sonnet.*
10. CI coverage-floor enforcement — test-plan §4/§6.3. *Sonnet.*
11. Provision the Homebrew tap — audit Tier 3. *Sonnet.*

**Performance (do alongside):**
12. Confirm and fix the `lastHash` O(n) re-scan (cache the tail hash); add the `BenchmarkStuck/*` and `BenchmarkLedgerAppend/*` scaling benchmarks — performance-spec §4.2/§7. **Opus to confirm the fix is correct under concurrency**, Sonnet to implement + benchmark.

**Feature work (after Tier 1):**
13. Cross-check ping-pong (F22) — build to the PRD's §5 test plan and §7 acceptance criteria. **Opus for the contract/intent design** (it touches the baton schema), Sonnet for adapters/tests.
14. Wire the scratchpad (F20); implement `strict_round_robin` / `last_writer_wins` or make the validator reject them (F6). *Sonnet.*

## The per-item loop (single pass, no re-work)

1. **Read** the feature-spec entry + its acceptance criteria. (Don't re-read the whole codebase — the spec cites the files you need.)
2. **Decide** any ambiguity with Opus *once*; write the decision into the spec.
3. **Build** with Sonnet to the acceptance criteria; extend existing types/ledger/gates.
4. **Test** to `test-plan.md` (and flip any "pending/expected-fail" gap test to green).
5. **Verify** with a parallel Sonnet panel: does the code match the spec? any regression? Run `make check-all`.
6. **Close**: update the maturity tag (○/◑ → ✅) and the user-manual in the *same* change (honest, capability-gated naming). Move to the next item. Do not revisit.

## Definition of done (per change)

Mirrors `test-plan.md` §8: touched behavior has unit tests; concurrency-sensitive changes have a `-race` test; coverage for touched packages stays above the floor; `make check-all` passes; contract changes get round-trip JSON tests and a `schema_version` bump if the shape changed; and any feature flipped to ✅ has its gap test green **and** its maturity tag updated in the same change.

## Dogfood it

This repo is a multi-agent harness — use it on itself. Drive the implementation with `pair-review` (planner/executor/reviewer) and run autonomous chunks through `cycle run --once` so the gate pipeline, stuck breaker, and ledger guard your own work. The adversarial-reviewer pattern (decorrelated Sonnet cross-check) is the same one that verified these specs — keep using it as your verification gate.
