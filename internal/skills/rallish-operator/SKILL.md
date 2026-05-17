---
name: rallish-operator
description: >
  Agent-driven tennis-style rally playbook. The agent runs all rallish CLI
  commands; the user only types three natural-language triggers. Covers server
  prep, returner prep, serving the first turn, picking up subsequent turns,
  and clean shutdown. Also covers squash mode (headless preset orchestration).
  Supports three behavioural patterns layered on top of the baton: cycle (plan/execute/review), discuss (multi-perspective), and help (stuck-help).
  Triggers: "랠리보낼 준비해", "let's serve", "serve prep", "rally prep — serving", "서브 준비", "랠리받을 준비해", "let's return", "returner prep", "rally prep — returning", "리턴 준비", "시작", "serve!", "go", "start rally", "내 차례", "내 차례 됐어?", "is it my turn", "ready", "ready to return", "끝", "match over", "stop rally", "랠리 끝", "cycle", "plan-execute-review", "사이클로 가자", "discuss", "discussion rally", "논의 랠리", "여러 시선으로", "stuck rally", "help me out", "막혔어 도와줘", "한 번만 봐줘"
version: 0.2.0
ssl:
  scheduling:
    anti_triggers:
      - "Adapter wiring / new runtime integration — see internal/adapter/ and DESIGN.md §11"
      - "Modifying the broker HTTP surface — see docs/prd-rally-mode.md and internal/broker/"
      - "Generic Go coding conventions — see AGENTS.md"
      - "Skill audit / SSL inspection — use galmuri:audit"
      - "Short triggers '시작' / 'go' / '끝' / '내 차례' are STATE-GATED. Match only when conversation already holds ROLE and SID (set by a prior 'serve prep' or 'returner prep' trigger). Bare 'go' / '시작' / '끝' in unrelated context must be ignored — treat as normal language, not a rally signal."
  structural:
    scenes: [ServerPrep, PatternSelect, ReceiverPrep, Serve, Return, Continue, MatchOver]
    resumable: true
    branches:
      - "ServerPrep: daemon not running → run `rallish daemon &` in background before `rally new`"
      - "ServerPrep: `rally new` stdout parsed for rly_... session id"
      - "ReceiverPrep: SID extracted from user message via rly_... pattern"
      - "ReceiverPrep: 404 on status check → tell user session not found, stop"
      - "Serve (first turn): user's 시작 message may already contain task description — use it; otherwise ask"
      - "Return/Continue: holder mismatch → report actual holder, do not proceed"
      - "409 from rally done: rare race; re-run rally status, report actual holder"
      - "SSE not used — agent polls with rally status on user trigger (per-message lifecycle)"
      - "crash recovery (daemon -9, stale socket files) → manual `rm -f ~/.rallish/{rallish.sock,socket,port}` then `rallish daemon &`"
      - "--handoff-to: supported; pass to rally done when user specifies a target participant"
      - "headless squash fallback: `rallish squash --preset solo-ralph --task \"...\"` when no second terminal available"
      - "pattern cue absent in ServerPrep message → ask user 'cycle / discuss / help / freeform'; default freeform after timeout"
      - "pattern cycle → planner side emits [plan] notes; executor side emits [result] notes; planner reviews with [review] on alternate turns"
      - "pattern discuss → both sides peer; convergence detected by mutual [agree] within 2 turns OR user '끝'"
      - "pattern help → owner stays driving; helper provides at most ~3 [hint] turns; owner's [resolved] ends session"
      - "mid-rally switch → [switch-pattern:<name>] proposed, acked next turn by [switch-ack:<name>]"
  logical:
    tools: [Bash, Read]
    side_effects:
      reads: ["~/.rallish/socket", "~/.rallish/port", "~/.rallish/sessions/<id>/log.jsonl"]
      writes:
        - "~/.rallish/rallish.sock (mode 0600, owned by daemon)"
        - "~/.rallish/sessions/<id>/log.jsonl (per-turn req/resp)"
        - "~/.rallish/presets/*.yaml (if user adds a custom preset)"
      deletes:
        - "~/.rallish/{rallish.sock, socket, port} — by daemon on SIGTERM; manually via `rm -f` for crash recovery when daemon was killed -9 and left the files stale"
      network: []
    idempotent: true
    rollback: null
---

# rallish-operator — Tennis Rally Playbook

This skill drives a live tennis-style rally between two coding-CLI sessions
through the rallish broker. The user types three things; the agent does the
rest.

## Bootstrap (when the rallish binary is missing)

The skill bundles a platform-detecting installer. If `command -v rallish`
fails, run the bundled script:

```sh
sh ~/.claude/skills/rallish-operator/scripts/install-binary.sh
```

