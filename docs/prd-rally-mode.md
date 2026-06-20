# PRD: Rally Mode — Live Baton-Passing Between Interactive CLI Sessions

## §1 Problem Definition

`rallish start` is headless-only: it spawns goroutine adapters inside the broker process. There is no path for two *human-launched* CLI sessions (e.g. two `claude` instances the user opens in separate terminals) to take turns on a shared task. The broker has no concept of an interactive participant waiting on a baton. Additionally, the `start` command conflates two unrelated concerns: the headless orchestrator preset system and the future interactive rally mode, making the CLI surface ambiguous.

## §2 Decision & Rationale

**Selected:** (a) Introduce `rallish rally` — a new subcommand group for live baton-passing between two named interactive participants. (b) Rename `rallish start` → `rallish squash` as the umbrella for headless presets (`solo-ralph`, `pair-review`). No backward-compat alias per AGENTS.md convention.

**Why:** Rally mode requires a fundamentally different broker primitive — a persistent SSE stream per named participant with exclusive baton ownership — which does not fit inside the existing role-based session model. A clean subcommand split makes the surface self-documenting and avoids overloading `--preset` with interactive vs headless semantics. Squash is the better name for "crushing through a task with headless agents."

## §3 Alternatives (Rejected)

| Alt | Pros | Cons | Verdict |
|-----|------|------|---------|
| A. Reuse existing `/sessions/:id/next?as=` SSE | Zero new routes | Role model and baton model conflict; 409/stale logic doesn't fit turn semantics | Rejected |
| B. Keep `start`, add `--interactive` flag | Fewer commands | Flag soup; squash and rally are orthogonal features not a continuum | Rejected |
| C. Backward-compat alias `start` → `squash` | Softer migration | Contradicts AGENTS.md / DESIGN.md "no shims" rule | Rejected |

## §4 Implementation Spec

### 4.1 New files

```
pkg/contract/rally.go          // RallySession, BatonEvent, DoneRequest public types
internal/broker/rally.go       // broker handlers for /rally/... routes
internal/broker/rally_test.go  // table-driven + httptest coverage
internal/cli/rally.go          // Cobra subcommands: new, join, done, status
internal/cli/squash.go         // renamed from start.go; RunStart → RunSquash
```

No new files at repo root. `internal/cli/start.go` is renamed to `internal/cli/squash.go`; `cmd/rallish/main.go` is updated in-place (startCmd → squashCmd, `cli.RunStart` → `cli.RunSquash`).

### 4.2 Contract types (`pkg/contract/rally.go`)

```go
type RallySession struct {
    ID           string            `json:"id"`
    Participants []string          `json:"participants"`
    Repo         string            `json:"repo,omitempty"`
    Task         string            `json:"task,omitempty"`
    Holder       string            `json:"holder"`       // current baton holder name
    TurnN        int               `json:"turn_n"`
    Status       string            `json:"status"`       // idle|<name>_turn|interrupted
    LastSeen     map[string]int64  `json:"last_seen"`    // participant → unix ms
    History      []BatonHandoff    `json:"history"`
    CreatedAt    int64             `json:"created_at"`   // unix ms
}

type BatonHandoff struct {
    From  string `json:"from"`
    To    string `json:"to"`
    TurnN int    `json:"turn_n"`
    Note  string `json:"note,omitempty"`
    At    int64  `json:"at"` // unix ms
}

type BatonEvent struct {
    TurnN int    `json:"turn_n"`
    From  string `json:"from"`
    Note  string `json:"note,omitempty"`
}

type DoneRequest struct {
    Note      string `json:"note,omitempty"`
    HandoffTo string `json:"handoff_to,omitempty"`
}
```

### 4.3 Broker routes (`internal/broker/rally.go`)

```
POST /rally/sessions                      → handleRallyCreate
GET  /rally/sessions/{id}/baton?as=<name> → handleRallyBaton   (SSE)
POST /rally/sessions/{id}/done?as=<name>  → handleRallyDone
GET  /rally/sessions/{id}                 → handleRallyStatus
```

Registered in `broker.NewServer` alongside existing routes. Rally state lives in a separate `rallyStates map[string]*rallyState` under `s.mu`.

### 4.4 State machine

```
idle
  → on first join (baton GET): holder = participants[0], status = "<name>_turn"
  → explicit override: POST /rally/sessions/:id/done?as=first with handoff_to=<name>

<name>_turn
  → SSE event sent to <name>'s baton stream (BatonEvent)
  → non-current participant POSTs done → 409 Conflict
  → current participant POSTs done → transition to next participant (round-robin
    or handoff_to if provided and in participants list)
  → handoff_to not in participants list → 400 Bad Request

interrupted
  → set on broker SIGTERM; SSE connections receive clean close before handler returns
  → any subsequent POST done → 409 Conflict (session interrupted)
  → any subsequent baton join by a session member → immediate `{"closed":true}` sentinel (non-members still receive 403)
```

