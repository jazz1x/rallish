# PRD: Rally Autoflow — `--first`, `--once`, and Skill-Side Auto-Loop

## §1 Problem Definition

The v0.2.0 rally skill expects the user to type `내 차례` on the receiver
side after every server turn — i.e. the user is the clock. In practice
this breaks autonomy: the receiver agent has no way to detect "my turn
just arrived" without a polling loop or an open SSE, and per-message
agent lifecycles can't easily hold either.

Two concrete gaps were observed during live validation of the discuss
pattern (session `rly_1779107266916_631e`):

1. **Server-side baton claim requires a phantom SSE join.** The broker
   marks the first SSE joiner as holder. Agents that only poll never
   claim the baton, so `rally done` fails with 409 "not your turn"
   forever. The current workaround (background `rally join` for ~2.5s,
   then kill) is not documented in the skill and feels like a hack.
2. **Receiver-side has no auto-progression.** After server's `rally done`,
   the receiver-side agent doesn't know baton arrived until the user
   prompts it again with `내 차례`. Sessions stall waiting for human
   triggers between every single turn.

The user's explicit goal: **"내 차례라고 안 해도 셀프로 확인해서 인지하고 핑퐁이 되어야지"**
— full autonomy after one setup trigger per side.

## §2 Decision & Rationale

**Selected:** ship two minimal CLI/broker additions plus a skill-side
auto-loop:

- **`rally new --first <name>`** — broker pre-assigns the baton at
  session-create time. No SSE join required to claim. Optional; default
  behaviour (idle until first join) preserved.
- **`rally join --once [--timeout <duration>]`** — SSE join exits after
  receiving the first baton event (exit 0) or after the timeout (exit 2).
  Optional flags; default behaviour (block indefinitely, multi-event)
  preserved.
- **Skill v0.3.0**: after one setup trigger per side, both agents enter a
  blocking auto-loop: `rally join --once` (wait my turn) → read history
  → do work → `rally done` → repeat. Loop exits on pattern-specific
  termination signals (`[agree]+[agree]`, `[review] approved` + no
  follow-up plan, `[resolved]`, or user "끝").

**Why:** the broker and CLI changes are tiny and orthogonal. They unlock
the auto-loop without touching the state machine, contract semantics,
or any existing test. The skill changes localise the autonomy logic
where it belongs — in the agent prompt.

## §3 Alternatives (Rejected)

