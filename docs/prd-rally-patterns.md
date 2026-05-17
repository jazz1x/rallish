# PRD: Rally Patterns — Cycle / Discuss / Help

## §1 Problem Definition

The rally primitive ships a generic two-participant baton with a freeform
`--note` field. In practice, users want it to power three distinct
collaboration patterns:

1. **Cycle** — plan/execute/review rotations. A plans, B executes, A
   reviews + plans the next slice. The natural use-case for delegating
   work across two coding-CLI sessions.
2. **Discuss** — multi-perspective debate. Two peers exchange opinions /
   questions / counters on a design decision until they converge.
3. **Help-when-stuck** — short, asymmetric exchange. An owner blocked on
   a sub-problem invites a helper for one or two turns of input, then
   resumes solo work.

Without explicit guidance, an agent reading the current `rallish-operator`
skill has to invent the conversation shape from scratch each session.
That produces inconsistent note quality, ambiguous turn boundaries, and
missed cycle structure.

## §2 Decision & Rationale

**Selected:** Encode the three patterns as **behavioral conventions in
the skill body**, layered on top of the unchanged rally primitive. No
broker / CLI / contract changes.

**Why:** rally is already general enough — single-baton, two-participant,
freeform notes. The patterns differ in (a) the role framing the agent
gives each side, (b) the note format on each turn, and (c) the
completion signal. All of that is documentation + agent-prompt territory,
not protocol territory. Keeping the CLI primitive minimal means future
patterns add zero code surface.

## §3 Alternatives (Rejected)

| Alt | Pros | Cons | Verdict |
|---|---|---|---|
| A. `rally new --mode cycle\|discuss\|help` flag persisted in session state | Visible in `rally status`; both sides agree on mode | Bloats CLI / contract; schema churn when patterns evolve; agents must still infer note shape | Rejected — encode in skill body instead |
| B. One skill per pattern (`rallish-cycle`, `rallish-discuss`, `rallish-help`) | Strong separation; clear triggers | Fragmented discovery; triple maintenance burden; cross-pattern flexibility lost (cannot switch mid-rally) | Rejected — single skill with branching |
| C. Documentation-only in handbook | Lowest churn | Agent has no runtime guidance; users must re-explain patterns to each session | Rejected — skill body is where the agent reads at trigger time |

## §4 Implementation Spec

### 4.1 Pattern triggers (matched additively, never exclusively)

