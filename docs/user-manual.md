# rallish — User Manual

> A task-oriented guide: install → first session → squash/rally → autonomous cycles → configuration → troubleshooting.
> **Version:** tracks `VERSION` (0.3.0) · **Last updated:** 2026-06-24 · [한국어](./user-manual.ko.md)

This manual is for **operators and integrators** running rallish on their own machine. For *why* rallish is shaped the way it is, see `docs/north-star.md`; for *what is and isn't wired*, see `docs/feature-spec.md`.

---

## 1. What rallish is (in one minute)

rallish is a small **local broker** that sits between two or more agent runtimes (the `claude` and `kimi` CLIs ship today) and runs **turn-taking** between them. The broker owns the conversation state and decides whose turn it is. Everything runs on your machine — no cloud coordinator.

You use it in three ways:

- **`squash`** — fire-and-forget a *headless* preset session; the broker spawns the adapters and runs the ping-pong to completion.
- **`rally`** — drive *interactive* baton-passing between live coding-CLI sessions.
- **`cycle`** — run *autonomous*, gated, resumable repository work; the canonical cron/CI entry point is `cycle run --once`.

---

## 2. Requirements

- A supported OS (Linux/macOS; Windows uses the TCP-loopback fallback instead of the Unix socket).
- At least one agent CLI on your `PATH`: the **`claude`** CLI (Claude Code) and/or the **`kimi`** CLI. These must be **authenticated** — rallish runs them as subprocesses but does not manage their credentials.
- For building from source: **Go 1.25+**.

---

## 3. Installation

Pick whichever path works for you. Verify with `rallish version` afterward.

```bash
# From source (Go 1.25+)
git clone https://github.com/jazz1x/rallish.git
cd rallish
make build
sudo install dist/rallish /usr/local/bin/rallish

# go install
go install github.com/jazz1x/rallish/cmd/rallish@latest

# Curl installer (fetches the latest GitHub Release tarball; no Go toolchain needed)
curl -fsSL https://raw.githubusercontent.com/jazz1x/rallish/main/install.sh | sh
```

> **Note on install paths.** GitHub Releases (cosign-signed, with SBOM) are the reliable artifact source. A Homebrew tap is planned but not yet provisioned. If a skills-registry one-liner is referenced elsewhere, prefer the paths above unless that registry is confirmed live.

### Bootstrap (recommended first run)

```bash
rallish bootstrap
```

Idempotent. It installs the bundled skill to `~/.claude/skills/rallish/`, collects a few config values (default preset, coding-CLI vendor, wait mode), writes `~/.rallish/config.yaml`, and checks daemon reachability. Flags: `--yes` (accept defaults), `--skip-skill`, `--skip-config`.

---

## 4. First session in five minutes

### 4.1 Check your environment

```bash
rallish doctor            # presence checks (fast, no turns spent)
rallish doctor --probe    # also verify each adapter is logged in (spends one turn each)
```

`doctor` reports daemon reachability, which adapters are on your `PATH`, and your config/skill paths.

