# rallish — Design

> *"Two agents, one melody."*
> A local broker that lets multiple coding-agent CLIs (Claude Code, Kimi Code, …) turn-taking — alternate turns — on a single task.

---

## 0. Implementation

**Language: Go 1.25+** (locked in)

Why Go:
- We are a CLI orchestrator that spawns other CLIs over OS pipes and serves a local HTTP/SSE broker. This is the exact category Go was built for: `gh`, `kubectl`, `terraform`, `docker`, `lazygit`, `claude-squad` all live here.
- Single static binary, cross-compiled from one machine; no runtime required on user machines.
- We do **not** call LLM APIs directly, so Python's AI ecosystem advantage is irrelevant.
- Long build/run stability is a Go design promise — important for a tool we want to keep alive for years.

Toolchain:

| Tool | Purpose |
|---|---|
| Go 1.25+ | language; pin minor in `go.mod` |
| `golangci-lint` | lint aggregator (config in `.golangci.yml`) |
| `goreleaser` | cross-platform release builds + Homebrew tap |
| `make` | developer entrypoint (`make test`, `make build`, `make check`) |
| GitHub Actions | CI: lint + test on push, release on tag |
| `gosec` | security linter (run via golangci-lint) |
| `govulncheck` | dependency vuln scan (run in CI) |

---

## 1. What this is, in one paragraph

`rallish` is a small **local broker process** that sits between N "agent runtimes" — each of which is an off-the-shelf coding CLI like `claude`, `kimi`, `codex`, etc. The broker owns the conversation state, decides whose turn it is, and shuttles compact turn payloads between them. Each agent runtime keeps using its own sub-agent / tool ecosystem internally; rallish only routes turns at the outer boundary.

The wire format follows the **A2A (Agent2Agent) protocol** where reasonable, so any A2A-compliant agent can be plugged in via an adapter.

---

## 2. Non-goals

- Not a new agent framework. We do not call LLM APIs directly.
- Not a tmux multiplexer. claude-squad / Crystal already do that; we are about **turn-level dialogue**, not parallel sessions.
- Not a UI. CLI + log files + (later) a tiny status TUI. Web dashboard is a stretch goal.
- Not a sandbox. Agents run with whatever permissions the user gives the CLI they wrap.

---

## 3. Architecture (runtime topology)

```
┌──────────────────────────────────────────────────┐
│  rallish broker (Go, single process, 127.0.0.1) │
│                                                  │
│  POST  /sessions               start session     │
│  GET   /sessions/:id/next?as=X SSE next-turn     │
│  POST  /sessions/:id/turn      submit response   │
│  GET   /sessions/:id           snapshot          │
│  POST  /sessions/:id/stop      end + reason      │
│                                                  │
│  internals:                                      │
│    SessionStore  → jsonl append-only log         │
│    Router        → decides next role/runtime     │
│    Budgeter      → tokens / turns / wallclock    │
│    ExitEvaluator → fires when exit_when matches  │
└────────▲────────────────────────────▲────────────┘
         │                            │
   ┌─────┴──────┐               ┌─────┴──────┐
   │ adapter:   │               │ adapter:   │
   │   claude   │               │   kimi     │
   │ (claude -p │               │ (kimi -p   │
   │  --output- │               │  …)        │
   │  format=   │               │            │
   │  stream-   │               │            │
   │  json)     │               │            │
   └────────────┘               └────────────┘
   each adapter is a goroutine (or out-of-process worker) that loops:
     1. GET /next  (long-poll via SSE)
     2. exec the agent CLI with prompt = turn payload
     3. parse stdout → TurnResponse
     4. POST /turn
```

In v0, adapters run **as goroutines in the broker process** for simplicity. The interface is identical to an out-of-process adapter (HTTP), so we can split later without breaking anyone.

---

## 4. Project layout

Standard Go layout. Public surface (`pkg/`) is small on purpose; everything else is `internal/`.