The skill currently activates on `랠리보낼 준비해` / `let's serve` etc.
Pattern is selected from a **second cue** in the same or next user
message. If the cue is absent, default = freeform (today's behavior).

| Pattern | Korean triggers | English triggers |
|---|---|---|
| cycle | `사이클로 가자`, `계획-실행-검토`, `계획 보내고 실행` | `cycle`, `plan-execute-review`, `plan-and-execute` |
| discuss | `논의 랠리`, `여러 시선으로`, `관점 검토 랠리` | `discuss`, `discussion rally`, `multi-perspective` |
| help | `막혔어 도와줘`, `도움 랠리`, `한 번만 봐줘` | `stuck rally`, `help me out`, `quick help` |

### 4.2 Role framing per pattern

| Pattern | server role | returner role | hierarchy |
|---|---|---|---|
| cycle | **planner** — emits a numbered task list, picks the next slice, reviews returner's last result | **executor** — implements the current slice, emits diff/summary | asymmetric (planner drives) |
| discuss | **peer1** — opens with a position | **peer2** — counters/refines | symmetric (no drive) |
| help | **owner** — describes the blocker, applies the suggestion | **helper** — asks clarifying questions, suggests fixes | asymmetric (owner owns the work, helper advises) |

### 4.3 Note conventions per pattern

Notes are still plain strings — these are **suggested prefixes** the
agent inserts so the receiving side can parse intent without parsing
prose:

| Pattern | Prefixes |
|---|---|
| cycle | `[plan] step N: …`, `[result] diff: …, tests: …`, `[review] approved \| change request: …` |
| discuss | `[opinion] …`, `[question] …`, `[counter] …`, `[agree] …` |
| help | `[stuck] symptom + what tried`, `[hint] try X, or check Y`, `[try] applied X, result Z`, `[resolved] thanks — moving on` |

### 4.4 Completion signals per pattern

| Pattern | Signal | Action |
|---|---|---|
| cycle | `[review] approved` (3rd or later turn) **or** user "끝" | Final cycle done; session ends |
| discuss | Both sides emit `[agree] …` within last 2 turns **or** user "끝" | Convergence reached |
| help | `[resolved] …` from owner **or** user "끝" | Helper can drop, owner resumes solo |

### 4.5 Pattern selection algorithm (in the agent)

When the user fires a setup trigger (`랠리보낼 준비해`):

1. Scan the same message for a pattern cue (§4.1).
2. If a cue is present → set conversation state `PATTERN = cycle | discuss | help`.
3. If absent → ask user: "패턴 골라줘 — cycle / discuss / help / freeform" (timeboxed, default freeform).
4. Encode `PATTERN` in the rally `--task` text as `[pattern:<name>] <user task>` so the returner side reads it on `rally status`.

The returner side, on `랠리받을 준비해 <SID>`:

1. Runs `rally status`, parses the `[pattern:<name>]` prefix from `task`.
2. Mirrors the same `PATTERN` in its local state.
3. Frames subsequent turns per §4.2 / §4.3 / §4.4.

### 4.6 Mid-rally pattern switch

Either side may propose a switch via a note prefixed `[switch-pattern:<name>]
reason: …`. The receiver acknowledges with `[switch-ack:<name>]` on the
next turn. Both sides update local `PATTERN`. No broker change required.

## §5 Test Criteria

- The skill audit (galmuri:audit SSL rubric) re-passes at **100/100/100**
  on the updated SKILL.md (EN + KO).
- Manual smoke for each pattern:
  - **cycle**: 3 turns, planner emits 3 slices, executor returns diff,
    planner reviews + emits next slice. Final review = approve.
  - **discuss**: 4 turns, alternating opinions, ends in `[agree]`.
  - **help**: 2 turns — `[stuck]` + `[hint]` + `[resolved]`.
- Frontmatter pattern triggers added; total trigger count stays ≤ 30
  (current is 23) to avoid trigger-bloat penalty.
- README install table + handbook unchanged (this PR is skill body +
  PRD/docs only).
- `make check` passes (no Go files changed).

## §6 Guardrails

- **No broker / CLI / contract changes.** All convention.
- Backward compatible: a rally session without a `[pattern:…]` prefix in
  `task` stays freeform exactly as today.
- Skill version bump: `0.1.0` → `0.2.0` (new behavior, no breaking
  change to existing trigger surface).
- Note prefixes (`[plan]`, `[result]`, …) are guidance, not enforcement.
  The CLI does not parse them; only the agent uses them as receiver
  hints.
- Existing rally tests in `internal/broker/rally_test.go` and
  `internal/cli/rally_test.go` continue to pass unchanged.
- SSL frontmatter on `internal/skills/rallish-operator/SKILL.md` must
  remain valid (scenes / branches / triggers / side_effects updated as
  needed; tools list unchanged: `[Bash, Read]`).

## §7 Acceptance Criteria

1. `docs/prd-rally-patterns.md` (this file) merged.
2. `internal/skills/rallish-operator/SKILL.md` + `.ko.md` updated:
   - frontmatter triggers expanded with pattern phrases
   - `ssl.structural.branches` lists the three pattern flows
   - body has a `## Rally Patterns` section with three subsections
   - version bumped to `0.2.0`
3. `docs/runbook-rally-mode.md` gains a §Patterns section with one
   walkthrough per pattern.
4. `docs/handbook.md` cross-links the pattern doc / runbook.
5. CHANGELOG.md / .ko / .jp gain a `[Unreleased] / Added` entry
   describing the patterns.
6. SSL audit on updated SKILL.md returns ≥ 95 on every layer (target
   100).
7. `make check` passes on the feat/rally-patterns branch.
