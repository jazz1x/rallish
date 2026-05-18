# Runbook: Rally Mode — Manual Verification

> **Prerequisites:** Go 1.25+, macOS/Linux, `~/.rallish/` directory writable, daemon running

---

## 1. Overview

Rally mode (`rallish rally`) provides live baton-passing between two or more human-launched CLI sessions. Unlike `rallish squash` — which spawns headless goroutine adapters inside the broker for automated presets (`solo-ralph`, `pair-review`) — rally mode streams a persistent SSE connection per named participant and enforces exclusive baton ownership. Only the current holder can call `done`; everyone else gets `409 Conflict`.

Key differences from squash:

| Aspect | squash | rally |
|--------|--------|-------|
| Who drives turns | Broker-spawned adapters | Human terminals |
| Session ID prefix | `ses_` | `rly_` |
| Turn passing | Automatic (routing rules) | `rally done` command |
| SSE stream | One per role, broker-managed | One per named participant |
| Interruptible | Yes (SIGTERM cancels) | Yes — status shows `interrupted` |

Routes:

```
POST /rally/sessions                      → create
GET  /rally/sessions/{id}/baton?as=<name> → join (SSE, long-lived)
POST /rally/sessions/{id}/done?as=<name>  → pass the baton
GET  /rally/sessions/{id}                 → status snapshot
```

---

## 2. Pre-flight

```bash
cd /path/to/rallish
make build
ls -la dist/rallish

# Start (or confirm) the daemon
./dist/rallish daemon &
DAEMON_PID=$!
sleep 1

./dist/rallish doctor
```

**Expected doctor output:**

```
daemon reachable via unix socket path=/Users/<you>/.rallish/rallish.sock perm=-rw-------
```

---

## 3. Two-terminal smoke walkthrough

Open two terminal windows. Both must point at the same `dist/rallish` build.

### Step 1 — Create the session (Terminal A)

```bash
SESSION=$(./dist/rallish rally new --participants alice,bob --task "ping pong demo")
echo "session: $SESSION"
```

**Expected output:**

```
session: rly_1747382400000_a3f9
```

The session starts in `idle` state; no one holds the baton yet.

---

### Step 2 — Bob joins and waits (Terminal B)

```bash
./dist/rallish rally join --session-id $SESSION --as bob
```

Terminal B blocks — bob is not participant[0], so no baton is delivered yet. Bob's SSE stream stays open.

---

### Step 3 — Alice joins (Terminal A)

In a **new tab or pane** of Terminal A (keep the `SESSION` variable):

```bash
./dist/rallish rally join --session-id $SESSION --as alice
```

Alice is `participants[0]`, so joining immediately delivers the first baton.

