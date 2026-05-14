# rallish

[![version](https://img.shields.io/badge/version-0.0.1-blue)](CHANGELOG.md)
[![license](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![go](https://img.shields.io/badge/go-1.25+-blue)](go.mod)

> *A local broker for multi-agent turn-taking, A2A-compliant.*

[한국어](README.ko.md) · [日本語](README.jp.md)

## What is rallish?

rallish is a small local broker process that sits between N agent runtimes — each of which is an off-the-shelf coding CLI like `claude`, `kimi`, `codex`, etc. The broker owns the conversation state, decides whose turn it is, and shuttles compact turn payloads between them.

The wire format follows the **A2A (Agent2Agent) protocol** where reasonable, so any A2A-compliant agent can be plugged in via an adapter.

## Features

| Feature | Description |
|---------|-------------|
| **Turn-taking** | Agents alternate turns via a shared broker; no direct coupling |
| **A2A Protocol** | `/.well-known/agent.json`, JSON-RPC 2.0 tasks, SSE streaming |
| **Token Budgets** | Hard caps on tokens, turns, and wall-clock time per session |
| **Scratchpad** | Rolling shared scratch with automatic compaction |
| **Presets** | YAML templates for roles, routing, and exit conditions |
| **Security** | Path traversal guards, secret redaction, minimal env allowlists |

## Quick Start

### Prerequisites

- Go 1.25+
- `claude` CLI and/or `kimi` CLI installed and authenticated

### Build

```bash
git clone https://github.com/jazz1x/rallish.git
cd rallish
make build
```

### Run

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

## Presets

Shipped presets live in `internal/preset/presets/`:

| Preset | Roles | Description |
|--------|-------|-------------|
| `solo-ralph` | 1 × claude | Single-agent execution with budget guards |
| `pair-review` | planner, executor, reviewer | Structured review loop |

Custom presets can be dropped in `~/.rallish/presets/<name>.yaml`.

## Architecture

```
┌──────────────────────────────────────────┐
│  rallish broker (Go, 127.0.0.1)         │
│  POST /sessions                          │
│  GET  /sessions/:id/next?as=<role> (SSE) │
│  POST /sessions/:id/turn                 │
│  GET  /.well-known/agent.json            │
│  POST /a2a                               │
└────────▲─────────────────────▲───────────┘
         │                     │
   ┌─────┴──────┐       ┌─────┴──────┐
   │   claude   │       │    kimi    │
   └────────────┘       └────────────┘
```

## Security

See [DESIGN.md](DESIGN.md) §14 and [docs/handbook.md](docs/handbook.md) for the full threat model.

## Contributing

1. `make check` must pass (`go vet`, `golangci-lint`, `go test -race`)
2. Follow conventional commits (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`)
3. Maintain ≥70% test coverage on `internal/session`, `internal/router`, `internal/exit`, `internal/preset`, `pkg/contract`

## License

MIT © jazz1x