| Alt | Pros | Cons | Verdict |
|---|---|---|---|
| A. Push notifications from broker to a client-side webhook | Truly event-driven | Requires the agent process to expose a listener; far outside the current architecture | Rejected |
| B. Polling loop in skill body (agent runs `rally status` every Ns) | No CLI changes | Each poll is a separate Bash tool call → high token cost; agent can't easily run a long polling loop within a single conversation turn | Rejected |
| C. Auto-claim the baton on every `rally done` call from a non-holder if the session is idle | Removes the need for `--first` | Conflates "claim" and "advance"; tricky 409 semantics; non-obvious to operators | Rejected |
| D. Keep the v0.2.0 manual-trigger flow but document the phantom-join workaround | Zero code | User-triggered every turn = not autonomous; the documented hack is fragile and SSE-timeout-dependent | Rejected (this is the gap we're fixing) |

## §4 Implementation Spec

### 4.1 Contract — `pkg/contract/rally.go`

```go
type NewRallyRequest struct {
    Participants []string `json:"participants"`
    Repo         string   `json:"repo,omitempty"`
    Task         string   `json:"task,omitempty"`
    FirstHolder  string   `json:"first_holder,omitempty"` // NEW — optional
}
```

No other contract changes.

### 4.2 Broker — `internal/broker/rally.go::handleRallyCreate`

After participants validation, if `req.FirstHolder != ""`:

- Validate `FirstHolder` is in `req.Participants` (400 otherwise).
- On session construction set:
  - `sess.holder = req.FirstHolder`
  - `sess.status = RallyTurnState(req.FirstHolder)`
  - `sess.turnN = 1`
- History stays empty (no prior handoff). The first `[history]` entry is
  produced when `FirstHolder` does its first `rally done`.

`participants[0] == as` auto-baton logic in `handleRallyBaton` keeps
working unchanged: if `FirstHolder` is set, session is already in
`<first>_turn` state and the late-join branch picks up the cue
immediately (existing late-join code path); if `FirstHolder` is unset
the first SSE joiner still claims the baton as today.

### 4.3 CLI — `internal/cli/rally.go`

- `rally new` gains `--first NAME` flag. Sent as `first_holder` in the
  POST body. Defaults to empty.
- `rally join` gains:
  - `--once` boolean — exit after the first baton event (exit code 0).
  - `--timeout DURATION` — Go-style duration string (`30s`, `5m`); if
    no baton within the deadline, exit code 2 with a stderr message.
    Default empty = block indefinitely.

`rally join --once --timeout 5m --as returner ...` is the documented
agent-side incantation.

### 4.4 Skill body — `internal/skills/rallish-operator/SKILL.md` v0.3.0

Trigger A (server prep):

1. Run `rally new --first server --task "[pattern:<name>] <task>" --participants server,returner`.
2. Save state. Status is `server_turn`, holder=server immediately.
3. Compose the first turn (cycle: `[plan] step 1: …`; discuss: `[opinion] …`; help: `[stuck] …`).
4. `rally done --as server --note "<above>"`.
5. Checkpoint to user: `🎾 서브 완료. 자동으로 상대 응답 대기할게.`
6. Enter the auto-loop (§4.5).

Trigger B (receiver prep):

1. `rally status` to confirm session + parse pattern.
2. Save state.
3. Enter the auto-loop (§4.5).

### 4.5 Auto-loop (both sides, identical structure)

```
forever:
    cue = bash("rally join --once --timeout 5m --as <ROLE>")  # blocks
    if exit-2 (timeout):
        tell user: "5분간 baton 없음. 계속 대기할까 혹은 끝낼까?"
        break  (or loop again on confirmation)

    parse cue → last note + last from
    if pattern-specific exit signal met:
        tell user: "[<signal>] — 랠리 종료."
        break

    compose response per pattern (cycle: result/plan/review; discuss:
        opinion/counter/question/agree; help: hint/try/resolved)
    bash("rally done --as <ROLE> --note <composed>")
    checkpoint to user (brief): "🎾 <note one-liner> 보냈어. 다음 차례 대기."
    continue
```

Exit signals (pattern-specific):

| Pattern | Exit cue |
|---|---|
| cycle | last 2 history entries both `[review] approved` AND no `[plan]` in the latest note |
| discuss | last 2 turns are both `[agree] …` (mutual convergence) |
| help | latest note is `[resolved] …` from owner |
| (any) | user types `끝` between turns (the agent honours user message during checkpoint) |

User interruption: between any `rally done` and the next `rally join
--once`, the agent yields back to user for any prompt (the checkpoint
line is the cue). User can type `끝`, redirect, or stay silent (silence
= continue).

### 4.6 Note on tracking

The skill body now also records (in conversation state):
- `LAST_HOLDER` — last value of `holder` we saw from status. Used to
  detect when baton finally arrives (LAST_HOLDER changed to me).
- `EXIT_REASON` — for end-of-rally summary.

## §5 Test Criteria

- Unit:
  - `TestRallyCreateWithFirstHolder` — request with `first_holder=server`
    yields immediate `server_turn` status; request with invalid name
    returns 400.
  - `TestRallyCreateWithoutFirstHolder` — backwards compat: still
    creates an idle session.
  - `TestRallyJoinOnce` — broker emits one baton; `join --once` exits 0
    with stdout containing the cue.
  - `TestRallyJoinOnceTimeout` — no baton emitted within 100ms timeout
    (configurable in test); CLI exits 2.
- Skill audit: SSL rubric scores stay at ≥ 95 on each layer; total
  trigger count stays manageable (no new short triggers).
- Manual smoke: a fresh discuss rally between this agent (server) and a
  second Claude Code session (returner) runs at least 4 turns without
  any user typing `내 차례` mid-session.

## §6 Guardrails

- **Backwards-compatible CLI.** Both new flags are optional. Existing
  `rally new` / `rally join` calls behave exactly as before.
- **Backwards-compatible contract.** `FirstHolder` is an `omitempty`
  field; the broker handles its absence identically to today.
- **stdlib only.** No new go.mod deps.
- **`make check` must pass.** `internal/broker/rally_test.go` and
  `internal/cli/rally_test.go` gain new cases; nothing is removed.
- **Skill SSL frontmatter integrity.** Triggers/branches/scenes updated
  to reflect the auto-loop. `tools` list unchanged.
- **No silent infinite loops in the agent.** Auto-loop must respect a
  per-iteration timeout (default 5 m) and emit a user-visible checkpoint
  every iteration so the user can intervene.

## §7 Acceptance Criteria

1. `docs/prd-rally-autoflow.md` (this PRD) merged.
2. `pkg/contract/rally.go` carries `FirstHolder string` (omitempty).
3. `internal/broker/rally.go` honours `FirstHolder` at session-create
   time; new tests pass.
4. `internal/cli/rally.go` exposes `--first` on `rally new` and
   `--once` / `--timeout` on `rally join`; new tests pass.
5. `internal/skills/rallish-operator/SKILL.md` and `.ko.md` updated to
   v0.3.0 with the auto-loop in §4.5 and updated SSL frontmatter.
6. `docs/runbook-rally-mode.md` gains an "Autoflow" section showing the
   v0.3.0 end-to-end (one setup trigger per side; no per-turn user
   triggers).
7. `docs/handbook.md` cross-links the new flags and the auto-loop.
8. CHANGELOG.md / .ko / .jp gain a `[Unreleased] / Added` entry.
9. SSL audit on updated SKILL.md returns ≥ 95 on every layer.
10. `make check` passes on the `feat/rally-autoflow` branch.

## Amendment — released as v0.2.0

The first public release of this feature shipped as part of rallish
`v0.2.0` (not the `v0.3.x` skill versions referenced in §4 above).
Skill version was aligned with the repo version at release time.

Two semantic refinements from the original spec:

1. **Default `WAIT_MODE` is `yield`**, not `block`. The blocking
   `rally join --once --timeout 5m` loop described in §4.5 burned ~5k
   agent tokens per 5-minute timeout window in live testing. The
   released auto-loop uses `rally status` polling on each user prompt
   instead, costing tens of tokens per check. `WAIT_MODE=block`
   remains available as an opt-in for known-ready sessions.

2. **Single-instance daemon protection** landed alongside the autoflow
   surface. Spawning a second `rallish daemon` against the same
   `~/.rallish/` now fails fast with a clear error instead of
   orphaning the first daemon.

All other §4–§6 spec sections shipped as documented.
