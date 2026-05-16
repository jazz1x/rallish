---
name: rallish-operator
description: >
  Operator playbook for running a rallish rally — live baton-passing between two
  human-launched coding-CLI sessions (claude, kimi, etc.) coordinated by the
  local rallish broker. Covers session setup, per-agent briefing, baton
  listening, hand-off, and shutdown. Also covers squash mode (headless preset
  orchestration). Read this when the user wants to start, join, or coordinate
  a multi-agent rally session in this repo.
  Triggers: "rally start", "let's rally", "start a rally", "two agents", "두 에이전트", "두 에이전트 같이", "baton pass", "baton hand-off", "multi-agent session", "pair coding session", "rallish 시작", "squash session", "headless squash"
version: 0.0.1
ssl:
  scheduling:
    anti_triggers:
      - "Adapter wiring / new runtime integration — see internal/adapter/ and DESIGN.md §11"
      - "Modifying the broker HTTP surface — see docs/prd-rally-mode.md and internal/broker/"
      - "Generic Go coding conventions — see AGENTS.md"
      - "Skill audit / SSL inspection — use galmuri:audit"
  structural:
    scenes: [Setup, Brief, Listen, HandOff, Status, Shutdown]
    resumable: true
    branches:
      - "fresh repo → run `make build` first, then `rallish doctor`"
      - "daemon not running → `rallish start` auto-spawns it; or `rallish daemon &` explicitly"
      - "headless preset (solo-ralph, pair-review) → use `rallish squash --preset <name>`"
      - "interactive 2-CLI → use `rallish rally new/join/done/status`"
      - "session interrupted (SSE drop) → re-run `rallish rally join` with same --as name; broker replays last baton"
      - "wrong participant POSTs done → 409 surfaced as exit 1 with stderr message"
  logical:
    tools: [Bash, Read]
    side_effects:
      reads: ["~/.rallish/socket", "~/.rallish/port", "~/.rallish/sessions/<id>/log.jsonl"]
      writes:
        - "~/.rallish/rallish.sock (mode 0600, owned by daemon)"
        - "~/.rallish/sessions/<id>/log.jsonl (per-turn req/resp)"
        - "~/.rallish/presets/*.yaml (if user adds a custom preset)"
      deletes:
        - "~/.rallish/{rallish.sock, socket, port} on daemon SIGTERM"
      network: []
    idempotent: true
    rollback: null
---

# rallish-operator — Live Rally Playbook

This skill briefs an agent (you, or a human) on running a **rally** session
between two live coding-CLI instances using the rallish broker in this repo.

## What rallish is

A local broker process that owns "whose turn is it" for multi-agent work.
Two modes:

- **squash** — headless. `rallish` spawns adapter subprocesses (`claude -p`,
  `kimi -p`) and runs a preset (`solo-ralph`, `pair-review`) end-to-end with
  no human in the loop.
- **rally** — interactive. Two humans (or human+agent pairs) launch their own
  CLI sessions; rallish only carries the baton between them via SSE.

Squash is "auto-pilot." Rally is "tennis between two live players, with
rallish keeping score."

## Pre-flight

```bash
make build                   # produce ./dist/rallish
./dist/rallish doctor        # verify adapter binaries + daemon state
```

`doctor` reports either `daemon not running` (next command auto-spawns it) or
`daemon reachable via unix socket path=~/.rallish/rallish.sock perm=-rw-------`.

## Squash mode (headless)

```bash
rallish squash --preset solo-ralph  --task "fix the flaky test in foo/bar"
rallish squash --preset pair-review --task "refactor session store"
```

The broker spawns the configured adapters and drives them to budget
exhaustion or `exit_when` match. Per-turn payloads land in
`~/.rallish/sessions/<id>/log.jsonl`. No further input needed.

## Rally mode (interactive)

### 1. Create session

```bash
SID=$(rallish rally new --participants alice,bob --task "OAuth2 PKCE")
echo $SID                    # rly_1747382400000_a3f9
```

