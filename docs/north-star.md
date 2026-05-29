# Rallish — Goals & North Star

**Status:** v7 (graph-unified, validated) · 2026-05-29
**Grounding:** distilled from a multi-round validation arc — direction decision,
three-lens foundations (SWE philosophy + decision methods + 2026 conventions),
anti-spin guardrail, guardrails catalog, and graph-model validation (incl. a
4-judge ratification panel + adversarial stress-test). The per-session report
artifacts were pruned as process noise; their conclusions are folded into this doc.
**Maturity legend:** ✅ built · ◑ partial · ○ planned (little/no code yet).
Pillars are tagged honestly; most objectives are ○ — this is a direction, not a claim of done.

## North Star

> The vendor-neutral, standards-conformant **repo-local work harness** that makes
> *any* agent runtime **safe, resumable, verifiable, and auditable** for long
> autonomous repository work — **without being the loop**.

## The moat (durable core — read this first)

The loop, the scheduler, and cloud durability are commoditized; even verification
**gates** are being absorbed natively (GitHub Copilot validation gates, 2026-03;
MS RAMPART, 2026-05) — but **platform-bound**. What a single vendor will *not* make
cross-vendor is the durable core:

> **vendor-neutrality + portable tamper-evident audit + un-gameable repo-local gates.**

Everything else (resume, anti-spin, action-gate) is **table-stakes** that keeps the
harness usable, not the differentiator. **Erosion trip-wire:** if a *cross-vendor,
neutral* gate/audit standard ships, re-evaluate this whole document.

## Essence (본질 — irreducible)

A verifiable, resumable unit of bounded work + a tamper-evident record of it, scoped
to one repository. Runtime-neutral.

> **Honesty note on "not the loop":** rallish *does* ship a `MultiAgentOrchestrator`
> loop today (`cycle start`). That is a **reference driver**, not the product — the
> product is the harness layer (gates / state / audit / breaker) any runtime can be
> driven through. A pure harness can only **halt**; it cannot *prevent* a cron/driver
> from reviving a run. So cross-revival guarantees (e.g. a global token ceiling)
> require a **cooperating driver** — stated as a dependency, not a harness power.

## Unifying model — the Work Graph (LangGraph execution × GraphQL query)

Everything the harness does is **one typed graph and operations on it** — two
well-known shapes, two layers, one graph:

- **Execution layer (LangGraph-style):** the cycle/orchestrator is a **stateful,
  cyclic control graph** (nodes = phases / gates / agent turns; cyclic edges over
  persisted state). Mappings are **supersets, not equivalences**: checkpoint
  (`StateFileSync` ≈ durability mode `sync`; resume key = `state.ID`) gives G1
  resume but at **whole-cycle granularity** — a mid-cycle side-effect *replays* on
  resume; LangGraph's `recursion_limit` is a blind step-counter (≈ G5's hard cost
  ceiling), whereas **G5 `Stuck()` is the *diagnostic* breaker LangGraph lacks**.
  The `MultiAgentOrchestrator` is the graph **executor** — a *reference driver*,
  delegated. "Not a loop": **rallish owns the *guarded graph*, not the *traversal*.**
- **Data/query layer (GraphQL-style *analogy*):** the trace **is** a typed,
  **queryable provenance graph** — conform to **W3C PROV-AGENT** vocabulary
  (OpenLineage frames this as a graph-query). **Verifiability is a separate,
  orthogonal layer** — a CT-style **Merkle log (RFC 9162)**; the linear `prev_hash`
  chain (IETF AAT fields) is step 1 → evolve toward a Merkle tree. Querying is for
  the **audit/consumer** layer; the A2A agent-card (G3) ≈ schema introspection.

These meet: **the execution graph's trace is the queryable data graph.**

**The universal operation — *match by graph*:**

> every guardrail = a **subgraph-pattern match** against the live Work Graph;
> **a halt = a bad subgraph matched.**

- **G5 stuck** = match a **cycle/repeat** (ping-pong = 2-cycle; no-progress =
  frontier stops growing).
- **G6 action-gate** = match a **dangerous-action pattern on the *pending* edge**
  before it is added.
- **G2 gate** = match a **violation pattern** in the diff subgraph.
- **G1 resume** = **restore the frontier** (checkpoint) and continue.

*Implementation: these are **cheap named O(window) predicates** (fingerprint
equality, bounded cycle, regex-on-one-edge, key restore) — **never** general
subgraph isomorphism (NP-hard, and a graph engine is itself a new attack surface).
The "graph" is a **lens**; the runtime stays a typed append-only log + a handful
of matchers; graph-query is reserved for the G4 audit/consumer layer.*