By default `doctor` checks that an adapter binary is *present and executable* — it does not log in. Add `--probe` to verify auth: each adapter on `PATH` answers one minimal real turn, so an unauthenticated or rate-limited CLI shows up as a failed `adapter:*:auth` check instead of surfacing later as a cryptic session error. (The probe is bounded, so a stuck login prompt won't hang the command.)

> Even without `--probe`, a session that fails on an unauthenticated CLI now reports an actionable message ("…runtime is not authenticated — run `claude` once interactively to log in…") rather than the old `parsing response: no JSON TurnResponse found in output`.

### 4.2 Verify the install with zero credentials

Before wiring up a real agent CLI, confirm rallish itself works end-to-end:

```bash
rallish squash --preset fake-demo --task "smoke test"
```

`fake-demo` is a bundled preset whose single role runs the in-process `fake`
runtime — no `claude`/`kimi` CLI, no API key. It auto-spawns the daemon, runs a
handful of turns, and exits `turns_exhausted`. If you see
`Session … completed successfully`, your install and daemon path are healthy.

### 4.3 Run a headless session (squash)

```bash
rallish squash --preset solo-ralph --task "add a --version flag"
```

`--task` is **required** when starting a new session. This auto-spawns the broker (squash is the one command that does), runs the `solo-ralph` preset — a single `claude`/sonnet agent under a 30-turn / 600k-token / 90-minute budget — and exits when an exit condition fires.

To use the three-role planner→executor→reviewer flow:

```bash
rallish squash --preset pair-review --task "add a --version flag"   # needs both `claude` and `kimi` on PATH
```

> **Zero-credential check.** To verify the install without burning credentials, use the bundled `fake-demo` preset (§4.2) — it runs the in-process `fake` runtime, no CLI or API key required.

---

## 5. Interactive baton-passing (rally)

Use `rally` when you want two or more live sessions to hand work back and forth, each driven by its own coding CLI.

> **Prerequisite:** unlike `squash`, the `rally` commands do **not** auto-spawn the broker. Start it first:
> ```bash
> rallish daemon &     # or run it in its own terminal
> ```

**1. Create a session:**

```bash
rallish rally new --participants alice,bob --repo /path/to/repo --task "refactor the parser"
# prints a session ID
```

`--first alice` pre-assigns the baton; otherwise the first-listed participant must `join` to start.

**2. Each participant joins and waits for the baton:**

```bash
rallish rally join --session-id <ID> --as alice
```

This opens a live SSE stream and blocks until the baton arrives, then prints the turn number and instructions. Useful flags:
- `--once` — exit after the first baton (good for scripting one turn).
- `--timeout 5m` — give up after the duration and **exit with code 2** (no baton arrived). Without `--timeout`, `join` blocks indefinitely — always set one if a wrong name could leave you hanging.

**3. Pass the baton on when your turn is done:**

```bash
rallish rally done --session-id <ID> --as alice --note "parser split into lexer+parser" --handoff-to bob
```

If you call `done` when it isn't your turn, you get a clear "not your turn" / "session interrupted" message (HTTP 409, exit 1) — nothing is corrupted.

**4. Check state anytime:**

```bash
rallish rally status --session-id <ID>
```

Shows status, current holder, turn number, task, repo, participants (with stale markers), and the last few turns.

**MCP variant.** `rallish rally mcp-agent --mode create|join|done|status|interrupt …` is a one-shot MCP client that prints raw JSON — handy for wiring rally into an MCP-aware agent. `join` here defaults to a 30 s timeout.

---

## 6. Presets and configuration

### 6.1 Config

Stored at `~/.rallish/config.yaml`. Manage it with:

```bash
rallish config list                 # all keys, current values, and source
rallish config get default_preset
rallish config set default_preset pair-review
rallish config path                 # print the file path
rallish config edit                 # open in $EDITOR (creates if missing)
```

| Key | Default | Meaning |
|-----|---------|---------|
| `default_preset` | `solo-ralph` | preset used by `squash` when `--preset` is omitted |
| `default_adapter` | `claude` | default adapter |
| `coding_cli` | `claude` | vendor: `claude`, `kimi`, or `codex` |
| `wait_mode` | `yield` | `yield` (poll) or `block` (blocking join) |
| `editor` | (empty) | overrides `$VISUAL` / `$EDITOR` for `config edit` |
| `telemetry` | `off` | `on` / `off` |

### 6.2 Preset anatomy

A preset is a YAML template. Built-in presets are compiled into the binary; `squash` resolves a preset by name first from the built-ins, then from `~/.rallish/presets/<name>.yaml`. (Note: `squash` looks up your own presets under `~/.rallish/presets/` only — it does **not** read a project-local `./.rallish/presets/` directory, even though `rallish add` can write there.)

```yaml
name: solo-ralph
description: Single runtime running the ralph loop with budget and exit guards.
roles:
  - id: ralph
    runtime: claude        # claude | kimi | fake
    model: sonnet
routing: round_robin       # round_robin | handoff_then_round_robin
                           # (strict_round_robin / last_writer_wins are not yet implemented)
budget:
  max_turns: 30            # required, > 0
  max_tokens: 600000       # required, > 0
  deadline_minutes: 90
exit_when:
  - tests_pass
  - turns_exhausted
  - deadline_passed
scratch:
  max_kb: 64
  summarize_with: claude-haiku
```

**Routing.** Only `round_robin` and `handoff_then_round_robin` work today; the other two names parse but fail at runtime. An explicit `handoff_to` in a turn always wins; a `blocked` self-eval escalates to a `reviewer` role if one exists.

**Exit conditions.** `turns_exhausted`, `tokens_exhausted`, `deadline_passed`, and `reviewer_approved` work on the squash/broker path. **`tests_pass` / `all_artifacts_compile` do NOT terminate a broker-driven squash** — the broker deliberately skips shell predicates (it would run them in the daemon's directory, not your repo). Those shell checks *do* run inside the autonomous cycle's gate pipeline. Plan your `exit_when` accordingly: for `squash`, rely on `reviewer_approved` / `turns_exhausted` / `deadline_passed`.

**The zero-credential preset ships built in** as `fake-demo` — no file to write:

```bash
rallish squash --preset fake-demo --task "smoke test"     # completes without any API credentials
```

Its definition (a single role on the in-process `fake` runtime) is a good
template if you want to build your own credential-free preset:

```yaml
name: fake-demo
description: Zero-credential smoke test using the in-process fake runtime.
roles:
  - id: demo
    runtime: fake
    model: none
routing: round_robin
budget: { max_turns: 5, max_tokens: 100000, deadline_minutes: 5 }
exit_when: [turns_exhausted, reviewer_approved, deadline_passed]
```

### 6.3 Adding components

```bash
rallish add --list                          # see built-in adapters/presets/skills
rallish add preset pair-review              # install a built-in preset locally
rallish add preset my-preset --from <URL>   # download one
rallish add adapter <name> --global         # install to ~/.rallish instead of ./.rallish
```

---

## 7. Autonomous cycles

A **cycle** is bounded, gated, resumable repository work. Use it when you want an agent to make real commits under guardrails.

### 7.1 Create and run

```bash
# Create a cycle (does not run it)
rallish cycle new --goal "add retry to the HTTP client" --branch feat/retry

# One-shot: create, optionally orchestrate agents, and watch until halt
rallish cycle start --goal "add retry to the HTTP client" --agents claude

# Reference driver for cron/CI: run exactly one bounded pass, then exit
rallish cycle run --once --cycle-id <ID> --agents claude,kimi
```

Useful `cycle new` / `start` flags (note the defaults differ by command): `--max-cycles` (`cycle new` default **10**; `cycle start` default **0** = unlimited), `--max-duration` minutes (`cycle new` default **0** = unlimited; `cycle start` default **240** = 4 h), `--auto-goal` discover the next goal after each cycle (`cycle new` default **false**; `cycle start` default **true**), and `--local-gate` (repeatable extra command). **`cycle new` only** (not registered on `cycle start`): `--audit-cmd` (override the audit gate, default `make check-all`; e.g. `npm test`) and `--polish-test-cmd` (override the polish test, default `go test -race ./...`).

### 7.2 What a cycle pass does

Each pass runs the **gate pipeline**: `Preflight → Audit → [your local gates] → Philosophy → Polish → Commit`. The cycle **halts** rather than commits if a gate fails. Highlights:
- **Preflight** refuses to run on `main`/`master`, requires a clean tree and a non-empty goal.
- **Commit** never uses `--amend` or `--no-verify`.
- A **stuck breaker** halts loops that repeat turns, repeat gate failures, ping-pong, or make no progress.
- Halts are **sticky** — a cron-revived spinning run self-halts and is not resurrected.

### 7.3 Exit codes (for cron/CI)

`cycle run --once` encodes the halt reason in its exit code: `0` clean pass, `10` stuck, `11` budget-exceeded, `12` preflight-failed, `13` gate-failure, `14` unparseable-turn, `15` user-requested, `16` self-audit-violation, `17` ssh-auth-failed, `18` max-cycles-reached, `19` unknown, `1` operational error. Branch your scheduler on these.

### 7.4 Inspect and control

```bash
rallish cycle status  --cycle-id <ID>     # phase, counts, branch, goal, halted?, violations
rallish cycle ledger  --cycle-id <ID>     # the append-only hash-chained audit trail (JSON)
rallish cycle watch   --cycle-id <ID>     # live SSE event stream (exits when halted)
rallish cycle next    --cycle-id <ID>     # advance one step (manual debug)
rallish cycle halt    --cycle-id <ID> --reason "stopping"
```

State and ledger live at `~/.rallish/cycles/cycle-<ID>.json` and `cycle-<ID>-ledger.jsonl`.

---

## 8. The action gate (optional safety hook)

rallish can **decide** whether a pending command is dangerous, but it does **not** execute or block anything itself — a runtime PreToolUse hook enforces the decision via the exit code.

```bash
rallish gate tooluse --command "rm -rf /"      # exits 13 (deny)
rallish gate tooluse --command "ls -la"        # exits 0  (allow)
```

Exit codes: `0` allow, `13` deny, `14` needs-human-in-the-loop. Wire it into your agent's PreToolUse hook so the hook refuses (13) or escalates (14). Without that wiring, the gate only *reports* — it cannot stop a command. Blocking decisions can be recorded to a cycle's ledger with `--cycle-id`.

The deny-list covers `rm -rf` of root/home, fork bombs, disk-overwrite (`dd`/`mkfs` to devices), and force-push to protected branches; `git reset --hard origin/…` and `DROP/TRUNCATE TABLE` escalate to human review.

---

## 9. Running the daemon

```bash
rallish daemon          # foreground; binds 127.0.0.1:<dynamic>, creates the Unix socket
```

The daemon writes its port to `~/.rallish/port` and a socket pointer to `~/.rallish/socket` (socket mode `0600`). It refuses to start if a live daemon already owns the socket, and cleans up stale sockets from non-graceful deaths on startup. It handles SIGINT/SIGTERM gracefully (closes active rally sessions, then shuts down). The CLI prefers the Unix socket and falls back to TCP loopback automatically.

---

## 10. Troubleshooting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| `parsing response: no JSON TurnResponse found in output` | adapter CLI not authenticated / rate-limited | run the CLI by hand (`claude -p hi`); log in; retry |
| `rally new` errors about no daemon | `rally`/`cycle` don't auto-spawn | start `rallish daemon &` first |
| `rally join` hangs forever | wrong participant name + no timeout | always pass `--timeout`; check `rally status` for valid names |
| `rally done` → "not your turn" (409) | you don't hold the baton | check `rally status`; wait for your turn |
| `squash` exit conditions never fire on `tests_pass` | broker skips shell predicates by design | use `reviewer_approved` / `turns_exhausted` / `deadline_passed` for squash; shell checks run in cycle gates |
| cycle won't start | on `main`/`master`, dirty tree, or empty goal | switch to a feature branch, commit/stash, set `--goal` |
| cycle exits 12 immediately | preflight failed | see above; check `cycle status` |
| adapter "not found on PATH" in `doctor` | CLI not installed/on PATH | install the `claude` or `kimi` CLI |
| can't reach daemon over socket | stale socket pointer | the CLI auto-cleans and falls back to TCP; or remove `~/.rallish/socket` and restart the daemon |

**Where things live:** config `~/.rallish/config.yaml`; daemon port/socket `~/.rallish/port`, `~/.rallish/socket`, `~/.rallish/rallish.sock`; sessions `~/.rallish/sessions/`; cycles `~/.rallish/cycles/`; skills `~/.claude/skills/rallish/`.

**Getting more detail:** `rallish doctor` for a health snapshot; `rallish cycle ledger --cycle-id <ID>` for the full audit trail of an autonomous run; `rallish <command> --help` for the authoritative flag list.

---

## 11. Command quick reference

```
rallish bootstrap                 one-step setup (skill + config + daemon check)
rallish doctor                    diagnose adapters, daemon, config
rallish version                   build version / commit / date

rallish squash --preset <name> --task <desc>   headless preset session (--task required; auto-spawns daemon)

rallish daemon                    run the broker (required for rally/cycle)
rallish rally new|join|done|status|mcp-agent     interactive baton-passing

rallish cycle new|start|run --once|status|ledger|watch|next|halt   autonomous work
rallish gate tooluse --command <cmd>             pre-exec policy decision (hook enforces)
rallish trigger <phrase>                         run a skill by natural-language phrase (e.g. autonomous-cycle)

rallish config list|get|set|path|edit           configuration
rallish add [adapter|preset|skill] <name>        install components
rallish skill install --name <skill>             install a bundled skill
```

Run any command with `--help` for its full, authoritative flag list.
