# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