**Expected output in Terminal A (alice's join):**

```
🎾 your turn (turn 1, from (start)): (no note)
   -> work in your CLI (e.g. claude). When done, in any terminal:
   ->   rallish rally done --session-id rly_1747382400000_a3f9 --as alice --note "<summary>"
```

Terminal B (bob) is still blocked, waiting.

---

### Step 4 — Alice passes the baton

In Terminal A (or any third window):

```bash
./dist/rallish rally done --session-id $SESSION --as alice --note "draft v1"
```

**Expected output:**

```
ok — baton passed to bob (turn 2)
```

---

### Step 5 — Bob receives the baton (Terminal B)

Terminal B's blocked `rally join` now unblocks and prints:

```
🎾 your turn (turn 2, from alice): draft v1
   -> work in your CLI (e.g. claude). When done, in any terminal:
   ->   rallish rally done --session-id rly_1747382400000_a3f9 --as bob --note "<summary>"
```

---

### Step 6 — Bob passes back to Alice

```bash
./dist/rallish rally done --session-id $SESSION --as bob --note "review pass"
```

**Expected output:**

```
ok — baton passed to alice (turn 3)
```

Alice's still-open join stream prints the new baton cue.

---

## 4. Status command sample output

```bash
./dist/rallish rally status --session-id $SESSION
```

**After two completed rounds:**

```
Session:      rly_1747382400000_a3f9
Status:       alice_turn
Holder:       alice
Turn:         3
Task:         ping pong demo
Participants:
  - alice (last seen 2s ago)
  - bob (last seen 5s ago)
History (last 5):
  turn 2: alice -> bob  note: "draft v1"
  turn 3: bob -> alice  note: "review pass"
```

Participants that have not sent a heartbeat within 30 s are flagged `[stale]`. Participants that have never connected show `[not yet connected]`.

---

## 5. Error paths to verify

### 5.1 — Non-holder calls done (409)

While alice holds the baton:

```bash
./dist/rallish rally done --session-id $SESSION --as bob --note "oops"
```

**Expected:**

```
not your turn — participant "bob" is not the current baton holder (holder: "alice")
Error: not your turn
```

HTTP layer returns `409 Conflict`.

---

### 5.2 — Stale participant after dropping SSE

Kill Terminal B's `rally join` process (Ctrl-C). Wait 31 s or use a test helper, then:

```bash
./dist/rallish rally status --session-id $SESSION
```

**Expected:**

```
  - bob (last seen 32s ago) [stale]
```

Staleness is advisory — the broker does not auto-advance the baton. Alice may call `done --handoff-to alice` to skip bob if desired.

---

### 5.3 — Invalid participant name (400)

```bash
./dist/rallish rally new --participants "alice,bad name!"
```

**Expected:**

```
Error: create rally session failed: 400 invalid participant name "bad name!": must match ^[a-zA-Z0-9_-]{1,16}$
```

Same validation applies at `rally join` and `rally done`.

---

## 6. SIGTERM behaviour during a rally

With both alice and bob connected via `rally join`:

```bash
kill -TERM "$DAEMON_PID"
wait "$DAEMON_PID"
echo "Exit code: $?"
```

**Expected broker log:**

```
time=... level=INFO msg="session_interrupted" session_id=rly_1747382400000_a3f9
```

**Expected on each join terminal:** the SSE stream closes cleanly (clients see `session closed`).

After the broker restarts:

```bash
./dist/rallish rally status --session-id $SESSION
```

**Expected:**

```
Status:       interrupted
```

The session state is preserved in memory until the broker process exits; after a restart the session is gone (in-memory store; no disk persistence for rally sessions).

---

## 7. Regression — `make check`

```bash
make check
```

**Expected:**

- `go vet ./...` → 0 issues
- `golangci-lint run` → 0 issues
- `go test ./... -race` → all green (includes `TestRallyCreateAndStatus`, `TestRallyBatonTwoRound`, `TestRallyDoneNonHolder409`)

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| `rally join` exits immediately with "join rally failed: 403" | Participant name not in session | Check `--participants` list used at `rally new` |
| `rally done` returns 409 | Not the current holder | Check `rally status` to see current holder |
| Both join terminals stuck, no baton delivered | Neither terminal is `participants[0]` | First listed participant must join to start the session |
| `rally status` shows `[not yet connected]` for both | Session created but no one has joined | Run `rally join` in at least one terminal |
| `session closed` immediately on join | Broker sent SIGTERM mid-session | Restart daemon; session state is lost |

---

## Rally Patterns

The `rallish-operator` skill (v0.2.0) layers three behavioural patterns on top
of the baton primitive. Pattern is selected at server-prep time; no broker or
CLI changes are required — the pattern is encoded in the `--task` field and
read back by the returner from `rally status`.

### Pattern: cycle (plan / execute / review)

**When to use:** delegating a sliced task across two coding-CLI sessions.
Planner owns the roadmap; executor owns the implementation.

**Terminal A (server) — trigger:**

```
랠리보낼 준비해 — 사이클로 가자
```

The agent detects `사이클로 가자`, sets `PATTERN=cycle`, prompts:

```
서브할 작업 뭐야?
> OAuth2 PKCE 도입
```

Then runs:

```bash
SID=$(rallish rally new --participants server,returner \
      --task "[pattern:cycle] OAuth2 PKCE 도입")
```

Prints:

```
🎾 Server 준비 완료. Session ID: rly_1747382400000_a3f9
다른 터미널에서 "랠리받을 준비해 rly_1747382400000_a3f9" 라고 말해줘.
받는 쪽 준비되면 여기에 "시작" 이라고 해.
```

**Terminal B (returner):**

```
랠리받을 준비해 rly_1747382400000_a3f9
```

Agent runs `rally status`, parses `[pattern:cycle]` from the task field, sets
`PATTERN=cycle`, frames itself as **executor**. Responds:

```
Returner 준비 완료. 서버가 서브할 때까지 대기 중.
서버가 넘겼다고 알려주면 그냥 "내 차례" 라고 말해.
```

**Four-turn walkthrough:**

| Turn | Side | Note passed via `rally done` |
|------|------|------------------------------|
| 1 | server (planner) | `[plan] step 1: add OAuth2 PKCE client config` |
| 2 | returner (executor) | `[result] diff: cmd/auth/oauth.go +42 / tests: pass` |
| 3 | server (planner) | `[review] approved. [plan] step 2: wire flag in CLI` |
| 4 | returner (executor) | `[result] diff: internal/cli/auth.go +28 / tests: pass` |

Server sends one more turn:

```
[review] approved. all slices done.
```

Session ends when the user says `끝` or after the final `[review] approved`.

---

### Pattern: discuss (multi-perspective)

**When to use:** design debates or architecture decisions where two viewpoints
must converge on a shared conclusion.

**Terminal A (server) — trigger:**

```
랠리보낼 준비해 — 논의 랠리
```

Agent sets `PATTERN=discuss`, prompts for the topic:

```
논의 주제가 뭐야?
> SQLite vs Postgres for v2
```

Then runs:

```bash
SID=$(rallish rally new --participants server,returner \
      --task "[pattern:discuss] SQLite vs Postgres for v2")
```

Both sides are framed as **peer** (no hierarchy). Note prefixes:
`[opinion]`, `[question]`, `[counter]`, `[agree]`.

**Five-turn walkthrough:**

| Turn | Side | Note |
|------|------|------|
| 1 | server (peer1) | `[opinion] migrate to Postgres; SQLite locks under write contention` |
| 2 | returner (peer2) | `[counter] WAL mode handles it; switching costs us 2 weeks` |
| 3 | server (peer1) | `[question] target wQPS where WAL still holds?` |
| 4 | returner (peer2) | `[opinion] our load peaks ~50 wQPS; WAL fine to 1000+` |
| 5 | server (peer1) | `[agree] stay on SQLite + WAL; revisit if wQPS > 500` |

Returner responds:

```
[agree] same
```

Both sides emitting `[agree]` within the last two turns signals convergence;
the session ends.

---

### Pattern: help (stuck-help)

**When to use:** the owner is blocked on a sub-problem and wants one or two
rounds of focused input from a helper before resuming solo work.

**Terminal A (owner) — trigger:**

```
랠리보낼 준비해 — 막혔어 도와줘
```

Agent sets `PATTERN=help`, framing server = **owner**, returner = **helper**.

**Three-turn walkthrough:**

| Turn | Side | Note |
|------|------|------|
| 1 | server (owner) | `[stuck] SSE writes block after 3 turns; tried setting WriteTimeout` |
| 2 | returner (helper) | `[hint] check whether http.ResponseController is shadowed by a parent context cancellation` |
| 3 | server (owner) | `[try] added context check; reproduced still` |
| 4 | returner (helper) | `[hint] flush before each write; goroutine may be GCed` |
| 5 | server (owner) | `[resolved] yes — added rc.Flush() after each event; root cause was buffered writer` |

**Helper rule:** the helper must not take more than ~3 consecutive `[hint]`
turns without an `[try]` from the owner. If that happens, the helper emits
`[suggest:share-context]` asking the owner to paste the relevant code or log.
The `[resolved]` note from the owner ends the session.

---

### Mid-rally pattern switch

Either side can propose switching to a different pattern mid-session using a
`[switch-pattern:<name>]` note:

**Proposer's turn:**

```bash
rallish rally done --session-id $SID --as server \
  --note "[switch-pattern:discuss] reason: scope expanded beyond a single task"
```

**Receiver's next turn** acknowledges:

```bash
rallish rally done --session-id $SID --as returner \
  --note "[switch-ack:discuss] switching — will frame next note as [opinion]"
```

Both sides update their local `PATTERN` after the exchange. The broker and CLI
are unaware of the switch; it is a pure convention between the two agents.

---

## Autoflow (v0.3+)

Starting with `rallish-operator` v0.3.0 and the matching CLI additions, both
sides of a rally can run fully autonomously after a **single setup trigger per
side**. The user types one sentence (server: `랠리보낼 준비해`; returner:
`랠리받을 준비해 <SID>`), and the agent handles every subsequent turn — composing
notes, calling `rally done`, waiting for the baton, and repeating — until a
pattern-specific exit signal or the user types `끝`.

What changed versus v0.2.0: the server no longer needs a phantom SSE join to
claim the baton (replaced by `rally new --first server`), and the returner no
longer needs to type `내 차례` between every turn (replaced by `rally join
--once --timeout 5m` blocking in a loop). Manual triggers (`내 차례`, `끝`, etc.)
remain supported at any time for override or fallback.

### Server-side (autoflow)

The user types:

```
랠리보낼 준비해 — 사이클로 가자
```

The agent runs `rallish doctor`, then creates the session with the baton
pre-assigned:

```sh
SID=$(rallish rally new \
      --participants server,returner \
      --first server \
      --task "[pattern:cycle] OAuth2 PKCE 도입")
```

Because of `--first server`, the session is immediately in `server_turn` /
`holder=server` / `turnN=1` — no SSE join is needed to claim the baton.

The agent tells the user:

```
🎾 Server 준비 완료. Session ID: rly_1747382400000_a3f9
다른 터미널에서 "랠리받을 준비해 rly_1747382400000_a3f9" 라고 말해줘.
```

The agent then composes and sends the first turn immediately:

```sh
rallish rally done \
      --session-id "$SID" \
      --as server \
      --note "[plan] step 1: add OAuth2 PKCE client config"
```

And enters the auto-loop — waiting for the baton, responding, and repeating
without further user input.

### Receiver-side (autoflow)

In the second terminal the user types:

```
랠리받을 준비해 rly_1747382400000_a3f9
```

The agent confirms the session:

```sh
rallish rally status --session-id rly_1747382400000_a3f9
```

It parses the `[pattern:cycle]` prefix from the `task` field, sets its own
`PATTERN=cycle`, frames itself as executor, and immediately enters the
auto-loop. **No `내 차례` trigger is required.** The first iteration of the loop
is:

```sh
rallish rally join \
      --session-id rly_1747382400000_a3f9 \
      --as returner \
      --once \
      --timeout 5m
```

This blocks until the baton arrives (exit 0) or 5 minutes pass with no baton
(exit 2). On exit 0 the agent reads the cue, composes its response, calls
`rally done`, checkpoints to the user with a one-liner, then loops back to
`rally join --once` for the next turn.

### Behaviour reference

| Aspect | Detail |
|---|---|
| **discuss** exit | Both sides emit `[agree]` within the last two turns (mutual convergence). |
| **cycle** exit | Planner emits `[review] approved` and there are no pending `[plan]` items in history. |
| **help** exit | Owner emits `[resolved] …` (helper detects this and stops the loop). |
| **Timeout fallback** | If no baton arrives within 5 minutes, the agent checkpoints to the user: `"🎾 5분간 baton 없음. 계속 대기할까(엔터)? 끝낼까(끝)?"`. Default: loop again on silence; break on `끝`. |
| **Hard ceiling** | After 20 iterations without an exit signal the agent pauses and asks the user whether to continue: `"🎾 20턴 넘었어. 정리할까?"`. |
| **User interruption** | The agent yields to the user between every `rally done` and the next `rally join --once`. If the user types anything during that window — including `끝` — the loop honours it. Silence = continue. |

### When to fall back to manual mode

The auto-loop requires the agent process to remain active between turns. If the
loop blocks unexpectedly (network blip, broker restart, or long agent
compaction), the user can always type `내 차례` and the v0.2.0 manual flow
resumes without any setup change:

```sh
rallish rally status --session-id $SID   # confirm it's your turn
rallish rally done   --session-id $SID \
                     --as returner \
                     --note "[result] …"
```

The two flows are compatible with the same session; switching between auto and
manual mid-session is safe.

---

## Reference

- PRD: [`docs/prd-rally-mode.md`](./prd-rally-mode.md)
- Rally patterns PRD: [`docs/prd-rally-patterns.md`](./prd-rally-patterns.md)
- Rally types: [`pkg/contract/rally.go`](../pkg/contract/rally.go)
- Broker handlers: [`internal/broker/rally.go`](../internal/broker/rally.go)
- CLI commands: [`internal/cli/rally.go`](../internal/cli/rally.go)
- IPC runbook: [`docs/runbook-ipc-unix-socket.md`](./runbook-ipc-unix-socket.md)
