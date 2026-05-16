# rallish

> A local broker for multi-agent turn-taking, A2A-compliant.

![version](https://img.shields.io/badge/version-0.0.1-blue)
![license](https://img.shields.io/badge/license-MIT-green)
![go](https://img.shields.io/badge/go-1.25+-blue)

**rallish** is a small local broker process that sits between N agent runtimes — any coding CLI with an adapter (Claude, Kimi, Cursor, Codex, etc., or even the same kind running in different contexts). The broker owns the conversation state, decides whose turn it is, and shuttles compact turn payloads between them.

Everything runs locally. No cloud broker, no external coordination service. The wire format follows the **A2A (Agent2Agent) protocol** where reasonable, so any A2A-compliant agent can be plugged in via an adapter.

[한국어](./README.ko.md) · [日本語](./README.jp.md)

## Features

| Feature | Description |
|---------|-------------|
| **Squash (headless)** | `rallish squash` runs headless preset sessions (`solo-ralph`, `pair-review`); broker spawns adapters automatically |
| **Rally (interactive)** | `rallish rally` provides live baton-passing between two or more human terminals; exclusive holder enforcement via SSE |
| **A2A Protocol** | `/.well-known/agent.json`, JSON-RPC 2.0 tasks, SSE streaming |
| **Token Budgets** | Hard caps on tokens, turns, and wall-clock time per session |
| **Scratchpad** | Rolling shared scratch with automatic compaction |
| **Presets** | YAML templates for roles, routing, and exit conditions |
| **Unix socket IPC** | CLI↔Daemon over `~/.rallish/rallish.sock` (mode `0600`); TCP loopback retained for A2A clients and Windows fallback |
| **Auto-daemon** | `rallish squash` spawns the broker if none is running; `rallish doctor` reports socket reachability |
| **Security** | Path traversal guards, secret redaction, minimal env allowlists |

## Architecture

```
┌──────────────────────────────────────────┐
│  rallish broker (Go)                     │
│  POST /sessions                          │
│  GET  /sessions/:id/next?as=<role> (SSE) │
│  POST /sessions/:id/turn                 │
│  GET  /.well-known/agent.json            │
│  POST /a2a                               │
└──┬───────────────┬───────────────────┬───┘
   │ unix socket   │ unix socket       │ tcp loopback
   │ ~/.rallish/   │ ~/.rallish/       │ 127.0.0.1:<port>
   │ rallish.sock  │ rallish.sock      │ (A2A + fallback)
┌──┴─────────┐   ┌─┴────────┐      ┌──┴───────────┐
│  agent A   │   │ agent B  │      │ external A2A │
│  (CLI)     │   │  (CLI)   │      │  client      │
└────────────┘   └──────────┘      └──────────────┘
```

Same broker serves both transports concurrently. The CLI (`rallish squash`, `rallish rally`, `rallish doctor`) prefers the Unix socket; external A2A clients use TCP loopback.

## Prerequisites

- **Go 1.25+** (for building from source)
- At least one compatible agent CLI installed and authenticated (see supported adapters)

Verify:

```bash
go version        # must be 1.25+
which claude      # any supported adapter binary on $PATH
```

## Install

### Option 1 — From source (recommended for development)

```bash
git clone https://github.com/jazz1x/rallish.git
cd rallish
make build
```

Binary appears at `./dist/rallish`.

### Option 2 — Homebrew (after first release)

```bash
brew tap jazz1x/rallish
brew install rallish
```

### Option 3 — go install

```bash
go install github.com/jazz1x/rallish@latest
```

## Quickstart

```bash
# Environment check (reports adapter presence + daemon reachability)
./dist/rallish doctor

# List built-in adapters and presets
./dist/rallish add --list

# Headless preset session (auto-spawns the daemon)
./dist/rallish squash \
  --preset pair-review \
  --task "Add OAuth2 support" \
  --repo ./my-project

# Smoke test without real adapters (fake/deterministic, 3 turns)
cat > ~/.rallish/presets/fake-demo.yaml <<'EOF'
name: fake-demo
roles:
  - {id: ralph, runtime: fake, model: fake-1}
routing: round_robin
budget: {max_turns: 3, max_tokens: 10000, deadline_minutes: 5}
exit_when: [turns_exhausted]
scratch: {max_kb: 16}
EOF
./dist/rallish squash --preset fake-demo --task "smoke test" --repo /tmp

# Two-terminal rally (live baton-passing between human sessions)
# Terminal A — create session and join as alice:
SESSION=$(./dist/rallish rally new --participants alice,bob --task "ping pong")
./dist/rallish rally join --session-id $SESSION --as alice   # blocks; alice gets first baton

# Terminal B — join as bob (blocks until alice passes):
./dist/rallish rally join --session-id $SESSION --as bob

# Any terminal — pass the baton:
./dist/rallish rally done --session-id $SESSION --as alice --note "draft v1"

# Check session state:
./dist/rallish rally status --session-id $SESSION

# A2A discovery (external clients use TCP loopback)
curl http://127.0.0.1:$(cat ~/.rallish/port)/.well-known/agent.json

# A2A send task
curl -X POST http://127.0.0.1:$(cat ~/.rallish/port)/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"parts":[{"text":"Hello"}]}}}'
```

Per-turn requests and responses land in `~/.rallish/sessions/<id>/log.jsonl`.

## Usage

### 1. Start a headless session

```bash
rallish squash --preset <name> --task "<description>" --repo <path>
```

Presets live in `internal/preset/presets/` (built-ins) or `~/.rallish/presets/` (custom). See [docs/handbook.md](docs/handbook.md) for preset authoring.

### 1b. Start an interactive rally session

```bash
# Create session; prints session ID
rallish rally new --participants <name1>,<name2> [--task "<description>"]

# Each participant joins in their own terminal (blocks waiting for the baton)
rallish rally join --session-id <id> --as <name>

# Pass the baton when done
rallish rally done --session-id <id> --as <name> [--note "<summary>"]

# Check status at any time
rallish rally status --session-id <id>
```

See [docs/runbook-rally-mode.md](docs/runbook-rally-mode.md) for the full two-terminal walkthrough.

### 2. A2A integration

Any A2A-compliant client can discover and send tasks:

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/.well-known/agent.json` | Agent Card |
| `POST` | `/a2a` | JSON-RPC 2.0 (tasks/send, tasks/get, tasks/cancel, tasks/sendSubscribe) |

See [docs/a2a-compatibility.md](docs/a2a-compatibility.md) for the full mapping.

### 3. Same-type pairing

You can pair two Claude instances, two Kimi instances, or any mix. The broker only cares about turn order, not vendor identity.

### 4. Check budget status

Budgets (tokens, turns, deadline) are enforced per session. When exhausted, the broker returns `410 Gone` and preserves the scratchpad for resume.

### 5. Custom presets

Drop a YAML file in `~/.rallish/presets/<name>.yaml`:

```yaml
name: my-preset
description: Optional one-line summary.
roles:
  - id: planner
    runtime: claude
    model: opus
  - id: executor
    runtime: kimi
    model: kimi-k2
routing: handoff_then_round_robin    # or round_robin
budget:
  max_turns: 20
  max_tokens: 400000
  deadline_minutes: 60
exit_when: [tests_pass, reviewer_approved, turns_exhausted]
scratch:
  max_kb: 64
  summarize_with: claude-haiku
```

### 6. Daemon lifecycle

```bash
rallish daemon &                            # explicit start (optional — squash auto-spawns)
ls ~/.rallish/                              # rallish.sock (0600), socket, port, sessions/
kill -TERM $(pgrep -f "rallish daemon")     # graceful shutdown removes all three files
```

`rallish doctor` confirms reachability:

```
daemon reachable via unix socket path=/Users/<you>/.rallish/rallish.sock perm=-rw-------
```

On Windows the broker falls back to TCP loopback only (Unix socket stub returns `ErrUnsupported`).

## Security

- Broker binds to `127.0.0.1` only.
- No auth layer in v0; use a reverse proxy or local firewall for shared machines.
- Preset files are validated for path traversal before loading.
- Secrets in env vars are redacted from logs.

See [DESIGN.md](DESIGN.md) §14 for the full threat model.

## Development

Enable the repo's pre-commit hook once per clone:

```bash
make setup-hooks
```

Run the full check suite:

```bash
make check   # go vet + golangci-lint + go test -race
```

### Test suite

```bash
make test    # go test ./...
make bench   # go test -bench=. -benchmem ./...
```

Coverage floor: ≥70% on `internal/session`, `internal/router`, `internal/exit`, `internal/preset`, `pkg/contract`.

## Conventions

See [AGENTS.md](AGENTS.md) for coding guidelines, project layout, and commit conventions.

## License

MIT — See [LICENSE](./LICENSE).

> *"Like a rally, the best turns happen when no one tries to hold the ball."*
