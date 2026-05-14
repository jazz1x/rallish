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
| **Turn-taking** | Agents alternate turns via a shared broker; no direct coupling |
| **A2A Protocol** | `/.well-known/agent.json`, JSON-RPC 2.0 tasks, SSE streaming |
| **Token Budgets** | Hard caps on tokens, turns, and wall-clock time per session |
| **Scratchpad** | Rolling shared scratch with automatic compaction |
| **Presets** | YAML templates for roles, routing, and exit conditions |
| **Security** | Path traversal guards, secret redaction, minimal env allowlists |

## Architecture

```
┌──────────────────────────────────────────┐
│  rallish broker (Go, 127.0.0.1)          │
│  POST /sessions                          │
│  GET  /sessions/:id/next?as=<role> (SSE) │
│  POST /sessions/:id/turn                 │
│  GET  /.well-known/agent.json            │
│  POST /a2a                               │
└────────▲─────────────────────▲───────────┘
         │                     │
   ┌─────┴──────┐       ┌─────┴──────┐
   │  agent A   │       │  agent B   │
   └────────────┘       └────────────┘
```

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
# Environment check
./dist/rallish doctor

# Start a turn-taking session
./dist/rallish start \
  --preset pair-review \
  --task "Add OAuth2 support" \
  --repo ./my-project

# A2A discovery
curl http://127.0.0.1:$(cat ~/.rallish/port)/.well-known/agent.json

# A2A send task
curl -X POST http://127.0.0.1:$(cat ~/.rallish/port)/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tasks/send","params":{"message":{"parts":[{"text":"Hello"}]}}}'
```

## Usage

### 1. Start a session

```bash
rallish start --preset <name> --task "<description>" --repo <path>
```

Presets live in `internal/preset/presets/` (built-ins) or `~/.rallish/presets/` (custom). See [docs/handbook.md](docs/handbook.md) for preset authoring.

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

### 4. Custom presets

Drop a YAML file in `~/.rallish/presets/<name>.yaml`:

```yaml
name: my-preset
roles:
  planner:
    adapter: claude
    model: claude-3-5-sonnet-20241022
routing:
  - planner
exit:
  maxTurns: 10
```

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
