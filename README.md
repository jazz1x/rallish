# rallish

> A local broker for multi-agent turn-taking, A2A-compliant.

![version](https://img.shields.io/badge/version-0.3.0-blue)
![license](https://img.shields.io/badge/license-MIT-green)
![go](https://img.shields.io/badge/go-1.25+-blue)

**rallish** is a small local broker process that sits between N agent runtimes — any coding CLI with an adapter (Claude, Kimi, Cursor, Codex, etc., or even the same kind running in different contexts). The broker owns the conversation state, decides whose turn it is, and shuttles compact turn payloads between them.

Everything runs locally. No cloud broker, no external coordination service. The wire format follows the **A2A (Agent2Agent) protocol** where reasonable, so any A2A-compliant agent can be plugged in via an adapter.

[한국어](./README.ko.md) · [日本語](./README.jp.md)

## Features

| Feature | Description |
|---------|-------------|
| **Squash (headless)** | `rallish squash` runs headless preset sessions (`solo-ralph`, `pair-review`); broker spawns adapters automatically |
| **Rally (interactive)** | `rallish rally` provides live baton-passing between two coding-CLI sessions; agents self-loop the ping-pong (no per-turn user trigger needed); exclusive holder enforcement via SSE |
| **A2A Protocol** | partial A2A v1.0: `/.well-known/agent-card.json` + `protocolVersion`, PascalCase JSON-RPC tasks (strict typed intake), SSE streaming |
| **Token Budgets** | Hard caps on tokens, turns, and wall-clock time per session |
| **Scratchpad** | Rolling shared scratch with automatic compaction |
| **Presets** | YAML templates for roles, routing, and exit conditions |
| **Unix socket IPC** | CLI↔Daemon over `~/.rallish/rallish.sock` (mode `0600`); TCP loopback retained for A2A clients and Windows fallback |
| **Auto-daemon** | `rallish squash` spawns the broker if none is running; `rallish doctor` reports socket reachability |
| **Security** | Path traversal guards, secret redaction, minimal env allowlists |

## Autonomous Work Harness

rallish is a vendor-neutral, repo-local **work harness**: it makes any agent runtime safe, resumable, verifiable, and auditable for long autonomous repository work — without being the loop. Six guardrail pillars:

- **Safety & resumability** — atomic, `.bak`-recovering checkpointed state; `cycle run --once` is the bounded reference driver a cron/scheduler invokes (exit code = halt reason).
- **Verification gates** — parse-don't-validate agent handshake, a gate self-eval, hash-pinned gate definitions.
- **Interop** — A2A **v1.0** (signed Agent Card at `/.well-known/agent-card.json`, real `protocolVersion`, strict typed intake).
- **Audit** — `schema_version`-stamped, hash-chained, replayable ledger with RFC 9162 Merkle inclusion/consistency proofs.
- **Anti-spin** — stuck/budget circuit-breakers + a sticky-halt reviver guard (a cron-revived spinning run self-halts and is not resurrected).
- **Action-gate** — pre-execution destructive-command deny-list + secret containment; rallish declares + records the decision, the runtime hook enforces.

Full direction + rationale: `docs/north-star.md`.

## Architecture

```
┌──────────────────────────────────────────┐
│  rallish broker (Go)                     │
│  POST /sessions                          │
│  GET  /sessions/:id/next?as=<role> (SSE) │
│  POST /sessions/:id/turn                 │
│  GET  /.well-known/agent-card.json       │
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

One command:

```bash
npx skills add jazz1x/rallish
```

That delivers the skill bundle (SKILL.md + bundled binary installer) to
`~/.claude/skills/rallish/`. Resolves via [skills.sh](https://www.skills.sh).

Open any project in Claude Code (or another skill-aware coding CLI) and say
`랠리보낼 준비해` / `let's serve`. On first use the skill self-installs the
`rallish` binary via the bundled platform-detecting script
(`scripts/install-binary.sh` → fetches the latest GitHub Release into
`/usr/local/bin` or `~/.local/bin`).

<details>
<summary><b>Power-user alternatives (skip the bundle)</b></summary>

| Method | Command |
|---|---|
| **Homebrew tap** (macOS) | _coming soon_ — `brew install jazz1x/rallish/rallish` will work once the `jazz1x/homebrew-rallish` tap repo and `TAP_GITHUB_TOKEN` secret are provisioned (see [#issues](https://github.com/jazz1x/rallish/issues)) |
| **curl** (any Unix) | `curl -fsSL https://raw.githubusercontent.com/jazz1x/rallish/main/install.sh \| sh` |
| **From source** | `git clone https://github.com/jazz1x/rallish && cd rallish && make build` |
| **`go install`** | `go install github.com/jazz1x/rallish/cmd/rallish@latest` |

After the binary is on `$PATH`, `rallish bootstrap` (idempotent) installs the
skill bundle and verifies the daemon.
</details>

> ✓ rallish runs once per user (not per project). After the one-time
> install you can rally from any directory — no need to be inside the
> rallish source tree. The daemon is global at `~/.rallish/`. See
> [docs/handbook.md#using-rallish-from-any-project](docs/handbook.md#using-rallish-from-any-project)
> for the project-agnostic workflow.

## Quickstart

```bash
# One-shot setup wizard (installs the skill + asks 3 short questions)
./dist/rallish bootstrap

# Environment check (renders adapters + daemon as a glyph status list)
./dist/rallish doctor

# Inspect or edit settings (~/.rallish/config.yaml)
./dist/rallish config list
./dist/rallish config set wait_mode block
./dist/rallish config edit              # opens $EDITOR

# Interactive component picker (npx-style)
./dist/rallish add

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

# Two-terminal tennis rally (live baton-passing between human sessions)
# Prefer the natural-language UX driven by skills/rallish —
# the agent (Claude Code, Cursor, …) runs all rally commands for you.
# In Terminal A's coding-CLI session you say:    "랠리보낼 준비해 — 사이클로 가자"
# Agent: rally new --first server + role=server, prints SID, serves first turn, yields.
# In Terminal B's coding-CLI session you say:    "랠리받을 준비해 <SID>"
# Agent: parses pattern, role=returner, immediately picks up the baton, yields.
# After each side finishes a turn the agent yields back to the user; on the
# next user message it checks status and continues if it's its turn. No
# per-turn "내 차례" trigger needed.
# Either side, at any time, to stop:             "끝"
#
# Raw CLI surface (used by the skill under the hood, or by scripts):
SESSION=$(./dist/rallish rally new --participants server,returner --task "warm-up rally")
./dist/rallish rally status --session-id $SESSION
./dist/rallish rally done   --session-id $SESSION --as server --note "draft v1"

# Bounded one-shot pass a cron/scheduler drives (exit code = halt reason)
rallish cycle run --once --cycle-id <id>
# Pre-execution policy gate a runtime PreToolUse hook calls (declare + record; the hook enforces)
rallish gate tooluse --command 'rm -rf /'    # -> {"verdict":"deny",...}  exit 13

# A2A discovery (external clients use TCP loopback)
curl http://127.0.0.1:$(cat ~/.rallish/port)/.well-known/agent-card.json

# A2A send task (v1.0 method name; tasks/send still works as a legacy alias)
curl -X POST http://127.0.0.1:$(cat ~/.rallish/port)/a2a \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"parts":[{"text":"Hello"}]}}}'
```

Per-turn requests and responses land in `~/.rallish/sessions/<id>/log.jsonl`.

## CLI surface

`rallish --help` groups commands into four sections. Output uses
PyClack-style glyphs (◇ info, ✓ ok, ⚠ warn, ■ err, ◆ prompt, └ footer)
and auto-falls-back to plain ASCII when `$NO_COLOR` is set or stdout is
not a TTY.

| Group | Commands |
|---|---|
| **Setup** | `bootstrap` (one-shot wizard) · `skill install` |
| **Rally** | `rally` (live baton) · `squash` (headless preset) |
| **Manage** | `add` (interactive picker · `--list` for catalog) · `config` (`list` / `get` / `set` / `path` / `edit`) |
| **System** | `doctor` (status table) · `daemon` · `version` |

`rallish bootstrap` fits on one screen by design — the wizard never
exceeds ~12 lines so coding-CLI session banners (skills discography,
trigger lists) stay visible after the install.

## Usage

### 1. Start a headless session

```bash
rallish squash --preset <name> --task "<description>" --repo <path>
```

Presets live in `internal/preset/presets/` (built-ins) or `~/.rallish/presets/` (custom). See [docs/handbook.md](docs/handbook.md) for preset authoring.

### 1b. Start an interactive rally session

**Agent-driven (recommended).** Open this repo in any coding CLI that
auto-discovers skills (Claude Code, Cursor, …). The
[`skills/rallish`](skills/rallish/SKILL.md) skill loads
on these natural-language triggers:

| You say | The agent does |
|---|---|
| `랠리보낼 준비해` / `let's serve` | `rally new`, takes role=`server`, prints the SID |
| `랠리받을 준비해 <SID>` / `let's return` | `rally status`, takes role=`returner`, waits |
| `시작` / `go` (server side) | serves the first turn, then `rally done` with a summary note |
| `내 차례` / `is it my turn` (receiver side) | `rally status`; if it's your turn, reads the previous note, does the work, runs `rally done` |
| `끝` / `match over` | clean shutdown |

Short triggers like `시작` / `go` / `끝` / `내 차례` only activate after
a prior prep trigger set the role + SID; bare generic words are ignored.

**Raw CLI (for scripts or non-skill-aware clients):**

```bash
rallish rally new    --participants <a>,<b> [--task "<desc>"]
rallish rally join   --session-id <id> --as <name>           # blocks on SSE
rallish rally done   --session-id <id> --as <name> [--note "<s>"] [--handoff-to <n>]
rallish rally status --session-id <id>
```

See [docs/runbook-rally-mode.md](docs/runbook-rally-mode.md) for the full
two-terminal walkthrough.

### 2. A2A integration

Any A2A-compliant client can discover and send tasks:

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/.well-known/agent-card.json` | Agent Card (v1.0; `/.well-known/agent.json` legacy alias) |
| `POST` | `/a2a` | JSON-RPC 2.0 (SendMessage, GetTask, CancelTask, SubscribeToTask; legacy `tasks/*` aliases) |

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

### 6. Autonomous cycle (harness)

`cycle new`, `cycle status`, `cycle halt`, and other `cycle` subcommands route through the broker and **require a running daemon**:

```bash
rallish daemon &                               # must be running first
rallish cycle new --goal "feat: add auth" --branch feat/auth
rallish cycle run --once --cycle-id <id>       # bounded one-shot (cron/scheduler entry point)
rallish cycle status --cycle-id <id>
```

The audit gate runs `make check-all` by default. To use a project-specific command, pass `--audit-cmd` at cycle creation:

```bash
rallish cycle new --goal "fix tests" --branch feat/fix \
  --audit-cmd "npm test"   # or "cargo test", "bun run test", etc.
```

An empty or whitespace-only `--audit-cmd` is a misconfiguration and fails loudly (no silent fallback to the default).

The polish gate runs `go test -race ./...` by default. To use a project-specific test command, pass `--polish-test-cmd` at cycle creation:

```bash
rallish cycle new --goal "fix tests" --branch feat/fix \
  --polish-test-cmd "npm test"   # or "cargo test", "pytest", etc.
```

An empty or whitespace-only `--polish-test-cmd` is a misconfiguration and fails loudly. The `scripts/check-no-raw-ansi.sh` check inside the polish gate is rallish-repo-specific and is silently skipped when the script is not present in the target repository (not applicable, not an error). `cycle run --once` is the daemon-free path: it resumes persisted state directly from `tmp/cycle-<id>.json` without the broker.

### 7. Daemon lifecycle

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
