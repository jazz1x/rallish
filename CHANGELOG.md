# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`rallish cycle` — autonomous-cycle subsystem.** One-shot `cycle start`
  (create → orchestrate → watch) with `--max-cycles`, `--max-duration`,
  `--auto-goal`, and `--log-file`. `cycle status` shows agent-rotation
  hint every 3 cycles. `cycle halt` / `cycle next` / `cycle watch` for
  interactive control.
- **`rallish trigger` — natural-language skill invocation.**
  `rallish trigger "자율 사이클"` matches embedded skill triggers and
  auto-invokes `cycle start` with SKILL.md defaults (max-cycles=10,
  max-duration=240, auto-goal, log-file). `--dry-run` prints the
  equivalent command without executing it.
- **Companion files for autonomous-cycle.** `skill install --name
  autonomous-cycle` writes the skill to `~/.claude/skills/autonomous-cycle`
  plus driver script (`~/.claude/scripts/autonomous-cycle.sh`) and handoff
  runbook (`~/.claude/runbooks/cycle-handoff.md`). Bootstrap step 1
  installs them automatically.
- **AutoGoal + time-based termination.** `autogoal.go` discovers next
  goals via `go vet`, `golangci-lint --fast-only`, and TODO/FIXME scan.
  `MaxDurationMinutes` caps runtime; `HaltSuccess` exits cleanly when the
  codebase is clean.
- **Project-specific cycle gates.** `cycle new` / `cycle start` accept
  repeatable `--local-gate "<command>"` checks that run after the built-in
  audit gate, persist in `CycleState.local_gates`, appear in `cycle status`,
  and are included in multi-agent cycle summaries.
- **Autonomous work harness contract.** `pkg/contract.WorkContract` defines
  the vendor-neutral work objective, scope, gates, budget, and orchestration
  shape that adapters can use independently of any specific agent runtime.
- **Harness ledger event contract.** `pkg/contract.HarnessLedgerEntry` defines
  the append-only audit event shape for future cycle ledgers.
- **Cycle ledger file sync.** `internal/cycle.LedgerFileSync` appends and reads
  harness ledger events as JSONL, establishing the storage seam for audit logs.
- **Gate-to-ledger projection.** `contract.NewGateLedgerEntry` maps gate reports
  into append-only pass/fail audit events.
- **Handoff-to-ledger projection.** `contract.NewHandoffLedgerEntry` maps agent
  handoff responses into append-only audit events with `handoff_to` preserved.
- **Orchestrator handoff ledger wiring.** Multi-agent cycles append
  `handoff_created` events when adapters return `handoff_to`.
- **Agent-turn ledger events.** Multi-agent cycles append `agent_turn` events
  for completed adapter turns, separate from optional handoff events.
- **Ledger failure policy.** The harness PRD and runbook now document
  best-effort broker ledger appends versus fail-fast orchestrator turn appends.
- **Cycle ledger readback.** `GET /cycles/{id}/ledger` returns append-only
  harness ledger entries, including halted cycles whose mutable state was
  already removed.
- **Cycle ledger CLI.** `rallish cycle ledger --cycle-id <id>` prints the
  harness ledger as pretty JSON for human review and downstream agents.
- **Cycle status ledger summary.** `rallish cycle status` shows the ledger entry
  count and last event when ledger readback is available.

- **Anti-spin liveness (G5).** A no-progress / spinning run halts itself before
  it bleeds tokens: `cycle.Stuck` detects repeated turns, repeated gate failures,
  ping-pong, and a stalled frontier; a sticky-halt reviver guard refuses to
  resume a halted cycle; and a hard lifetime cost ceiling catches a productive
  runaway summed across revivals.
- **Pre-execution action-gate (G6).** `contract.DecideToolUse` (destructive
  deny-list + secret containment) classifies a pending command and `rallish gate
  tooluse` returns the verdict with a meaningful exit code — rallish declares the
  policy and records the decision, the runtime hook enforces.
