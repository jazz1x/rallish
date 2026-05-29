# Foundations Validation — Three Lenses on the Rallish Goals

**Date:** 2026-05-29 · feeds the ratification of [`docs/north-star.md`](../north-star.md)
**Lenses (as requested):** (1) software-engineering philosophy, (2) classical +
modern decision methods, (3) 2026 industry conventions/trends.
**Method:** 3 parallel research+synthesis agents; all code claims spot-verified
in-repo by the orchestrator before recording here.

## Verdict in one paragraph

The **direction** — vendor-neutral, repo-local *work harness*, not a loop engine —
is **confirmed independently by all three lenses**. What the foundations
*change* is (a) the **sequencing** (Theory of Constraints + Occam + the legal
deadline put G2/G4 ahead of G3, which is already ~80% shaped), (b) a set of
**concrete correctness corrections** (parse-don't-validate the agent handshake;
strict—not liberal—A2A intake; version the public contract; bind gate
definitions into the chained ledger), and (c) three **missing objectives**
(replay/determinism, gate self-eval, knowledge-file ingestion).

## Lens 1 — Software-engineering philosophy

| Principle | Demand | Implication for rallish | Verdict |
|---|---|---|---|
| **Parse-don't-validate** (King) | parse at boundary into a type where illegal states can't exist | `orchestrator.go:229-242` try-parses `next_goal` then **silently treats raw prose as the goal** → make it a typed parse; unparseable ⇒ explicit `HaltReason` | **Challenge** |
| **Railway-Oriented** (Wlaschin) | errors-as-data; no silent fallback | auto-goal helpers discard exec errors (`out, _ := r.Run`) so a broken toolchain looks like "clean repo" → route onto `Result`/`HaltedError` | **Challenge** |
| **Postel + 2023 critique** (RFC 9413) | modern: be **strict**, fail fast; liberality entrenches bug-for-bug clients | lenient A2A part-extraction undercuts the "standards-conformant" claim → typed decode + `DisallowUnknownFields`, proper JSON-RPC errors | **Challenge** |
| **Hyrum's law** | every observable behavior of a public API becomes a contract | `pkg/contract` + ledger JSONL have **no version field** (card `version`=build version, verified a2a.go:30) → add explicit `schema_version`/`protocolVersion` | **Refine** |
| **Clean Arch / hexagonal / screaming** | deps point inward; dirs name the domain | 2-method adapter port + `contract`-only imports + `cycle/gates/broker/ledger` dirs = textbook; don't over-layer | **Confirm** |
| **Unix / Gall's law / least power** | do one thing; grow from simple; least-powerful inputs | JSONL ledger = greppable; repo-local scope right; `next_goal` free prose is **too much power** → constrain to a grammar | **Confirm + Refine** |

**Top corrections:** ① kill the `next_goal` raw-text fallback (highest leverage; on the G2 differentiator). ② strict A2A intake. ③ version the public contract + ledger envelope.

## Lens 2 — Decision methods (classical + modern)

**Overall: methods CONFIRM the thesis but CHALLENGE the sequencing — reorder, don't redirect.**

| Method | Applied | Verdict |
|---|---|---|
| First principles / essence reduction | irreducible = "verifiable, resumable bounded work + tamper-evident record"; A2A is *transport* (accident), the gate-verdict is essence | demote A2A from position 1 |
| Via negativa (Taleb) | non-goals define the product better than features | give non-goals **teeth**: a CI import-guard test (core imports no loop/scheduler pkg) |
| Stoic dichotomy of control (Epictetus) | loop length/limits/vendor = not ours; gates/state/ledger integrity = ours | strongest endorsement of the non-goals |
| Theory of Constraints (Goldratt) | THE bottleneck = *verification you can believe* (G2), not interchange (G3) | exploit the constraint: harden gates before polishing A2A |
| Wardley / Sun Tzu | loop = commodity; harness/gate/audit = product; win on neutral ground vendors won't hold | invest up the value chain; *consume* A2A as a standard |
| Socratic / Cartesian doubt | "append-only ≠ audit"; "conformant"/"audit" don't survive doubt yet | **honest naming**: don't claim "audit"/"conformant" until hash-chain/strict-parse exist |
| Goodhart + 2nd-order | opaque `[]string` gates get gamed; audit then records "green theater" | gate **definitions** must live in the hash-chained ledger; verdict produced by a path the worker can't rewrite mid-cycle |
| Chesterton's fence | know why `cycle run --once` exists before treating it as "the loop" | keep `--once` as a documented **reference driver**, not product loop |