That fetches the latest GitHub Release binary for the current OS/arch
and installs it to `/usr/local/bin` (or `~/.local/bin` if unwritable).
After it succeeds, re-run the trigger.

## Resolve the `rallish` binary first
Before any rally command, pick the runnable path:

1. Try `command -v rallish`. If found, use bare `rallish`.
2. Else look at `$PWD/dist/rallish` (after a fresh `make build`). If present, use that absolute path for every subsequent CLI call.
3. Else build it: `make build` in the repo root, then use `$PWD/dist/rallish`.

Refer to the chosen path as `$RALLISH` for the rest of this skill.

## Conversation state to maintain
- SID: rally session id (from `rally new` or user message)
- ROLE: "server" or "returner"
- PHASE: prep / serving / returning / done
- RALLISH: resolved binary path (see above)
- PATTERN: cycle | discuss | help | freeform (default; set by Trigger A or by mid-rally switch)

## Trigger A — "랠리보낼 준비해" (or English equivalents)
The agent on this side becomes the server.

1. Run `rallish doctor` to confirm broker reachable. If daemon not running,
   run `rallish daemon &` in the background.
2. Run `SID=$(rallish rally new --participants server,returner --task "TBD")`.
   Parse SID from stdout.
3. Save state: SID, ROLE=server, PHASE=prep.
3a. Pattern selection. Scan the user's original "랠리보낼 준비해" message for a pattern cue (see "Rally Patterns" section below). If a cue is present, set PATTERN accordingly. Otherwise ask once: "패턴 선택 — cycle (계획/실행/검토), discuss (다관점 논의), help (막힐 때 짧은 조언), freeform (자유)?" with a 1-turn timeout to default freeform. Encode the chosen pattern as a `[pattern:<name>]` prefix in `rally new --task`, e.g. `--task "[pattern:cycle] OAuth2 PKCE 도입"`. The receiver side parses this prefix from `rally status`.
4. Tell user:
   > Server 준비 완료. Session ID: <SID>.
   > 다른 터미널에서 "랠리받을 준비해 <SID>" 라고 말해줘.
   > 받는 쪽 준비되면 여기에 "시작" 이라고 해.

## Trigger B — "랠리받을 준비해 <SID>" (or English equivalents)
The agent on this side becomes the returner.

1. Extract SID from the user message (any rly_... pattern).
2. Run `rallish rally status --session-id $SID` to confirm session exists.
   If not found (404), tell user "그 ID로 세션이 없어. 서버 쪽에서 다시 만들어달라고 해줘." and stop.
3. Save state: SID, ROLE=returner, PHASE=prep.
3a. Detect pattern. From `rally status` output, parse the leading `[pattern:<name>]` token off the `task` field. Set local PATTERN = that name (or `freeform` if absent). Mirror the role framing in §Rally Patterns below.
4. Tell user:
   > Returner 준비 완료. 서버가 서브할 때까지 대기 중.
   > 서버가 넘겼다고 알려주면 그냥 "내 차례" 라고 말해.

## Trigger C — "시작" (server side, after prep)
The agent serves the first turn.

1. Verify ROLE == server and PHASE == prep (else: tell user the wrong-side message).
2. Run `rallish rally status --session-id $SID`. Confirm holder == "server".
3. Ask user: "서브할 작업 뭐야?" — unless the user's "시작" message already includes a task description (then use that).
4. Do the work — read files, write code, run commands, whatever the task requires.
5. Run `rallish rally done --session-id $SID --as server --note "<one-line summary of what I just did>"`.
6. Tell user:
   > 🎾 서브 완료. Returner한테 넘겼어.
   > 상대 터미널에서 "내 차례" 라고 말하면 받을 거야.

## Trigger D — "내 차례" (any input after prep, on receiver side)
The agent picks up the baton if it's its turn.

1. Run `rallish rally status --session-id $SID`.
2. If holder != my ROLE: tell user "아직 내 차례 아니야. 현재 홀더: <holder>." and stop.
3. If holder == my ROLE: read the most recent history entry's note.
4. Tell user "🎾 상대가 넘긴 메모: \"<note>\". 이대로 진행할까?" (give user a chance to redirect).
5. Wait for user OK or revised instruction. Then do the work.
6. Run `rallish rally done --session-id $SID --as <my ROLE> --note "<summary>"`.
7. Tell user:
   > 🎾 리턴 완료. 다시 상대 차례.
   > 상대가 넘기면 또 "내 차례" 라고 말해.

## Trigger E — "끝" / "match over"
Clean shutdown.

1. Tell user: "랠리 종료. 데몬은 살아있어 — 다음 세션도 같은 데몬 씀.
   완전히 끄려면 `kill -TERM $(pgrep -f 'rallish daemon')` 직접 실행."