- **Tamper-evident, replayable audit (G4).** Ledger entries carry a
  `schema_version` and are hash-chained (`prev_hash`/`hash`); `contract.VerifyChain`
  detects content tampering, `contract.Replay` reconstructs the control graph
  (verify-before-reconstruct), and a CT-style Merkle layer (RFC 9162) adds
  inclusion + consistency proofs.
- **Un-gameable verification gates (G2).** The agent handshake is parsed strictly
  (an unparseable turn halts, never a silent prose fallback); a gate self-eval
  proves the philosophy scanners catch seeded violations; gate definitions are
  hash-pinned so an in-cycle edit is detectable.
- **A2A v1.0 conformance (G3).** The broker serves a signed Agent Card at
  `/.well-known/agent-card.json` with a real `protocolVersion`, PascalCase RPCs
  (`SendMessage`/`GetTask`/`CancelTask`/`SubscribeToTask`), and strict typed
  intake that rejects unknown fields; the legacy path stays for back-compat.
- **Bounded resume reference driver (G1).** `rallish cycle run --once` runs one
  non-watching pass and exits with a code derived from the halt reason — the safe
  invocation a cron / scheduler drives; resume rides the existing atomic state file.
- **Guardrails as invariants.** A CI import-guard test forbids the core from
  importing a loop/scheduler/graph-DB package; `go mod verify` is wired into the
  push gate; AGENTS.md / CLAUDE.md are ingested as a structured convention source.

## [0.3.0] - 2026-05-20

### Added

- **`internal/ui` — CLI presentation SSOT.** Color tokens
  (info / success / warn / error / dim / prompt / accent / heading),
  PyClack glyphs (`◇ ✓ ■ ⚠ ◆ └ • → │`), prompt / confirm / numeric
  select helpers, and a tiny aligned-column Table renderer. Color
  auto-disables under `NO_COLOR`, `TERM=dumb`, or when stdout is not a
  TTY. Pipe / CI output stays plain ASCII (regression-tested).
- **`rallish config` command group.** `list` (table view of all keys
  with current value + source), `get <key>`, `set <key> <value>` (with
  enum validation for `wait_mode`, `telemetry`, `coding_cli`), `path`,
  `edit` (opens `$EDITOR` after lazily seeding `~/.rallish/config.yaml`
  with defaults). Schema lives in `internal/config`.
- **Compact bootstrap wizard.** `rallish bootstrap` is now a 4-step,
  one-screen interactive flow (skill install → config wizard → summary
  → daemon check) that asks for the three most-asked-about settings
  (default preset, coding-CLI vendor, wait mode). `--yes` skips prompts
  for CI; `--skip-skill` / `--skip-config` skip individual phases.
- **`rallish add` interactive picker.** Bare `rallish add` (no args)
  walks the user through type → name → scope, npx-style.
- **Grouped root help.** `rallish --help` now sorts commands under
  Setup / Rally / Manage / System headers; `rallish` (no args) prints
  a compact 4-line hint banner. `--version` (cobra) added.
- **`rallish doctor` view.** New structured `doctor.Inspect()` API
  returns typed `Check` records; the CLI renders them with glyphs.
- **`scripts/check-no-raw-ANSI.sh` guardrail** + lefthook hook — fails
  the commit if a `\x1b[` escape lands outside `internal/ui`.

### Fixed

- **Rally baton-passing hardening.** Fixed a send-on-closed-channel panic
  race between `rally done` and daemon shutdown. `done` on an already
  interrupted session is now rejected instead of resurrecting the session.
  Joining an interrupted session receives the `{"closed":true}` sentinel
  immediately. `handoff_to` naming a non-member is now rejected rather
  than silently falling back to round-robin. Duplicate joins are rejected
  to prevent stream orphaning and cleanup corruption. The CLI now
  distinguishes an interrupted-session 409 from "not your turn".