Session ID format: `rly_<unixmillis>_<rand4hex>` (e.g. `rly_1747382400000_a3f9`). Four hex chars from `crypto/rand` read once at create time. Validated by `validateRallyID`.

### 4.5 Heartbeat / liveness

Each open baton SSE stream sends a `: ping\n\n` SSE comment every 15 s. Broker records `last_seen[name] = now.UnixMilli()` on each ping write. `handleRallyStatus` marks a participant `stale` if `now - last_seen[name] > 30000 ms`. Staleness is advisory; it does not auto-advance the baton.

SSE writes use `http.ResponseController.SetWriteDeadline` (2 s per write) + `select` on `ctx.Done()` to avoid blocking on dead clients.

### 4.6 CLI commands (`internal/cli/rally.go`)

```
rallish rally new --participants A,B [--repo <path>] [--task <text>]
  → POST /rally/sessions; prints session-id

rallish rally join --session-id <id> --as <name>
  → GET /rally/sessions/:id/baton?as=<name>
  → blocks on SSE; on BatonEvent prints: "🎾 your turn (turn <N>, from <from>): <note>"

rallish rally done --session-id <id> --as <name> [--note <text>] [--handoff-to <name>]
  → POST /rally/sessions/:id/done?as=<name>  body: DoneRequest

rallish rally status --session-id <id>
  → GET /rally/sessions/:id; pretty-prints holder, turn count, stale flags, last 5 history entries
```

`--participants` is comma-split; each name validated against `[a-zA-Z0-9_-]{1,16}`.

### 4.7 Squash rename

- `internal/cli/start.go` → `internal/cli/squash.go`; function `RunStart` → `RunSquash`; type `StartOptions` → `SquashOptions`.
- `cmd/rallish/main.go`: `startCmd()` → `squashCmd()`; wire `cli.RunSquash`.
- Cobra command Use string: `"squash"`. No `"start"` alias registered.

## §5 Test Criteria

- `TestRallyCreateAndStatus`: POST create → GET status returns `idle`.
- `TestRallyBatonTwoRound`: two httptest clients (`A`, `B`) complete two full baton rounds; history has 4 entries.
- `TestRallyDoneNonHolder409`: B POSTs done when A holds → 409.
- `TestRallySSEStaleAfterHeartbeat`: manipulate clock past 30 s; status marks participant stale.
- `TestRallyInvalidParticipantName`: name `"bad name!"` → 400.
- `TestRallyContentTypeEnforcement`: POST without `Content-Type: application/json` → 415.
- `TestSquashRunEquivalence`: integration test starts squash with `solo-ralph` preset; verifies session reaches terminal state (matches existing `RunStart` golden).
- Race-free: `go test -race ./internal/broker/... ./internal/cli/...`

## §6 Guardrails

- No new external dependencies; stdlib only (`crypto/rand`, `net/http`, `log/slog`, etc.).
- Session ID prefix `rly_` distinguishes rally sessions from squash sessions at a glance.
- Participant name: regex `^[a-zA-Z0-9_-]{1,16}$`; validated at create and at every done/join handler.
- `--repo` path passed through `internal/safepath` (already in AGENTS.md layout); reject paths that escape the provided root.
- SSE write deadline: `http.ResponseController.SetWriteDeadline(time.Now().Add(2*time.Second))` before each `fmt.Fprintf`; on error, log and return.
- All POST handlers check `r.Header.Get("Content-Type") == "application/json"`; respond 415 otherwise.
- Coverage floor ≥70% on `internal/broker/rally.go` and `internal/cli/rally.go` (extends existing AGENTS.md list).
- No new files at repo root; no global mutable state added to `broker.Server` beyond `rallyStates map[string]*rallyState` under the existing `s.mu`.
- SIGTERM path: `broker.Server` shutdown drains open SSE connections by cancelling their request contexts before `http.Server.Shutdown` returns; each baton handler writes a terminal `data: {"closed":true}\n\n` then exits cleanly.

## §7 Acceptance Criteria

1. `make check` passes (`go vet` + `golangci-lint` + `go test -race ./...`).
2. Two-terminal manual smoke: `rallish rally new --participants A,B --task "ping"` → session-id printed; terminal-1 `rallish rally join ... --as A` blocks; terminal-2 `rallish rally status ...` shows `A_turn`; terminal-1 `rallish rally done ... --as A`; terminal-2's join unblocks and prints baton cue; repeat one more round; `rallish rally status` shows 2 handoffs in history.
3. `rallish squash --preset solo-ralph --task "ping"` runs to completion identically to the former `rallish start --preset solo-ralph --task "ping"` (no regression on session creation, runner loop, exit conditions).
4. SIGTERM to broker mid-rally: participants' SSE connections receive `data: {"closed":true}` before the process exits; after restart, `rallish rally status` returns `404 session not found` (rally sessions are in-memory only).
5. README (EN/KO/JP) and CHANGELOG (EN/KO/JP) updated; `docs/runbook-rally-mode.md` exists and describes the two-terminal smoke test end-to-end.