```
rallish/
├── cmd/
│   └── rallish/                main.go — CLI entrypoint, flag parsing only
├── internal/
│   ├── broker/                  HTTP/SSE server, route handlers
│   ├── session/                 SessionStore, jsonl append, replay
│   ├── router/                  next-turn decision logic
│   ├── exit/                    ExitEvaluator + shell predicate runner
│   ├── budget/                  token / turn / deadline tracking
│   ├── scratch/                 rolling scratchpad + compaction trigger
│   ├── preset/                  YAML loader, strict schema validation
│   ├── adapter/                 adapter interface + registry
│   │   ├── claude/              wraps `claude -p`
│   │   ├── kimi/                wraps kimi headless
│   │   └── fake/                deterministic test adapter
│   ├── ipc/                     Unix-socket transport for `rallish attach`
│   ├── logx/                    structured logging + secret redaction
│   ├── safepath/                path-traversal-safe filepath helpers
│   └── buildinfo/               version / commit / date (injected via ldflags)
├── pkg/
│   └── contract/                public types (TurnRequest, TurnResponse, …)
│                                — stable, importable by 3rd-party adapters
├── presets/                     shipped YAML presets (embedded via //go:embed)
├── testdata/                    fixtures
├── .golangci.yml
├── .goreleaser.yaml
├── .github/workflows/ci.yml
├── Makefile
├── go.mod / go.sum
├── DESIGN.md
└── README.md
```

Conventions:
- `cmd/rallish/main.go` is **< 100 lines**. It wires flags → calls `internal/...` packages. No business logic.
- Anything in `internal/` is private. 3rd-party adapter authors only see `pkg/contract`.
- No package depends on another's internals; pass interfaces, not concrete structs, at boundaries.
- Every package has a `doc.go` with a one-paragraph explanation.
- Each package has `_test.go` files. Broker uses `httptest`. Adapters use the `fake` adapter for cross-package tests.

---

## 5. Architecture conventions

Hexagonal-ish. The core (`session`, `router`, `exit`, `budget`, `scratch`) knows nothing about HTTP or processes. The broker (HTTP/SSE) and adapters (`os/exec`) are the ports around it.

Hard rules:

1. **`context.Context` is the first arg** of every function that does I/O, spawns processes, or could block. Cancellation propagates from CLI → broker → adapter → child process.
2. **No package-level mutable state.** Stores and registries are constructed in `main.go` and injected.
3. **Errors wrap with `fmt.Errorf("...: %w", err)`**. Use `errors.Is` / `errors.As` at boundaries. Never `panic` outside `main`.
4. **No `interface{}` / `any` in the contract package.** Types are concrete and JSON-tagged.
5. **Time is injected** (`type Clock interface{ Now() time.Time }`) so tests can fast-forward budgets/deadlines.
6. **Logs are structured** (`log/slog`), never `fmt.Println`. One global handler set in main; everywhere else logs via context.
7. **Public API stability**: `pkg/contract` follows semver. `internal/*` can break freely.

---

## 6. Development cycle

Per change:
1. Write/extend a test in the affected package.
2. Implement.
3. `make check` = `go vet ./... && golangci-lint run && go test ./... -race`.
4. Commit with conventional-commit prefix (`feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`, `sec:`).
5. Push; CI re-runs `make check` + `govulncheck` + builds binary.

Release (tagged):
1. Bump version in `internal/buildinfo` (or rely on git tag injected via ldflags).
2. Tag `vX.Y.Z`; CI triggers `goreleaser`.
3. Artifacts: macOS arm64/amd64, Linux arm64/amd64, Windows amd64; checksums + SBOM (syft).
4. Homebrew tap auto-updated by goreleaser.
5. Changelog generated from conventional commits.

Quality bars:
- Test coverage ≥ 70% on `internal/session`, `internal/router`, `internal/exit`, `internal/preset`, `pkg/contract`.
- No new `gosec` findings at HIGH severity.
- `go test -race` clean.

---

## 7. Contract (the only thing other projects need to agree on)

Lives in `pkg/contract`.

### TurnRequest (broker → adapter)

```go
type TurnRequest struct {
    Session       string         `json:"session"`
    Turn          int            `json:"turn"`            // monotonic
    Role          string         `json:"role"`            // planner|executor|reviewer|...
    RuntimeHint   string         `json:"runtime_hint"`    // claude|kimi|...
    ModelHint     string         `json:"model_hint,omitempty"`
    Budget        Budget         `json:"budget"`
    ScratchPath   string         `json:"shared_scratch_path"`
    LastTurn      *LastTurn      `json:"last_turn,omitempty"`
    Task          Task           `json:"task"`
    ExitWhen      []ExitCondition `json:"exit_when"`
}
```

### TurnResponse (adapter → broker)

```go
type TurnResponse struct {
    Done           bool     `json:"done"`
    HandoffTo      string   `json:"handoff_to,omitempty"`
    Summary        string   `json:"summary"`              // ≤ ~400 tokens; the only thing the next turn sees
    Artifacts      []string `json:"artifacts,omitempty"`  // file paths touched
    SelfEval       string   `json:"self_eval"`            // confident|uncertain|blocked
    NotesForHuman  string   `json:"notes_for_human,omitempty"`
    Usage          *Usage   `json:"usage,omitempty"`      // tokens_in, tokens_out, ms
}
```

