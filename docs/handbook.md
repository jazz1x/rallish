# rallish Handbook

> For operators and integrators.

## Table of Contents

1. [Installation](#installation)
2. [Configuration](#configuration)
3. [Presets](#presets)
4. [A2A Integration](#a2a-integration)
5. [Security](#security)
6. [Troubleshooting](#troubleshooting)

## Installation

### From Source

```bash
git clone https://github.com/jazz1x/rallish.git
cd rallish
make build
sudo cp dist/rallish /usr/local/bin/
```

### From Homebrew (after first release)

```bash
brew tap jazz1x/rallish
brew install rallish
```

### Prerequisites

- Go 1.25+
- `claude` or `kimi` CLI with a valid API key

## Configuration

rallish stores runtime state in `~/.rallish/`:

| File / Dir | Purpose |
|------------|---------|
| `port` | Auto-detected free port |
| `sessions/` | Session JSON dumps |
| `presets/` | Custom preset overrides |

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `RALLISH_PORT` | auto | Broker HTTP port |
| `RALLISH_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

## Presets

Presets are YAML files that define:

- `roles`: name → adapter mapping
- `routing`: who runs after whom
- `exit`: conditions for stopping a session

Example:

```yaml
name: pair-review
roles:
  planner:
    adapter: claude
    model: claude-3-5-sonnet-20241022
  executor:
    adapter: claude
    model: claude-3-5-sonnet-20241022
  reviewer:
    adapter: kimi
    model: kimi-moonshot-v1-32k
routing:
  - planner
  - executor
  - reviewer
exit:
  maxTurns: 10
  deadlineMinutes: 30
```

## A2A Integration

rallish exposes two A2A endpoints:

- `GET /.well-known/agent.json` — Agent Card
- `POST /a2a` — JSON-RPC 2.0 task methods

Supported methods:

| Method | Description |
|--------|-------------|
| `tasks/send` | Send a message, return final task state |
| `tasks/sendSubscribe` | Send a message, stream SSE updates |
| `tasks/get` | Get current task state by ID |
| `tasks/cancel` | Cancel a running task |

See [docs/a2a-compatibility.md](a2a-compatibility.md) for the full mapping.

## Security

- Broker binds to `127.0.0.1` only.
- No auth layer in v0; use a reverse proxy or local firewall for shared machines.
- Preset files are validated for path traversal before loading.
- Secrets in env vars are redacted from logs.

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `bind: address already in use` | Another rallish instance running | `killall rallish` or set `RALLISH_PORT` |
| `adapter not found: claude` | CLI binary not in `$PATH` | Install `claude` CLI and ensure it is on `$PATH` |
| SSE stream hangs | All agents waiting for input | Check `doctor` output for API key issues |
| Budget exceeded early | Token count drift | Verify `model` matches actual model in preset |