Names must match `^[a-zA-Z0-9_-]{1,16}$`. Two or more participants required.

### 2. Each participant joins their own terminal

```bash
# Terminal A
rallish rally join --session-id $SID --as alice

# Terminal B
rallish rally join --session-id $SID --as bob
```

The first joiner gets the first baton automatically. Joining is blocking —
the process holds SSE open and prints a cue when it's that participant's
turn:

```
🏓 your turn (turn 1, from (start)): (no note)
   → work in your CLI (e.g. claude). When done, in any terminal:
   →   rallish rally done --session-id rly_... --as alice --note "<summary>"
```

### 3. Brief the live agent

The live coding CLI (claude/kimi/cursor) does NOT see the rally signal
automatically. Tell it:

> You are participant `<name>` in rally session `<SID>`. When the join
> terminal prints `🏓 your turn`, do the work for this turn. When finished,
> stop and print: `RALLY:DONE — <one-line summary>`. Do not continue past
> that line — wait for the next turn.

A typical system-prompt block in the operator's first message to each agent:

```
You're the PLANNER in rally <SID> with REVIEWER.
- Output a concrete plan with file paths and diffs.
- When complete, emit "RALLY:DONE — <summary>" and stop.
- Conventions: small diffs, conventional commits, see AGENTS.md.
```

### 4. Pass the baton

When the live agent finishes its turn, the operator runs:

```bash
rallish rally done --session-id $SID --as alice --note "plan v1: 3 endpoints"
```

Optional `--handoff-to bob` overrides default round-robin order. Output:
`ok — baton passed to bob (turn 2)`. Bob's join terminal immediately prints
its cue with alice's note as context.

### 5. Status

```bash
rallish rally status --session-id $SID
```

Shows current holder, turn count, participant last-seen heartbeats, and
handoff history.

### 6. Shutdown

```bash
kill -TERM $(pgrep -f "rallish daemon")
```

Daemon broadcasts `data: {"closed":true}` to active SSE streams, transitions
session to `interrupted`, removes `~/.rallish/{rallish.sock, socket, port}`
within ~1s.

## Conventions to teach the agent

- **Don't loop forever.** Emit `RALLY:DONE` and stop. The operator (or you,
  if shell access is granted) runs `rally done`.
- **Read the previous note** before working. It's the previous participant's
  summary of what they just finished.
- **First turn's note is `(no note)`** — start from the task description in
  `rallish rally status`.
- **On 409** ("not your turn"): check `rally status` to see who actually
  holds the baton.
- **On disconnect** (SSE drops): re-run `rallish rally join --as <name>`.
  The broker replays the current baton if it's your turn.

## Anti-patterns

| Don't | Why |
|---|---|
| Run `rally done` from a non-holder | 409; broker rejects (and logs it) |
| Skip the `--note` flag for non-trivial turns | Next holder has no context |
| Keep working past `RALLY:DONE` | Defeats turn boundary; broker can't help |
| Edit `~/.rallish/sessions/<id>/log.jsonl` manually | It's append-only audit; tampering breaks resume |

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `daemon not running` after `rally new` | First-run; broker auto-spawns next command | retry; `rallish doctor` confirms |
| `🏓 your turn` never arrives | Other participant hasn't joined OR you're not the holder | `rally status` to see holder |
| stderr `Error: not your turn (holder: bob)` | You POSTed done as wrong participant | retry as the actual holder |
| Socket file leaked after crash | Daemon killed -9 instead of -TERM | `rm -f ~/.rallish/{rallish.sock,socket,port}` and re-launch |

## Reference

- PRD: `docs/prd-rally-mode.md`
- Runbook (verification walkthrough): `docs/runbook-rally-mode.md`
- Code: `internal/broker/rally.go`, `internal/cli/rally.go`
- Project conventions: `AGENTS.md`
- Architecture: `DESIGN.md`