## [0.2.1] - 2026-05-18

### Changed

- **Skill rename: `rallish-operator` → `rallish`.** The skill bundle's
  identifier, install directory, and frontmatter `name:` field were
  collapsed from `rallish-operator` to plain `rallish` — the
  `-operator` suffix added no signal once the project's vendor-neutral
  identity was settled. Existing installs at
  `~/.claude/skills/rallish-operator/` are not auto-migrated; users
  should run `rm -rf ~/.claude/skills/rallish-operator/ && rallish
  bootstrap` after upgrading. Trigger phrases (`랠리보낼 준비해`,
  `let's serve`, …) are unchanged; agents that resolve the skill by
  frontmatter `name:` or trigger string will continue to work
  immediately after re-bootstrap. The `go:embed` path,
  `defaultSkillTarget()`, and the repo-root `skills/rallish` symlink
  are all aligned.

## [0.2.0] - 2026-05-18

### Added

- **Rally autoflow** — the skill drives both sides of a rally
  autonomously after a single setup trigger per side. Server-side uses
  `rally new --first server` to pre-assign the baton (no SSE
  phantom-join trick required). Receiver-side picks up the baton on
  the first `rally status` poll. Default `WAIT_MODE=yield`: the agent
  yields back to the user after every `rally done`, status-checks on
  the next user message, and continues if it's its turn. Opt-in
  `WAIT_MODE=block` available via `rally join --once --timeout <dur>`
  for known-ready sessions. Pattern-specific exit signals end the
  loop automatically.
- **Cross-vendor compatibility validated**: the rallish-operator skill
  is auto-discovered by Claude Code, Kimi, Codex, Cursor, and any other
  skill-aware CLI via the brand-group path `~/.claude/skills/`. No
  per-vendor configuration is required. Live validation: a
  discuss-pattern rally between Claude Code and Kimi reached mutual
  `[agree]` in 4 turns. Skill body and handbook updated with a
  cross-vendor callout.