**Why `Summary` is mandatory:** the next turn does NOT receive the previous agent's full transcript. It receives only `Summary` + the shared scratch file. This is the single biggest token-optimization lever.

### Mapping to A2A

- A2A `Message` ≈ TurnRequest / TurnResponse
- A2A `Task` ≈ Session
- A2A `Artifact` ≈ artifacts[] (file paths, not blobs — we are local)

We start with our own JSON shape; expose an A2A-compatible endpoint in Phase 5.

---

## 8. Session state on disk

```
<repo_root>/.rallish/
  config.yaml                          # which preset to use in this repo
  sessions/
    <session-id>/
      log.jsonl                        # append-only: every TurnRequest + TurnResponse
      scratch.md                       # rolling shared scratchpad
      meta.json                        # status, start time, exit reason
      artifacts/                       # optional: diffs/snapshots per turn
```

`log.jsonl` is the source of truth. Broker death → on restart, in-memory state is rebuilt by replaying the log.

---

## 9. Router & ExitEvaluator

### Router

Picks the next role. Order of precedence:
1. If previous TurnResponse has `HandoffTo` → use it (with allowlist check).
2. Else apply preset `routing` rule (default: `round_robin`).
3. If a role is `blocked`, escalate to `reviewer` if defined, else pause session.

### ExitConditions

```yaml
exit_when:
  - all_artifacts_compile        # runs repo-defined typecheck command
  - tests_pass                   # runs repo-defined test command
  - reviewer_approved            # last reviewer turn had self_eval=confident && done=true
  - turns_exhausted              # budget.turns_left == 0
  - tokens_exhausted
  - deadline_passed
  - human_signal                 # `rallish stop <id>`
```

Each condition is a small predicate. Shell-running predicates (`tests_pass`, `all_artifacts_compile`) require explicit opt-in via `--allow-shell-exit` to avoid surprise execution.

---

## 10. Preset format

```yaml
# .rallish/config.yaml or ~/.rallish/presets/<name>.yaml
name: pair-review
description: Claude plans, Kimi executes, Claude reviews.

roles:
  - id: planner
    runtime: claude
    model: opus
  - id: executor
    runtime: kimi
    model: kimi-k2
  - id: reviewer
    runtime: claude
    model: sonnet

routing: handoff_then_round_robin   # | strict_round_robin | last_writer_wins

budget:
  max_turns: 20
  max_tokens: 400000
  deadline_minutes: 60

exit_when:
  - tests_pass
  - reviewer_approved
  - turns_exhausted

scratch:
  max_kb: 64
  summarize_with: claude-haiku
```

Shipped presets (target):
- `pair-review` — planner / executor / reviewer
- `solo-ralph` — one runtime, broker just runs the ralph loop with budget/exit
- `triad` — three peers, group-chat routing
- `dueling-executors` — same task to 2 runtimes in parallel, reviewer picks winner

Loader uses **strict YAML** (`yaml.Decoder.KnownFields(true)`). Unknown keys are errors, not warnings.

---

## 11. Token-optimization rules (non-negotiable)

1. **Next turn never receives the previous full transcript.** Only `Summary` + scratch diff.
2. **Scratch is rolling**: when it exceeds `scratch.max_kb`, broker invokes `scratch.summarize_with` to compact the oldest half into a 1-paragraph block.
3. **Haiku-tier compaction interludes**: presets may insert a `compactor` role every K turns whose only job is to rewrite scratch tighter. Cheap turns save expensive ones.
4. **Adapters MUST report token usage** in `TurnResponse.Usage`. Broker shows running totals; budget enforcement kicks in here.
5. **No "be helpful" prompt bloat from the broker.** TurnRequest is structured data, not prose.

---

## 12. Adapters

Each adapter implements:

```go
type Adapter interface {
    Name() string
    Run(ctx context.Context, req contract.TurnRequest) (contract.TurnResponse, error)
}
```

### claude adapter (initial)

- Invokes: `claude -p <prompt> --output-format=stream-json --max-turns=1`
- Prompt is a deterministic template embedding TurnRequest as a fenced JSON block + a brief instruction to reply with a fenced JSON TurnResponse.
- Parses last fenced JSON from stdout.
- Binary resolved once via `exec.LookPath` at startup; absolute path stored.

### kimi adapter (initial)

- TODO Phase 0: confirm `kimi` headless flags. Adapter follows the same pattern: prompt-in, JSON-out.
- If kimi has no stable stdout protocol, adapter writes to a tempfile and reads result.

### Pluggable