2. Forget state.

## Rally Patterns

The skill supports three behavioural patterns on top of the rally
primitive. Pattern is selected at server prep (Trigger A) and mirrored
by the returner from `rally status`. All three use the same `rally
new/join/done/status` commands — only the **role framing** and **note
prefixes** differ.

### Pattern: cycle (plan / execute / review)

For delegating a task across two coding-CLI sessions in alternating
slices.

- ROLE framing: server = `planner`, returner = `executor`.
- Note conventions:
  - planner → `[plan] step N: <one-line directive>`
  - executor → `[result] diff: <summary>, tests: <pass|fail>`
  - planner (every other turn) → `[review] approved` or `[review] change request: <feedback>`
- Completion: planner emits `[review] approved` once the slice list is
  exhausted, or user says `끝`.

Example exchange:
1. planner — `[plan] step 1: add OAuth2 PKCE client config`
2. executor — `[result] diff: cmd/auth/oauth.go +42 / tests: pass`
3. planner — `[review] approved. [plan] step 2: wire flag in CLI`
4. executor — `[result] diff: internal/cli/auth.go +28 / tests: pass`
5. planner — `[review] approved. all slices done.`

### Pattern: discuss (multi-perspective)

For design decisions or technical debates where two viewpoints must
converge.

- ROLE framing: both sides are `peer` (no hierarchy).
- Note conventions:
  - `[opinion] <stance + one rationale>`
  - `[question] <what you want the other side to defend>`
  - `[counter] <objection + alternative>`
  - `[agree] <concession + restated agreed point>`
- Completion: both sides emit `[agree]` within the last two turns, or
  user says `끝`.

Example exchange:
1. peer1 — `[opinion] migrate to Postgres; SQLite locks under write contention`
2. peer2 — `[counter] WAL mode handles it; switching costs us 2 weeks`
3. peer1 — `[question] do you have a target wQPS where WAL still holds?`
4. peer2 — `[opinion] our load peaks ~50 wQPS; WAL fine to 1000+`
5. peer1 — `[agree] stay on SQLite + WAL; revisit if wQPS > 500`
6. peer2 — `[agree] same`

### Pattern: help (stuck-help)

For short, asymmetric exchanges when the owner is blocked and wants
one or two rounds of input.

- ROLE framing: server = `owner` (keeps driving the task), returner = `helper`.
- Note conventions:
  - owner → `[stuck] symptom: <error or behaviour>, tried: <X, Y>`
  - helper → `[hint] try <Z>, or check <W>` (helper does NOT do the work)
  - owner → `[try] applied <Z>, result: <new state>`
  - owner → `[resolved] <root cause + fix>` (ends the rally)
- Expected length: 2–6 turns. Helper should refuse to take more than
  three consecutive `[hint]` turns without an `[try]` from the owner —
  if so, helper emits `[suggest:share-context]` asking the owner to
  paste the relevant code / log.
- Completion: owner emits `[resolved]`, or user says `끝`.

Example exchange:
1. owner — `[stuck] SSE writes block after 3 turns; tried setting WriteTimeout`
2. helper — `[hint] check whether http.ResponseController is shadowed by a parent context cancellation`
3. owner — `[try] added context check; reproduced still`
4. helper — `[hint] flush before each write; goroutine may be GCed`
5. owner — `[resolved] yes — added rc.Flush() after each event; root cause was buffered writer`

### Mid-rally pattern switch

Either side may propose a switch:

- proposer → `[switch-pattern:<name>] reason: <why>`
- next-turn receiver → `[switch-ack:<name>]`

Both sides then frame subsequent turns per the new pattern. The skill
does not enforce switches; it is a coordination convention.

### Freeform (default)

If no pattern cue was detected at ServerPrep, PATTERN = `freeform`.
The agent uses plain notes without prefixes. This preserves the
original v0.1.x rally behaviour for users who don't want patterns.

## Error paths (handle silently when possible)
- 409 "not your turn": rare, but if it happens, run rally status and report the
  actual holder to the user.
- daemon connection refused: run `rallish doctor`; if daemon not up, `rallish
  daemon &` and retry.
- SSE not used in this flow — the agent polls with `rally status` instead
  (avoids long-running background processes).

## Why polling, not SSE?
The agent's lifecycle is per-message, not persistent. Background SSE would
require process supervision that goes beyond what this skill should impose.
`rally status` is cheap (one HTTP GET) and the user-driven turn cadence is
already explicit, so polling on "내 차례" is the right level of automation.

## Reference
- PRD: docs/prd-rally-mode.md
- Runbook: docs/runbook-rally-mode.md
- Code: internal/broker/rally.go, internal/cli/rally.go
- Project conventions: AGENTS.md