**Payoff (honest):** frontier-growth-vs-cycle is a **harder-to-game *stuck*
signal** than self-report — but it is **not** un-gameable (an agent can churn new
nodes). **The only un-gameable signal is the verifier-produced green gate (G2),
which the worker cannot write.** Precedent (not novelty): git = Merkle DAG ·
W3C PROV / PROV-AGENT (≈ G4) · CodeQL & PIDS (security-as-graph-query) · LangGraph
(stateful agent graph). Graphs are **vendor-neutral** (→ moat) and recordable (→ G4).

## Strategic Goals — six pillars (equal in scope; **sequenced by risk, not rank**)

⚑ = a failure **this user has actually hit**.

### G1 — Safety & Resumability *(table-stakes)*
Survive session/rate/usage/context-limit death; resume from saved state.
- ✅ resume substrate: `StateFileSync` atomic write + `.bak` recovery (verify, don't rebuild).
- ○ a bounded, non-watching `cycle run --once` (exit code = halt reason) as the documented reference driver.
- ○ detect limit + reset-time and restart (needs a cooperating driver; harness halts only).
- *Coupling:* resume MUST be progress-gated — **G1 without G5 amplifies spin** ⚑.

### G2 — Verification Rigor *(the differentiator — and the part 2026 is absorbing fastest, so it must be vendor-neutral + un-gameable)*
- ◑ a gate pipeline exists (preflight / audit / philosophy / polish / commit; `--no-verify` & main-branch bans).
- ○ **parse-don't-validate the agent handshake** (typed `next_goal`; unparseable ⇒ halt). *Today `applyResponse` trusts `resp.Summary` — contradicts our own "trust structural facts" rule; this fixes it.*
- ○ **tamper-resistant gates** (threat-model: resists *in-cycle* test/gate edits via hash-pinned definitions + verifier ≠ executor; does **not** defend against a malicious runtime).
- ○ gate self-eval (a seeded regression must be caught); ○ supply-chain pinning (lockfile + hash; pkg-exists check vs slopsquatting); ○ ingest AGENTS.md/CLAUDE.md as a convention gate.
- *Success:* seeded violations caught in CI; **anti-reward-hacking is first-class** — an agent that edits/skips tests to fake "green" is detected.

### G3 — Generality & Interop
- ◑ A2A broker shaped, but on a **legacy draft** (serves `/.well-known/agent.json` + `tasks/*`).
- ○ **A2A v1.0 conformance** (Linux-Foundation-governed): `agent-card.json`, real `protocolVersion`, PascalCase RPCs, signed card + mutual auth (F16); ○ strict typed intake (`DisallowUnknownFields`; Postel critique).
- *Success:* a stock A2A v1.0 client drives a task end-to-end (today it would 404); new runtime via one 2-method adapter (✅ adapter port already minimal).

### G4 — Audit & Compliance *(durable core)*
- ✅ append-only JSONL ledger. **Today it is NOT hash-chained** — so it is not yet "tamper-evident audit"; do not call it that until built.
- ○ hash-chain (`prev_hash`/`hash`, SHA-256/RFC 8785, genesis/close); ○ replay/determinism (reconstruct the control graph; a reader, not a runtime); ○ record sandbox posture + identity per event.
- *Two orthogonal layers:* **query** → conform to **W3C PROV-AGENT** vocabulary (+ OpenLineage framing); **verifiability** → linear `prev_hash` chain (step 1, IETF AAT fields) evolving to a **CT-style Merkle log (RFC 9162)** for consistency proofs. PROV gives the queryable graph; CT gives tamper-evidence — neither does the other's job. Aligns to EU AI Act Art. 12 (track the *shape*, not the expiring draft).

### G5 — Liveness & Anti-Spin *(table-stakes — the active fire)* ⚑
Make real progress or stop. **Pairs with G1.**
- ✅ 3-cycle fresh-agent reset; ✅ halt + zombie-prevention (state removed on halt).
- ○ **stuck-breaker** `Stuck()→Halt` — cheap O(window) predicates over the ledger (repeated-turn ≥4 · same gate_failed ≥3 · ping-pong ≥6 · no `validation_green`/no new diff in K), **not** subgraph isomorphism. **Detect "stuck", don't define "progress."** This is *the diagnostic breaker LangGraph's blind `recursion_limit` lacks*; the truly un-gameable signal remains G2's verifier gate (frontier-growth only resists self-report gaming, not churn).
- ○ **hard cost/resource ceiling distinct from stuck** (a *productive* runaway won't trip a stuck detector); bound tokens + tool-calls + wall-clock. *Cross-revival ceiling needs the cooperating driver (see Essence note).*
- ○ reviver guard: sticky halt; revive only on recent measurable progress; fresh objective on resume.

### G6 — Action-Gate & Containment *(table-stakes — the most-skipped layer; verified gap)*
- ○ **no pre-execution action-gate exists today** (gates take `(ctx, state)`, evaluate *post-hoc*).
- ○ pre-execution deny-list (`rm -rf`, `git push --force`, `DROP TABLE`, prod writes → deny/HITL, logged before exec); ○ secret containment (sensitive-dir blocklist, no broad tokens); ○ path-scoped write allowlist + diff cap.
- *Boundary:* rallish **declares policy + records the decision**; the runtime hook / sandbox **enforces** — it does not become the executor.

## Guardrail layers — *where* each guardrail lives (구현시 가드레일)

| Layer | Job | Rallish |
|---|---|---|
| **Design-time** | failures impossible by construction | parse-don't-validate, ROP, least-power, append-only single-writer |
| **Build/CI-time** | verify before integrate | G2 gates, branch+PR, `--no-verify` ban, lockfile pin, pinned `.toolchain/` ⚑ |
| **Runtime** | pre-exec gate + breakers + containment | G6 action-gate, G5 breaker + cost ceiling, sandbox |
| **Audit/Governance** | immutable record, replay, HITL, policy | G4 hash-chain + replay; graded-permission allowlist (not bypass) ⚑ |

## Cross-cutting principles

- Parse-don't-validate at every boundary; unparseable input is an *error*.
- Strict, not liberal, at interfaces (RFC 9413 / Postel critique).
- Version the public surface (`schema_version`/`protocolVersion`; Hyrum). ○ not present yet.
- Honest, capability-gated naming — not "audit" until hash-chained, not "conformant" until strict-parsed.
- ROP throughout; no silent fallback.
- Trust structural facts, not self-report (Goodhart).
- **Match by graph** — model state/work/audit as one typed Work Graph; every
  guardrail is a subgraph-pattern match over it (LangGraph execution × GraphQL query).

## Non-Goals

- ❌ Not a loop / runtime *(the shipped orchestrator is a reference driver, not core)* · ❌ not a scheduler / cron · ❌ not cloud VM durability · ❌ not vendor-locked · ❌ not aspirational governance.
- ❌ **No graph-DB / subgraph-isomorphism / GNN dependency.** The Work Graph is a *lens*; the SSOT is a typed append-only hash-chained log (`prev_hash` = the edge → already a DAG). Guardrails are cheap matchers, not graph-engine queries.
- ○ **To be enforced** (not yet): a CI import-guard test asserting the core imports no loop/scheduler **or graph-DB** package (via negativa as an invariant). *(Currently aspirational — see sequencing.)*

## Sequencing (truth-making → safety breakers → differentiator → polish)

| # | Step | Why |
|---|---|---|
| 1 | `schema_version` + honest naming | cheap; stops a Hyrum trap + false claims |
| 2 | **G5 stuck-breaker + reviver guard + hard cost ceiling** | active token-bleed fire ⚑; cheap ledger-reader; makes G1 safe |
| 3 | **G6 destructive-action deny-list + secret containment** | catastrophic-prevention; the #1 missing layer |
| 4 | G4-min hash-chain | makes "audit" *true*; ~2 fields |
| 5 | **G2 tamper-resistant gates + parse-don't-validate + self-eval + dep pin** | the differentiator + the bottleneck |
| 6 | G4+ replay/determinism | biggest convention gap; a ledger reader |
| 7 | G3 A2A v1.0 (strict, signed, mutual-auth) | polish — already shaped |
| 8 | G1 verify resume + `cycle run --once` | substrate mature (safe under G5) |
| — | build-time: pin toolchain ⚑, CI import-guard, graded-permission allowlist ⚑, AGENTS.md ingest | cross-cutting |

> **Contingency:** a near-term EU AI Act Art. 12 user pulls full-G4 forward; else G2 leads after the cheap hash-chain.

## Re-evaluation triggers (don't let this doc rot)

- A cross-vendor neutral gate/audit standard ships → moat eroded, rethink.
- A real external A2A client or a real Art. 12 obligation appears → pull G3 / full-G4 forward.
- The "no OSS occupies our quadrant" claim is **an unproven hypothesis** (closest: Open Agent Passport) — disprove it by auditing OAP before over-investing.