Adapters register in `internal/adapter/adapter.go`'s registry by name. Adding a new runtime = one file + one line in the registry.

---

## 13. CLI surface

```
rallish squash [--preset NAME] [--task FILE|"inline task"] [--session-id ID]
rallish rally new --participants A,B [--task TEXT]
rallish rally join --session-id ID --as NAME
rallish rally done --session-id ID --as NAME [--note TEXT]
rallish rally status --session-id ID
rallish attach <session-id>           # follow live SSE stream in terminal
rallish status [<session-id>]         # show all running / recent sessions
rallish stop <session-id> [--reason]
rallish log <session-id> [--turn N]   # pretty-print log.jsonl
rallish presets                       # list shipped + user presets
rallish doctor                        # check adapters, paths, perms
rallish daemon                        # explicit foreground broker (rarely used)
rallish version                       # version + commit + go runtime
```

Daemon mode: `rallish squash` will auto-spawn a daemon (background broker) if none is running. CLI commands talk to it over Unix socket at `~/.rallish/sock`. The broker also listens on `127.0.0.1:<port>` (port written to `~/.rallish/port`) — kept simple for adapter introspection.

---

## 14. Security

This tool spawns child processes and exposes a local HTTP server. The threat model is **"shared developer machine"** — not a public service.

### Threat model

