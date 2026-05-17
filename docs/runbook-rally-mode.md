# Runbook: Rally Mode — Manual Verification

> **Branch:** `feat/rally-mode`
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

## Reference

- PRD: [`docs/prd-rally-mode.md`](./prd-rally-mode.md)
- Rally types: [`pkg/contract/rally.go`](../pkg/contract/rally.go)
- Broker handlers: [`internal/broker/rally.go`](../internal/broker/rally.go)
- CLI commands: [`internal/cli/rally.go`](../internal/cli/rally.go)
- IPC runbook: [`docs/runbook-ipc-unix-socket.md`](./runbook-ipc-unix-socket.md)
