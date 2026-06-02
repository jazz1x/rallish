# rallish Handbook

> For operators and integrators.

## Table of Contents

1. [Installation](#installation)
2. [Configuration](#configuration)
3. [Presets](#presets)
4. [A2A Integration](#a2a-integration)
5. [Rally vs Squash](#rally-vs-squash)
6. [Using rallish from any project](#using-rallish-from-any-project)
7. [Security](#security)
8. [Troubleshooting](#troubleshooting)

## Installation

One command via [skills.sh](https://www.skills.sh):

```bash
npx skills add jazz1x/rallish
```

That installs the `rallish` skill (SKILL.md + bundled
`scripts/install-binary.sh`). Open any project in Claude Code (or
another skill-aware coding CLI) and say `랠리보낼 준비해` /
`let's serve`; on first trigger the agent self-installs the `rallish`
binary via the bundled script.

<details>
<summary><b>Alternative install paths</b></summary>

```bash
# Curl (no toolchain required, fetches the latest GitHub Release tarball)
curl -fsSL https://raw.githubusercontent.com/jazz1x/rallish/main/install.sh | sh

# From source (Go 1.25+)
git clone https://github.com/jazz1x/rallish.git
cd rallish
make build
sudo install dist/rallish /usr/local/bin/rallish

# go install
go install github.com/jazz1x/rallish/cmd/rallish@latest

# Homebrew tap — coming soon (needs jazz1x/homebrew-rallish + TAP_GITHUB_TOKEN)
# brew install jazz1x/rallish/rallish
```

</details>

### Bootstrap

After the binary is on `$PATH`:

```bash
rallish bootstrap
```

Idempotent. Materializes the skill at `~/.claude/skills/rallish/`
(if not already there) and verifies the daemon is reachable.

### Prerequisites

- macOS or Linux (Windows uses a TCP-fallback build; some IPC features
  are no-ops on Windows).
- `claude` or `kimi` CLI on `$PATH` with a valid API key, for squash
  sessions. (Rally mode only routes between user-driven sessions, so it
  doesn't strictly need adapters in `$PATH`.)

## Configuration

rallish stores runtime state in `~/.rallish/`:

| File / Dir | Purpose |
|---|---|
| `rallish.sock` | Unix domain socket; mode `0600`; primary CLI↔Daemon transport |
| `socket` | Plain-text pointer file holding the socket path (`~/.rallish/rallish.sock`) |
| `port` | TCP loopback port for A2A clients and Windows fallback |
| `sessions/<id>/log.jsonl` | Append-only per-turn req/resp audit log |
| `sessions/<id>/scratch/` | Rolling scratchpad (auto-compacted when `max_kb` exceeded) |
| `presets/*.yaml` | User-supplied preset overrides (built-ins live in `internal/preset/presets/`) |

The daemon removes `rallish.sock`, `socket`, and `port` on SIGTERM. If
it was killed `-9`, run `rm -f ~/.rallish/{rallish.sock,socket,port}`
before relaunching to clear the stale files.

### Single-instance daemon

rallish enforces a single daemon per user. When you run `rallish daemon`
and a daemon is already bound to `~/.rallish/rallish.sock`, the second
invocation exits immediately with:

```
rallish daemon already running at /Users/<you>/.rallish/rallish.sock — not starting a second instance
```

This prevents the silent "orphan" bug where a second daemon would
unlink the live socket file and leave the first daemon stranded. If the
error appears unexpectedly (e.g. after a crash where the socket file
was not cleaned up), recover with:

```bash
kill -TERM $(pgrep -f "rallish daemon")
# wait a moment for the socket file to be removed, then:
rallish daemon &
```

### Skill discovery

`~/.claude/skills/rallish/` (installed by `rallish bootstrap`
or `npx skills add jazz1x/rallish`) contains:

```
SKILL.md
SKILL.ko.md
scripts/install-binary.sh
```

Any skill-aware coding CLI scans this path on session start.

## Presets

Built-in presets live at `internal/preset/presets/` and are embedded
into the binary. Custom presets go at `~/.rallish/presets/<name>.yaml`.

Schema (matches the loader at `internal/preset/preset.go`):

```yaml
name: pair-review
description: Planner → executor → reviewer rotation with budget gates.
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
routing: handoff_then_round_robin    # or round_robin
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

Field reference:

| Field | Required | Notes |
|---|---|---|
| `name` | ✓ | Filename stem must match |
| `description` | — | One-line summary; appears in `rallish add --list` |
| `roles` | ✓ | List of `{id, runtime, model}` |
| `routing` | ✓ | `round_robin` or `handoff_then_round_robin` |
| `budget.max_turns` | ✓ | Hard cap on turn count |
| `budget.max_tokens` | ✓ | Hard cap on cumulative tokens (broker enforces) |
| `budget.deadline_minutes` | ✓ | Wall-clock cap from session creation |
| `exit_when` | ✓ | List of exit predicates (see `internal/exit/`) |
| `scratch.max_kb` | ✓ | Rolling scratch budget; triggers auto-compaction |
| `scratch.summarize_with` | — | Adapter+model used for compaction summaries |

## A2A Integration

rallish exposes (partial A2A v1.0 — signed cards / mutual auth deferred):

- `GET /.well-known/agent-card.json` — Agent Card (v1.0 path; `/.well-known/agent.json` is a legacy alias)
- `POST /a2a` — JSON-RPC 2.0 task methods (strict typed intake; unknown fields rejected)

Supported methods (v1.0 name; legacy alias in parentheses):

| Method | Description |
|---|---|
| `SendMessage` (`tasks/send`) | Send a message, return final task state |
| `SubscribeToTask` (`tasks/sendSubscribe`) | Send a message, stream SSE updates |
| `GetTask` (`tasks/get`) | Get current task state by ID |
| `CancelTask` (`tasks/cancel`) | Cancel a running task |

A2A clients reach the broker via TCP loopback at the port written to
`~/.rallish/port`. The Unix socket is reserved for the rallish CLI
itself; external A2A clients use TCP.

See [docs/a2a-compatibility.md](a2a-compatibility.md) for the field-by-field
JSON-RPC mapping.

## Rally vs Squash

| Mode | When to use | Command |
|---|---|---|
| **squash** | Headless, agent-driven — let rallish spawn adapters and drive a preset to completion | `rallish squash --preset solo-ralph --task "fix the flaky test"` |
| **rally** | Two human-launched CLI sessions take turns; rallish only carries the baton | The skill drives this. Say `랠리보낼 준비해` in one terminal, `랠리받을 준비해 <SID>` in another. |

See [docs/runbook-rally-mode.md](runbook-rally-mode.md) for a full
two-terminal walkthrough.

### Rally patterns

Rally sessions can adopt one of three behavioural patterns layered on
top of the baton primitive:

- **cycle** — planner ↔ executor; structured slice-by-slice work with
  `[plan]` / `[result]` / `[review]` note conventions.
- **discuss** — peer ↔ peer; design debate that converges on mutual
  `[agree]`.
- **help** — owner ↔ helper; short asymmetric exchange when the owner
  is stuck. Helper refuses to take more than ~3 consecutive `[hint]`
  turns without a `[try]` from the owner.

The pattern is selected at server-prep time by appending a cue to the
trigger (e.g. `랠리보낼 준비해 — 사이클로 가자`). See
[docs/runbook-rally-mode.md#rally-patterns](runbook-rally-mode.md#rally-patterns)
for full walkthroughs and
[docs/prd-rally-patterns.md](prd-rally-patterns.md) for the design
rationale.

**Cross-vendor:** the `rallish` skill is auto-discovered by any
skill-aware coding CLI — Claude Code, Kimi, Codex, Cursor, etc. — via
the brand-group convention at `~/.claude/skills/`. No per-vendor setup
is needed. Live validation: a discuss-pattern rally between Claude Code
and Kimi reached mutual `[agree]` in 4 turns.

**Autoflow (v0.2.0+):** by default the `rallish` skill drives both
sides autonomously after a single setup trigger per side. The skill uses a
yield-first design: on entry it emits a status poll and yields back to the
user rather than holding the agent in a long wait. This eliminates idle token
spend on the waiting side while the active side works. The baton ping-pongs
without user intervention between turns; the loop exits on pattern-specific
signals (mutual `[agree]`, final `[review] approved`, or `[resolved]`) or
the user typing `끝`. See
[docs/runbook-rally-mode.md#autoflow-v020](runbook-rally-mode.md#autoflow-v020)
and [docs/prd-rally-autoflow.md](prd-rally-autoflow.md). For cases where both
sides are known-ready and you prefer a blocking wait, set `WAIT_MODE=block` in
the skill invocation.

### Autonomous harness commands

rallish also ships a repo-local work harness for long autonomous runs (G1–G6 guardrails: safety, verification gates, A2A interop, audit ledger, anti-spin, action-gate).

**Daemon requirement:** `cycle new`, `cycle status`, `cycle halt`, and the SSE-watch commands route through the broker. **The daemon must be running** before invoking these:

```bash
rallish daemon &                              # start the daemon first
rallish cycle new --goal "fix tests" \
  --branch feat/fix \
  --audit-cmd "npm test"                      # optional: override the default 'make check-all'
rallish cycle status --cycle-id <id>
rallish cycle halt   --cycle-id <id>
```

`cycle run --once` is the exception — it resumes state directly from `~/.rallish/cycles/cycle-<id>.json` (the SAME location the daemon writes to; override via `--state-dir`, default `~/.rallish/cycles`) and does **not** require the daemon. That path is outside any worked-on repo, so the broker never dirties the working tree PreflightGate requires clean.

**Audit gate:** the built-in audit gate runs `make check-all` by default. Every repo that uses the cycle harness must expose a `make check-all` target, or override it at cycle creation time with `--audit-cmd`:

| Ecosystem | Example `--audit-cmd` |
|---|---|
| Node / npm | `npm test` |
| Node / bun | `bun run test` |
| Rust | `cargo test` |
| Python | `pytest` |
| Make (custom target) | `make my-check` |

An empty or whitespace-only `--audit-cmd` is a misconfiguration: the gate fails loudly and does not silently revert to the default.

**Polish gate:** the built-in polish gate runs `go test -race ./...` by default. Override with `--polish-test-cmd` at cycle creation:

| Ecosystem | Example `--polish-test-cmd` |
|---|---|
| Node / npm | `npm test` |
| Rust | `cargo test` |
| Python | `pytest` |
| Make | `make test` |

An empty or whitespace-only `--polish-test-cmd` is a misconfiguration (fails loudly). The `scripts/check-no-raw-ansi.sh` check is rallish-repo-specific and is silently skipped when the script is absent in the target repo (not applicable, not an error).

```bash
# Bounded one-shot pass a cron/scheduler drives (exit code = halt reason)
rallish cycle run --once --cycle-id <id>
# Pre-execution policy gate a runtime PreToolUse hook calls (declare + record; the hook enforces)
rallish gate tooluse --command 'rm -rf /'    # -> {"verdict":"deny",...}  exit 13
```

Full direction + rationale: `docs/north-star.md`.

## Using rallish from any project

rallish is installed once, globally. Nothing in the skill bundle,
daemon, or binary is tied to the rallish source tree. After the
one-time install (`npx skills add jazz1x/rallish` + first-trigger
self-install of the binary) you can rally from any directory on your
machine:

```bash
# In a totally unrelated project
cd /any/project/dir
# Open Claude Code (or Kimi, Cursor, Codex …) and say:
랠리보낼 준비해
```

The agent discovers the skill from `~/.claude/skills/rallish/`
regardless of cwd, and the daemon binds globally at `~/.rallish/`. The
`--repo` flag passed in `rallish squash` commands is session metadata
only — it scopes the broker's working-directory context for that
session, but does not change where the skill or daemon live.

```bash
# Cross-project squash example
rallish squash \
  --preset pair-review \
  --task "Audit the auth module" \
  --repo /work/other-company/backend
```

See [Installation](#installation) for the one-time setup.

## Security

- Daemon binds Unix socket at `~/.rallish/rallish.sock` (mode `0600`)
  and TCP at `127.0.0.1:<port>`. Neither is exposed beyond the local
  machine.
- No auth layer in v0.x. On shared machines, use OS-level user
  isolation or a reverse proxy.
- Socket-pointer file (`~/.rallish/socket`) is validated via
  `internal/safepath.UnderRoot` before any FS touch, so a tampered
  pointer cannot redirect to arbitrary paths.
- `--repo` paths flow through `internal/safepath.Clean` and an explicit
  `os.Stat` directory check before reaching the broker.
- Preset files are validated for path traversal before loading.
- `forbidigo` lint rules ban `os.Environ()` and `exec.Command("sh"…)`
  in library code (see DESIGN.md §14).
- `govulncheck` runs on every push/PR.
- Release artifacts carry SBOMs (SPDX-JSON, via syft) and cosign
  keyless signatures.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `daemon not running` from `rallish doctor` | First run | The next CLI command auto-spawns the daemon. Or `rallish daemon &` explicitly. |
| `🎾 your turn` cue never arrives in rally | Other participant hasn't joined, or you're not the current holder | `rallish rally status --session-id <id>` to see the current holder. |
| `not your turn (holder: <name>)` on `rally done` | Stale state or wrong `--as` | Confirm with `rally status`; retry with the correct holder name. |
| Stale socket files after crash | Daemon killed `-9` (not `-TERM`) | `rm -f ~/.rallish/{rallish.sock,socket,port}` then relaunch. |
| `bind: address already in use` | Another rallish daemon running | `pkill -TERM -f "rallish daemon"` |
| daemon refuses to start with "already running" | a previous daemon is alive on the same socket | kill it with `kill -TERM $(pgrep -f "rallish daemon")` then retry |
| `adapter not found: claude` | CLI binary not on `$PATH` | Install the `claude` CLI and ensure `which claude` succeeds. |
| Budget exceeded early | Token-count drift between adapter & broker | Verify `model:` in your preset matches the model the adapter actually invokes. |
| `Error: Skill not found: rallish` in Claude Code | Skill bundle not installed globally | `npx skills add jazz1x/rallish` or `rallish skill install` |
| `rally join` exits with code 2 | `--timeout` fired (default 5 m of no baton) | Normal — the auto-loop will checkpoint to user; either wait, or type `끝`. |
