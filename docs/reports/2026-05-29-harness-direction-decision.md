# Decision Record — Is the Rallish "Work Harness" Direction Feasible and Correct?

**Date:** 2026-05-29 · **Branch:** feat/autonomous-cycle @ 7466c43
**Method:** essence reduction (본질환원) + context derivation (맥락도출) + first
principles (제1원칙) + forced binary decision (forki), grounded in **dated,
sourced** industry research from the last ~3 months (Mar–May 2026).
**Supersedes:** the initial `2026-05-29-harness-strategy-review.md` (folded in).

## TL;DR — Verdict: **GO, conditioned.**

The direction ("be the token-efficient, vendor-neutral, **repo-local safety
harness** — gates + state + audit + resume — *not* another loop engine") is
**strongly validated** by convergent 2026 evidence. It sits on 4 of the 5
strongest current vectors. It is **not** reinventing the wheel — *for the parts
that matter*. But three conditions decide whether it stays correct:

1. **Conform** on the edges others have standardized — A2A wire (rallish is
   **2 breaking revisions stale → currently interop-broken**) and the audit-log
   shape (an IETF draft + EU AI Act deadline are converging on rallish's design).
2. **Differentiate** in the open middle — repo-local **verification gates** +
   **vendor-neutral resume-after-limit**. This is genuinely unoccupied.
3. **Resist** scope-creep back into being a loop/scheduler engine — that part is
   commoditized and the runtime gives it away.

## 1. Corrected framing (the question was mis-stated before)

"예약 사이클" is **not** a task scheduler. It is a **session-limit restart
safety mechanism**: when a long autonomous run dies on a usage/rate/context
limit, something must restart it and resume from the last good state. This
reframes the validation question to: *do today's runtimes already provide
unattended resume-after-limit?* If yes → rallish would be reinventing. If no →
it is real value.

## 2. First-principles essence reduction — the irreducible core

Strip away everything a runtime now provides for free (the loop, the scheduler,
cloud VM durability). What remains that nothing else supplies?

> **A vendor-neutral, repo-local layer that makes *any* agent runtime safe and
> resumable for long repository work, and emits a portable audit of it.**

Decomposed to base truths:

| # | Base truth (2026) | Consequence for rallish |
|---|---|---|
| T1 | LLM agents have hard ceilings (context, rate, usage). | Long work **must** checkpoint + resume. → state file + **ledger** are core, not optional. |
| T2 | Generation is cheap; **verification is the bottleneck**. | The **gate** is where durable value concentrates. → gates are the differentiator. |
| T3 | Runtimes are vendor-locked and are commoditizing the loop. | Durable value is the **vendor-neutral** layer the loop can't absorb without itself becoming neutral. → adapter neutrality is the moat substrate. |
| T4 | Interop only has value if it **conforms** to the live standard. | A stale A2A is **worse than none** (a false promise that 404s). → conform or drop the claim. |
| T5 | Audit only has value if it is **portable + verifiable**. | Align the ledger to the emerging standard; add tamper-evidence. |

The loop is **absent from the essence**. That absence is correct.

## 3. Industry reality, Mar–May 2026 (dated, sourced)

### 3a. Resume-after-limit — a GENUINE, unclosed gap
- **Claude Code:** native auto-resume-after-limit **not shipped**. Feature
  request #47276 (2026-04-13) closed as duplicate; #26775/#35744/#36320 open;
  community fills it with shell wrappers (autoclaude, tmux). "Routines" (beta,
  ~Apr 2026) are cloud scheduled runs but are **rejected at the cap until the
  window resets** — no sleep-and-resume. [routines](https://code.claude.com/docs/en/routines)
- **OpenAI Codex CLI:** `codex resume` is **manual**; **v0.132.0 (2026-05-20)
  deliberately STOPS on usage limits "instead of looping."** Native auto-resume
  request #21073 (2026-05-04) **open**. [changelog](https://developers.openai.com/codex/changelog)
- **Cloud tier (Cursor via Temporal; Google Antigravity Managed Agents;
  OpenHands event-replay):** real durability against **process death** (VM
  hibernate/restore, state persistence) — but **not** the repo-local CLI
  "hit a usage limit → sleep → resume the same run" case. [Cursor](https://cursor.com/blog/cloud-agent-lessons)
- **Verdict:** commoditization is happening one tier **up** (cloud infra), not
  at the **repo-local CLI tier** where rallish lives. Building resume-after-limit
  here is **not** reinventing the wheel. *(Confidence: high for Claude/Codex via
  primary issue+changelog; medium for Devin/Amp specifics — secondary sources.)*

### 3b. Interop — A2A won; rallish is non-conformant
- **A2A** is Linux-Foundation-hosted, **v1.0.0 (2026-03-12)**, v1.0.1
  (2026-05-28), **150+ orgs**. It is the winning agent↔agent standard;
  MCP (agent→tools) is complementary, converging. [LF, 2026-04-09](https://www.linuxfoundation.org/press/a2a-protocol-surpasses-150-organizations-lands-in-major-cloud-platforms-and-sees-enterprise-production-use-in-first-year) · [v1.0 notes](https://a2a-protocol.org/latest/whats-new-v1/)
- **Rallish is 2 breaking revs behind (verified in code):** serves
  `/.well-known/agent.json` (live: **`agent-card.json`**) and
  `tasks/send|sendSubscribe|get|cancel` (live v1.0: **`SendMessage`/`GetTask`/
  `CancelTask`/`SubscribeToTask`**); `A2APart.Type "text"|"data"` (live: `kind`).
  Files: `internal/broker/a2a.go:22,67-73`, `pkg/contract/a2a.go:48,63`. **A live
  A2A client 404s on the card and won't recognize the methods.** The interop
  "moat" is, today, a **liability**.

### 3c. Audit ledger — standardizing onto rallish's own shape
- **OTel GenAI SemConv** (CNCF) = the observability track (agent spans still
  "Development"). **IETF `draft-sharif-agent-audit-trail-00` (2026-03-29)** =
  tamper-evident track: **append-only JSONL + SHA-256 hash chaining (RFC 8785) +
  optional ECDSA**, motivated by **EU AI Act Art. 12 (full application
  2026-08-02)**. [IETF](https://datatracker.ietf.org/doc/draft-sharif-agent-audit-trail/)
- Rallish's `ledger.go` + `harness_ledger.go` is **append-only JSONL already** —
  one hash-chain field away from the emerging compliance shape. A window to
  align, not a closed door. *(The draft is individual/no-WG — emerging, not
  settled. Confidence: medium-high.)*

### 3d. Philosophy/trend — "harness > loop" is the dominant 2026 frame
- "**Agent = Model + Harness; the harness is the durable value, the loop is
  commoditized.**" Faros.ai (2026-05-22) cites LangChain going **30th → 5th on
  Terminal Bench 2.0 in Mar 2026 with no model change** — pure harness gain.
  [Faros](https://www.faros.ai/blog/harness-engineering)
- **Verification is THE bottleneck:** "validation has replaced code generation
  as the bottleneck… winners will have the strongest automated quality gates"
  (TechSAA, 2026-04-24). Spec-driven dev treats specs as **executable gates**.
- **Multi-agent consensus landed on hybrid:** one orchestrator owning context +
  ephemeral isolated sub-agents returning **compressed summaries**; token usage
  ≈ 80% of perf variance. (Cognition × Anthropic, "surprisingly aligned.")
- **Repo-local harness is a named, emerging category:** Abhishek Tiwari
  (2026-05-02): *"A repository-local harness with gates, state management, audit
  trails, and resumption capability represents durable infrastructure value —
  independent of model or loop logic."* That is **rallish's exact shape**.

## 4. Fundamentals check (clean arch / ROP / parse-don't-validate / SSOT)

| Principle | Status | Evidence |
|---|---|---|
| **Clean / hexagonal arch** — runtime behind a port | ✓ strong | `Adapter` = 2 methods (Name, Run); vendor leak isolated to claude adapter (`--max-turns=1`, `ANTHROPIC_`) |
| **ROP** — Result over throw | ✓ used | `Result[State]` on Advance/Halt/CompleteCycle/SetGoal (state.go:70-101); broker best-effort vs orchestrator fail-fast is a documented failure-domain split |
| **Parse-don't-validate** | ⚠ weak spot | orchestrator.go:229-242 try-parses `resp.Summary` for `next_goal`, then **silently falls back to treating arbitrary text as the goal** — no schema enforcement on the agent handshake |
| **SSOT / additive contracts** | ✓ mostly | `WorkContract` single projection; stable JSON. **But** A2A method names are hardcoded to a dead draft instead of tracking the spec — an SSOT drift |
| **Token economy** (T2/§3d) | ✓ edge / ✗ persistence | adapter-facing: `cycleStateSummary` + 20/20/10 caps + 4k stdout truncation (orchestrator.go:149-193). persistence: `History` appends unbounded (state.go:109), `Stderr` not truncated (only Stdout, audit.go:27), `LedgerFileSync.ReadAll` loads whole file |

## 5. The decision (forki)

**Fork is NOT "harness vs loop"** — settled (harness, by convergent evidence).
The real fork:

> **A) Build it all bespoke** (own loop + own A2A + own audit format + gates)
> **B) Conform-and-differentiate** (conform on standardized edges; differentiate
>    on the open middle; lean on the runtime where it already delivers)

**Decision: B.** Derivation:
- *Context (맥락):* 2026 rewards harness + verification + vendor-neutrality +
  compliance (EU AI Act Aug 2026); it punishes vendor-locked loop clones.
- *First principles (제1원칙):* T2–T5. Value is the gate (open) + neutral resume
  (open). Interop/audit have value only if conformant (T4/T5) → conform.
- *Essence (본질):* the loop is not in the essence; conforming the wire/audit
  preserves the essence while removing false promises.

## 6. Conditioned roadmap (ordered; each ≈ one cycle / one commit)

**Conform (stop the false promises):**
1. **A2A v1.0 conformance** — `agent.json → agent-card.json`; RPCs to
   `SendMessage/GetTask/CancelTask/SubscribeToTask`; `A2APart` `type → kind`
   (keep old as deprecated aliases). *Without this, the interop claim is false.*
2. **Ledger standard alignment** — add SHA-256 hash-chaining (RFC 8785) +
   genesis/close records to the JSONL, tracking IETF AAT; keep it deterministic.

**Differentiate (invest in the open middle):**
3. **Resume-after-limit** — detect usage/rate-limit + reset time, persist state,
   restart and resume the same run unattended (the real "예약 사이클"). Fill the
   gap Claude Code/Codex CLI leave open; lean on cloud durability where present.
4. **Harden the gates** as the product — they are the unoccupied, defensible niche.

**Token economy (cheap, high-leverage):**
5. `cycle run --once` — bounded, non-watching, JSON-out, exit-code = halt reason
   (today `cycle start` blocks on `watch`, cycle.go:180). The clean invocation a
   resume mechanism or any external trigger calls.
6. Cap `History`/`ViolationsFound` on write; truncate `Stderr` like `Stdout`;
   paginate `LedgerFileSync.ReadAll`.

**Hygiene:**
7. Tighten the agent handshake (parse-don't-validate the `next_goal` payload;
   stop the silent text fallback).
8. Decouple harness core from Claude-Code-specific skill/preset install so the
   contract+broker+ledger stand alone.

## 7. Risks & honest confidence

- **High confidence:** harness-over-loop is the genuine 2026 consensus;
  resume-after-limit unshipped in Claude/Codex CLI; A2A v1.0 status + rallish's
  staleness (latter verified in code).
- **Medium / proving-a-negative:** "repo-local verification gates are still
  bespoke/unoccupied" (absence in 2026 roundups, not proof of absence);
  Devin/Amp/OpenHands resume specifics (secondary sources).
- **Speculative (flagged):** joint MCP/A2A interop spec projected Q3 2026
  (unshipped); whether gates/state/resume get absorbed natively later — if they
  do, the moat narrows to **vendor-neutrality + audit rigor**, not the mechanism.
- **Strategic risk:** scope-creep. The gravitational pull is to rebuild the loop.
  The discipline is to stay the *narrow* layer (gates + state + audit + resume)
  and conform on everything standardized.

## 8. One-line takeaway

The direction is right, well-timed, and not a reinvented wheel — **provided**
rallish conforms its A2A wire and audit ledger to the standards that solidified
in Mar–May 2026, fills the real repo-local resume-after-limit gap, and keeps its
discipline as the *harness*, never the loop.