- **External-repo usage**: the rallish skill, daemon, and binary are
  all global (`~/.claude/skills/`, `~/.rallish/`, `/usr/local/bin`).
  No source-tree dependency exists after the one-time install. New
  handbook section [Using rallish from any project](docs/handbook.md#using-rallish-from-any-project)
  and README callouts document the project-agnostic workflow. The
  `--repo` flag in `rallish squash` is session metadata only — it does
  not affect where the skill or daemon reside.

### Changed

- **Single-instance daemon protection**: `rallish daemon` now refuses
  to start when another instance is already bound to
  `~/.rallish/rallish.sock`, printing:
  `rallish daemon already running at <path> — not starting a second instance`
  and exiting non-zero. Previously a second invocation would silently
  unlink the live daemon's socket file and orphan it, causing all
  in-flight sessions to lose IPC. Recovery: `kill -TERM $(pgrep -f
  "rallish daemon")` then re-launch.

- Two new CLI affordances unlock the autoflow:
  - `rally new --first <name>` — pre-assigns the baton at create time;
    no SSE phantom-join trick required.
  - `rally join --once [--timeout <dur>]` — exits cleanly after the
    first baton event, with exit code 2 on timeout. Default behaviour
    (block forever, multi-event) preserved when flags absent.
  Backwards-compatible: existing sessions and CLI calls unchanged.

## [0.1.2] - 2026-05-17

### Added

- **Rally patterns** — three behavioural patterns layered on the rally
  primitive: **cycle** (planner ↔ executor with `[plan]`/`[result]`/`[review]`
  notes), **discuss** (peer ↔ peer converging on mutual `[agree]`), and
  **help** (owner ↔ helper short asymmetric exchange with
  `[stuck]`/`[hint]`/`[try]`/`[resolved]`). Pattern is selected at server-prep
  time via natural-language cue (`"사이클로 가자"`, `"논의 랠리"`, `"막혔어
  도와줘"`). No broker / CLI / contract changes; encoded as conventions
  in the rallish-operator skill body (v0.1.0 → v0.2.0). See
  [docs/prd-rally-patterns.md](docs/prd-rally-patterns.md) and
  [docs/runbook-rally-mode.md#rally-patterns](docs/runbook-rally-mode.md#rally-patterns).

## [0.1.1] - 2026-05-17

### Changed

- Disable the `brews:` block in `.goreleaser.yaml` temporarily. The
  Homebrew tap repo (`jazz1x/homebrew-rallish`) and `TAP_GITHUB_TOKEN`
  secret are not yet provisioned; v0.1.0's release pipeline failed at
  the brew publish step. Until the tap is set up, install via the curl
  one-liner, `npx skills add`, or source build. Brew returns in a
  follow-up release.

## [0.1.0] - 2026-05-17

Adds rally mode (live baton-passing between two interactive coding-CLI
sessions), packages the operator playbook as a vendor-neutral skill
bundle, hardens IPC + tag pipeline. Will land as v0.1.0 once tagged.

### Added

- **Rally mode** — live baton-passing primitive (`rally new/join/done/status`).
  - Agent-driven UX: three natural-language triggers (`랠리보낼 준비해` /
    `랠리받을 준비해 <SID>` / `시작` / `내 차례` / `끝`) drive the entire
    session; the agent runs every rallish CLI call.
  - Tennis theme (🎾): `server` / `returner` roles, single baton at a time.
  - Session id format `rly_<unixmillis>_<rand4hex>`; SSE heartbeat (15s);
    stale-participant detection; exclusive-holder enforcement (409).
  - `broker.CloseAllRallies()` broadcasts `{"closed":true}` on SIGTERM so
    SSE clients drain cleanly within the 5s shutdown deadline.
- **`rallish-operator` skill bundle** — vendor-neutral skill at
  `skills/rallish-operator/` (canonical at `internal/skills/rallish-operator/`
  via symlink). One-line install: `npx skills add jazz1x/rallish`.
  - Bundles `scripts/install-binary.sh` — when the agent's first trigger
    finds no `rallish` on PATH it runs the bundled installer (uname →
    GitHub Release tarball → `/usr/local/bin` or `~/.local/bin`).
  - Embedded in the binary via `//go:embed all:rallish-operator`;
    `rallish skill install` and `rallish bootstrap` materialize it.
- **Squash umbrella** — `rallish squash` replaces `rallish start` and
  covers the headless preset orchestrator (`solo-ralph`, `pair-review`).
  No backward-compat alias (per AGENTS.md).
- **Unix domain socket IPC** at `~/.rallish/rallish.sock` (mode `0600`) as
  the primary CLI↔Daemon transport; TCP loopback retained for A2A clients
  and Windows fallback (build-tagged stub returns `ErrUnsupported`).
- **`rallish doctor`** reports daemon reachability over the socket,
  including socket permission check (warns if looser than `0600`).
- **A2A Protocol layer** — `GET /.well-known/agent.json`, `POST /a2a`
  (JSON-RPC 2.0: `tasks/send`, `tasks/get`, `tasks/cancel`,
  `tasks/sendSubscribe` via SSE), `pkg/contract/a2a.go`.
- **Token budget hard enforcement** in broker (`handleNextTurn`).
- **`internal/scratch/scratch.go`** — automatic compaction when `max_kb`
  exceeded; model-hint injection into adapter prompts.
- **`internal/safepath/`** — path-traversal guard for user-supplied paths;
  used by rally `--repo` flag.
- **Release helper** — `make release-{patch,minor,major,dry-run}`. The
  `scripts/release.sh` script bumps `VERSION`, propagates to README
  badges, commits, tags, and pushes; refuses dirty tree / wrong branch /
  unpushed commits / non-monotonic versions / existing-tag collisions
  (local + remote).
- **Lefthook hooks** — `commit-msg` (conventional prefix enforcement),
  `pre-commit` (fmt/vet/test/lint), `pre-push` (build/vet/test).
- **LICENSE** (MIT) — backs the README badge and the goreleaser
  archive `files:` glob.
- **PRD + runbook** for rally mode (`docs/prd-rally-mode.md`,
  `docs/runbook-rally-mode.md`).

### Changed

- `rallish start` removed; existing scripts must migrate to `rallish squash`.
- Runner HTTP client now routes through the socket-aware transport.
  Previously every `next`/`turn` request went through a vanilla
  `http.Client` and silently failed DNS lookups for `http://rallish.local`.
- Daemon cleanup tightened: closes the Unix listener and removes
  socket-pointer + port files even when TCP serve errors.
- AGENTS.md: conventional commits now machine-enforced via `commit-msg`
  hook. Allowed prefixes: `feat fix refactor docs test chore sec ci build
  perf style`. Adds `Feature Documentation Workflow` and project-layout
  rows for new packages.
- README / CHANGELOG / DESIGN.md kept in lockstep across EN / KO / JP.
- Three languages of READMEs reorganized: single `npx skills add
  jazz1x/rallish` headline; power-user install paths collapsed into a
  `<details>` block.

### Fixed

- Runner HTTP client wasn't socket-aware — every poll failed after the
  initial session create (broker rally `data:`-line completions never
  arrived). Fixed via `runner.NewLoopWithClient`.
- `handleRallyBaton` late-join branch did not read the previous note
  from history, so participants who joined after a handoff saw
  `note=""` instead of the handing-off participant's summary.
- Daemon SIGTERM left `rallish.sock`, `socket`, and `port` on disk when
  a TCP serve error occurred before graceful shutdown.
- Unix domain socket was created with the default `0755` permission;
  now explicitly `chmod 0600` after `Listen`.
- Daemon and `doctor` cobra commands had blank `Short` descriptions
  in `--help` output.
- Cobra errors were silenced (`SilenceErrors: true`); validation
  failures like invalid participant names exited 1 with no stderr.
  Main now prints `Error: ...` to stderr before exit.
- `make check` invoked `golangci-lint` from `$PATH` even though the
  repo pins it at `.toolchain/bin/`. Makefile now auto-discovers the
  toolchain binary and prepends its Go runtime to the invocation PATH.

### Security

- Unix socket permissions hardened to `0600` (broker side).
- Socket-pointer file (`~/.rallish/socket`) validated against the
  rallish home root before use in `cli.RunStart` to block traversal
  via tampered pointers.
- `--repo` paths now flow through `internal/safepath.Clean` and an
  explicit `os.Stat` directory check before being shipped to the broker.
- `forbidigo` lint rules ban `os.Environ()` and `exec.Command("sh"…)`
  in library code (DESIGN.md §14).
- `govulncheck` runs on every push/PR (`.github/workflows/ci.yml`).
- `cosign` keyless signing + SBOM via `syft` on every release artifact
  (configured in `.goreleaser.yaml`).

### CI / pipeline

- `release.yml` validates the pushed tag matches `^v[0-9]+\.[0-9]+\.[0-9]+$`
  before invoking goreleaser; trims permission scope to the actual
  minimum (`contents:write` + `id-token:write`).
- `ci.yml` build job pins `CGO_ENABLED=0` to match goreleaser; smoke-runs
  `dist/rallish version` after build; matrix now covers macOS + Linux.
- Dependabot tracks both `gomod` and `github-actions` ecosystems.

### Known follow-ups (deferred to a later release)

- Pin third-party GitHub Actions by SHA (currently on mutable tags).
- Add CodeQL workflow.
- Add Windows to the CI build matrix.
- Enforce the 70 % coverage floor in CI (currently doc-only in AGENTS.md).
- `SECURITY.md` / `CODE_OF_CONDUCT.md`.
