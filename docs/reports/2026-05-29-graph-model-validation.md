# Graph-Model Validation — grounding + adversarial stress-test

**Date:** 2026-05-29 · validates the v6 "Work Graph" model in [`docs/north-star.md`](../north-star.md) → feeds v7.
**Method:** 3 parallel agents — execution-layer grounding, data/audit-layer grounding, and an adversarial stress-test. All code claims re-checked in-repo.

## Verdict: **VALIDATE the lens, REFINE the claims.**

The graph framing is the dominant 2026 convention and well-precedented — but the
literal "subgraph match over one typed graph" over-claimed (over-engineering +
Goodhart). Four refinements make it robust and implementable. Net: keep the
unifying lens; commit in code to a **typed append-only log + ~5 cheap matchers**,
never a graph DB.

## Axis 1 — Execution layer (LangGraph): VALIDATE + 2 superset fixes
- Graph-as-control-flow is **dominant, not fading**: LangGraph 1.0 GA (Oct 2025), 1.2.0 (2026-05-11); "production standard for stateful, auditable agentic workflows." rallish's "owns the *guarded graph*, not the *traversal*" matches the 2026 **framework-inside-engine** seam. [sourced]
- **REFINE — checkpoint ≠ durable resume (superset):** "Checkpoints Are Not Durable Execution" (Diagrid, 2026-02-25). A checkpoint = snapshot + *manual* resume; auto-failure-detection / no-duplicate-execution need a cooperating driver. Adopt LangGraph vocabulary: `StateFileSync` ≈ durability mode **`sync`**; resume key = `thread_id` (= `state.ID`); **whole-cycle granularity, no "pending writes"** → a mid-cycle side-effect *replays* on resume (state the limit).
- **REFINE — recursion-limit ≠ stuck-breaker:** LangGraph `recursion_limit` (default 25 → `GraphRecursionError`) is a **blind step-counter** ("tells you nothing about *why* you loop"). That ≈ rallish's **hard cost ceiling**, NOT the semantic breaker. rallish `Stuck()` (repeat-turn / same-gate / ping-pong / no-new-diff) is **strictly more** — frame it as *the diagnostic breaker LangGraph lacks* (a differentiator). [sourced]

## Axis 2 — Data/audit layer (GraphQL/PROV/Merkle): REFINE — two orthogonal shapes, layer them
- **Provenance-as-graph is standardized:** W3C PROV, with a 2026 agent extension **PROV-AGENT** (`AIAgent`/`AgentTool`/`AIModelInvocation`, MCP-aware; arXiv 2508.02866 + Feb-2026 extension). Conform to **PROV-AGENT vocabulary**, not raw PROV. [sourced]
- **Queryable provenance is real precedent** (PROV-AGENT ships lineage queries + NL GUI; OpenLineage frames monitoring as "a graph-query problem"; arXiv 2509.13978 does runtime anomaly detection over streaming provenance — direct precedent for "stuck = structural graph property"). "GraphQL-style" is an **analogy**, not a spec; the spec to track is PROV-AGENT + OpenLineage. [sourced]
- **REFINE — "tamper-evident" needs Merkle, not a linear chain:** CT (RFC 9162) is the canonical verifiable-log shape and is a **Merkle tree** (membership + consistency proofs + split-view detection). The IETF AAT draft's *linear* SHA-256 chaining is tamper-evident to a sequential reader but lacks consistency proofs. So: linear `prev_hash`/`hash` = correct **step 1** (matches AAT fields); evolve toward a **Merkle log** for the strong claim. PROV-AGENT has **zero crypto** → PROV (queryable) and CT (verifiable) are **orthogonal layers**; want both, expect neither to do the other's job. [sourced]
- OTel GenAI = the *execution* trace (span graph); OpenLineage/PROV = the *data/provenance* trace. Maps cleanly to rallish's two layers. [sourced]