| Threat | Mitigation |
|---|---|
| Non-broker local process talks to the broker | Bind `127.0.0.1` **and** require per-session bearer token on every HTTP/SSE request |
| Token leak via process list | Token lives in `~/.rallish/sock.token` (mode `0600`), never in argv or env |
| Path traversal via `repo_root` / `ScratchPath` / `artifacts` | `internal/safepath`: `filepath.Clean` + `EvalSymlinks` + `HasPrefix` check against the session's allowed roots; reject anything that escapes |
| Command injection via preset / task fields | **Never `sh -c`.** Always `exec.Command(name, args...)` with literal arg slices. `forbidigo` lint rule bans `exec.Command("sh"`/`bash` |
| Malicious YAML preset (unknown keys, type confusion) | `yaml.Decoder.KnownFields(true)`; reject unknown keys; allowlist field types via reflection |
| Child-process env leaks (`AWS_*`, repo secrets) | Adapters spawn with a **minimal env allowlist** (`PATH`, `HOME`, `LANG`, `TERM`, plus per-runtime explicit set like `ANTHROPIC_*` for claude); never inherit full `os.Environ()` |
| Secrets in TurnRequest/Response → `log.jsonl` | `internal/logx` redaction layer scrubs known patterns (Bearer/Basic auth, common API-key prefixes `sk-…`, `xoxb-…`, `ghp_…`) before write |
| Oversized payload DoS | Hard caps: `task.body` ≤ 64 KB, `Summary` ≤ 8 KB, scratch ≤ `preset.scratch.max_kb`, total session size cap with backpressure (HTTP 413) |
| Long-running session leak (child won't die) | Mandatory `deadline_minutes`. Broker enforces via `ctx` cancel → `SIGTERM` → 5s grace → `SIGKILL` |
| SSE reconnect hijack | Every SSE reconnect re-validates the bearer token; sessions bind to the token they were created with |
| Adapter binary substitution (PATH attack) | Adapters resolve via `exec.LookPath` once at startup; store absolute path; `rallish doctor` prints resolved paths |
| State files readable by other users | `~/.rallish/` created mode `0700`; all child files mode `0600` |
| Symlink race when writing artifacts | Open with `O_NOFOLLOW` where available; check `Lstat` before write |
| Untrusted preset hosted in `~/.rallish/presets/` modified by another user | Refuse to load presets whose owner ≠ current uid (`syscall.Stat_t`) |
| Crash-time secrets in core dumps | Disable core dumps where applicable (`prctl(PR_SET_DUMPABLE, 0)` on Linux); on macOS, defaults are safe |
| Supply-chain (compromised dependency) | `govulncheck` in CI; pin `go.sum`; minimal direct deps |

### Non-mitigations (explicitly out of scope for v0)

- We do **not** sandbox the wrapped agent CLI. If the user runs `claude` with full write access to their repo, `rallish` does too — same as running `claude` directly. Sandboxing belongs to the user (containers, `container-use`, etc.).
- We do not vet the prompts the agent sends to its LLM provider. Prompt-injection from a malicious task description is a risk the user accepts.
- We do not encrypt local state at rest. Disk encryption is the OS's responsibility.

### Security tests required before v1

- Fuzz `preset` YAML loader.
- Fuzz the contract JSON decoders.
- Path-traversal table tests on every path that comes from request bodies.
- `forbidigo` rule: `os.Environ()` never reaches `exec.Cmd.Env`.
- `forbidigo` rule: `exec.Command("sh"|"bash"|"zsh", ...)` is banned.

---

## 15. Phased delivery

Each phase is a runnable cut. Do not bundle.

### Phase 0 — Draft + scaffold
- [x] `DESIGN.md` (this file)
- [ ] `go.mod` (`module github.com/<owner>/rallish`, `go 1.25`)
- [ ] Directory skeleton per §4 with `doc.go` stubs
- [ ] `Makefile` (`build`, `test`, `check`, `run`, `lint`, `tidy`)
- [ ] `.golangci.yml` — enable: `gofmt, govet, errcheck, staticcheck, ineffassign, gosimple, unused, forbidigo, gosec, revive, gocritic`
- [ ] `.goreleaser.yaml` (placeholder; full wiring in Phase 4)
- [ ] `.github/workflows/ci.yml` — `make check` + `govulncheck`
- [ ] `pkg/contract/types.go` — TurnRequest, TurnResponse, ExitCondition, Budget, etc. with JSON tags + struct docs
- [ ] `internal/preset/preset.go` — strict YAML loader + zod-like validation
- [ ] `cmd/rallish/main.go` — `version` / `doctor` stubs only
- [ ] Smoke: `make build && ./dist/rallish version` prints semver

### Phase 1 — Broker MVP
- [ ] `internal/session` — jsonl-backed SessionStore, replay on startup
- [ ] `internal/broker` — Hono-equivalent using `net/http` + `chi` (or stdlib mux); routes: `POST /sessions`, `GET /sessions/:id/next` (SSE), `POST /sessions/:id/turn`, `GET /sessions/:id`
- [ ] `internal/router` — round-robin + handoff
- [ ] `internal/budget` — token/turn/deadline
- [ ] Tests: `fake` adapter ping-pongs 3 turns; `log.jsonl` correctness via golden file

### Phase 2 — Adapters
- [ ] `internal/adapter/claude` — wraps `claude -p`
- [ ] `internal/adapter/kimi` — wraps kimi headless (confirm flags)
- [ ] `internal/adapter/fake` — deterministic test adapter
- [ ] `rallish doctor` verifies each adapter binary

### Phase 3 — Loop runner & presets
- [ ] `rallish squash` boots broker (if not running) + registers adapter goroutines per role
- [ ] Preset loader integration
- [ ] ExitEvaluator with `turns_exhausted`, `tests_pass` (shell), `reviewer_approved`
- [ ] Ship presets: `pair-review`, `solo-ralph` (embedded via `//go:embed`)

### Phase 4 — Packaging & reuse
- [ ] Full `goreleaser.yaml`; tagged release pipeline
- [ ] Homebrew tap (`<owner>/homebrew-rallish`)
- [ ] `rallish init` drops `.rallish/config.yaml` in any repo
- [ ] Dogfood: run rallish in a different repo successfully

### Phase 5 — Autonomy, A2A, polish
- [ ] Scratch rolling-window compaction (haiku role)
- [ ] Token meter with budget enforcement and live UI
- [ ] Watchdog: blocked/no-response → escalate role
- [ ] A2A-compatible HTTP endpoint (`/a2a/...`) layered over internal contract
- [ ] Optional TUI (`rallish status --watch`) using `bubbletea`

---

## 16. Open questions for Phase 0

1. **Kimi headless flags** — does `kimi` ship a `--print` / `-p` non-interactive mode with stable stdout? If not, adapter writes to/reads from a tempfile.
2. **Broker bind** — default `127.0.0.1:<random>`, port written to `~/.rallish/port`. Unix socket as primary, TCP as fallback.
3. **Single-binary size** — Go binaries are ~20–30 MB stripped, fine.
4. **Concurrency model** — adapters as goroutines in v0; out-of-process workers as a Phase 5 option behind the same interface.
5. **Module path** — `github.com/<owner>/rallish`; decide owner before `go mod init`.

---

## 17. Definitely-not-doing (v0)

- Web UI
- Multi-host distribution
- Encryption / multi-tenant auth
- Persistent SQLite (jsonl is enough)
- Built-in LLM calls
- Git history manipulation
- Sandboxing the wrapped agents (delegate to the user)

---

## 18. Naming reference

- The product: **rallish**
- The binary: `rallish` (alias `htty` is optional)
- A "session" = one task, however many turns
- A "turn" = one (request, response) pair
- A "relay" = one alternating exchange between two agents

Anything new follows this vocabulary. No `agent.go` / `manager.go` / `executor.go` boilerplate — use the domain terms.