**Premortem (inversion) — the 4 likeliest failure modes:**
1. **Runtimes commoditize safety natively** → prevented only by G3 vendor-neutrality + G4 *portable* audit (the one thing a single vendor won't make cross-vendor). *Current sequencing under-weights this.*
2. **"Audit" ships uncryptographic, fails EU AI Act Art. 12** (deadline 2026-08-02, ~9 weeks) → prevented by G4 hash-chain, currently **zero code**. Highest schedule risk.
3. **Gate theater** (gates gamed; audit faithfully records meaningless green) → prevented by G2 hardening + gate-defs-in-chain.
4. **Scope creep back into a loop** → prevented by non-goals **with a CI test**, not prose.

## Lens 3 — 2026 conventions/patterns (dated, sourced)

| Convention | Status | Rallish |
|---|---|---|
| **Replay / determinism** (AgentRR arXiv 2505.17716; deterministic-replay, 2026-04-12) | fastest-moving Apr-2026 convention | **ADOPT — the missing half of G4.** Ledger is the substrate; add replay (control-graph reconstruction). It's a *reader*, not scope creep. |
| **Eval: "harness is half the score"** (Terminal-Bench 2.0; 10-20pp from scaffolding alone) | standard | **ADOPT a thin gate self-eval** (seeded-regression test proving the gates gate); AVOID shipping a benchmark. Unevaluated harness = unfalsifiable. |
| **Knowledge in repo files** — **AGENTS.md now LF standard** (60k+ repos) + CLAUDE.md | standardizing (format split) | **ADOPT read-only ingestion** as a convention-gate source; parse-don't-validate *both* formats. |
| **`/.well-known/agent-card.json` + signed cards** (A2A v0.3+) | standard | **ADOPT** (G3): serve signed card at the live path (rallish serves old `agent.json`). |
| **Sandboxing** (OWASP Agentic ASI05; Landlock/Seatbelt) | a required control | **AVOID building**; record **sandbox posture** in the ledger for audit completeness (G4). |
| **12-factor agents / spec-as-gate** (Spec-Kit) | established | ADOPT as design audit / a gate *type*; don't build authoring flows. |

**OSS overlap (don't reinvent):** closest analog is **Open Agent Passport (OAP)** — policy interception + *signed audit records* (overlaps G2+G4) but it's a synchronous tool-call interceptor; rallish is a ledger-first, append-only **hash-chained session** harness, A2A-native, repo-local. **statewright** (tool-permission FSM), **LangGraph Deep Agents** (runtime-locked). *No single OSS occupies rallish's quadrant (vendor-neutral + repo-local + verification-gates + hash-chained replayable ledger + A2A v1.0)* — treat as a hypothesis to disprove by auditing OAP, not a proven fact.

**Net-new Apr–May 2026 context:** "harness" is now a named industry category — **DeepSeek stood up a dedicated AI-harness team (2026-05-19)**; **Qwen3.7-Max supports external harnesses** (2026-05-20); CISA/Five-Eyes "Careful Adoption of Agentic AI" (2026-05-01) reinforces the safety/audit framing.

## Synthesis — what changes in the goals

**Direction:** unchanged (confirmed ×3). **Sequencing:** reordered. **Pillars:**
gain concrete objectives. **New cross-cutting principles** adopted.

Reordered sequence (front-load cheap *truth-making* fixes; then the bottleneck;
then polish):
1. **Honesty + versioning (cheap, do first):** add `schema_version`/`protocolVersion` to contract + ledger; stop calling it "audit"/"conformant" in docs until earned.
2. **G4-min: hash-chain the ledger** (2 fields `prev_hash`/`hash`; RFC 8785) — makes "audit" honest; Art. 12 insurance.
3. **G2: the differentiator + bottleneck** — parse-don't-validate the `next_goal` handshake; ship a gate self-eval; bind gate definitions into the chain (anti-Goodhart).
4. **G4+: replay/determinism** on the ledger (the biggest gap).
5. **G3: A2A v1.0 conformance** — strict typed intake; `agent-card.json`; real `protocolVersion`; signed card. (Polish — already shaped.)
6. **G1: verify** resume-after-limit (StateFileSync already has atomic write + `.bak` recovery, verified sync.go:26-62) + document `cycle run --once` as a reference driver.
Cross-cutting: non-goals as a **CI import-guard test**; ingest AGENTS.md/CLAUDE.md as a convention-gate source; record sandbox posture in the ledger.

> Contingency: if a near-term user has a hard **EU AI Act Art. 12** requirement,
> pull full-G4 forward; otherwise G2 (the differentiator) leads after the cheap
> hash-chain. This is the one ordering choice that depends on user context.

## Confidence & honesty

- **High / verified-in-repo:** the next_goal fallback, no contract version, no hash-chain, AgentCard.Version=build version, StateFileSync maturity, LocalGates=`[]string`.
- **High / sourced:** A2A v1.0 + `.well-known/agent-card.json`; AGENTS.md LF standard; Terminal-Bench "harness is half the score"; IETF AAT + EU AI Act Art. 12 date; replay convention; DeepSeek/Qwen harness-category signals.
- **Medium / proving-a-negative:** "no OSS occupies rallish's quadrant" (disprove by auditing OAP); exact maturity of OAP/statewright (single-source list).
- **Canonical (from knowledge):** the SE principles and decision methods themselves.