## Axis 3 — Guardrail-as-graph-match: REFINE — keep the lens, drop the graph-DB
- **Grounded:** security-as-graph-query is mature — **CodeQL** (compiles code to a queryable graph DB, runs violation-subgraph queries), **PIDS** (rule = a *subgraph of signatures* over a provenance graph; ACM CSurv 3539605). Strong analog for "G2 gate = violation pattern in a diff subgraph." [sourced]
- **ATTACK 1 — NP-hard is a vocabulary trap.** General subgraph isomorphism is NP-complete, but rallish needs **none** of it: every named matcher is a cheap special case — cycle/ping-pong = bounded fixed-pattern (linear), repeat-turn/same-gate = **fingerprint equality** (O(1)), deny-list = **regex on one pending edge** (Semgrep-cheap), resume = key lookup. Saying "subgraph match" tempts a future maintainer toward a graph DB / GNN. **MirGuard** (arXiv 2508.10639): graph+GNN created a *new* attack surface (benign-edge injection). Heavier graph machinery = more to game. [sourced]
- **ATTACK 2 — Goodhart on "frontier growth."** "progress = structural, not self-reported" is only half true: an agent that touches a new file/branch each cycle grows the frontier while doing nothing (churn = documented reward-hack). Frontier-growth resists *self-report* gaming, not *behavioral* gaming. **The only truly un-gameable signal is the verifier-produced green gate (G2), which the worker cannot write.** [sourced: synthesis.ai 2025-05; EvilGenie arXiv 2511.21654; tianpan 2026-04]
- **ATTACK 3 — over-modeling the data layer.** A flat typed append-only log **already is a DAG** (`prev_hash` = parent edge); you get the graph *for free* without a node/edge store or query engine. Least-power / greppable (the repo's own foundations) is strictly sufficient for G1/G5/G6.

## Refinements adopted into v7
1. **Graph = a lens; SSOT = one typed, append-only, hash-chained event log** (`prev_hash` = edge). **New non-goal: no graph-DB / isomorphism / GNN dependency**, enforced by a CI import-guard (mirrors the "no loop pkg" guard).
2. **Breakers/gates = ~5 cheap *named* O(window) matchers** (fingerprint equality, bounded cycle, no-new-`validation_green`-in-K, pending-edge regex, key restore) — graph-query reserved for the **G4 audit/consumer** layer only.
3. **Goodhart fix:** frontier-growth = a *harder-to-game stuck signal* (not "un-gameable progress"); the un-gameable signal is the verifier's green gate (G2).
4. **Standards:** execution mappings are **supersets** (name durability mode + whole-cycle limit; Stuck() > recursion-limit); audit = **PROV-AGENT (query) + CT-Merkle (verify)**, linear chain → Merkle, track IETF AAT fields.

## Confidence
- **High / sourced:** LangGraph dominance + the two superset critiques; PROV-AGENT/OpenLineage/CT-Merkle/OTel; CodeQL/PIDS precedent; NP-hard special-case collapse; Goodhart-on-frontier.
- **Reasoned:** the "every named matcher is a cheap special case" mapping; the leanest-implementation recommendation (grounded in the repo's least-power foundation).

### Sources
LangGraph durable-execution + GRAPH_RECURSION_LIMIT (docs.langchain.com, 2026) · "Checkpoints Are Not Durable Execution" Diagrid 2026-02-25 · PROV-AGENT arXiv 2508.02866 · streaming-provenance anomaly detection arXiv 2509.13978 · RFC 9162 (CT v2) · draft-sharif-agent-audit-trail-00 (2026-03-29) · OTel GenAI conventions (2026) · OpenLineage (2026-05) · PIDS survey ACM 3539605 · MirGuard arXiv 2508.10639 · subgraph-isomorphism NP-completeness · reward-hacking: synthesis.ai 2025-05, EvilGenie arXiv 2511.21654, tianpan 2026-04.
