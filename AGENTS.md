# AGENTS.md — Coding Guidelines for rallish

> This file is for AI agents working on this codebase.

## Build & Test

```bash
make check-all  # gofmt + go vet + go test -race + golangci-lint + no-raw-ansi (CI parity, recommended)
make check      # subset — go vet + golangci-lint + go test -race
make build      # go build -o dist/rallish
make test       # go test -race ./...
```

`make check-all` is the single source of truth for "is this branch
shippable" — it mirrors the GitHub Actions gate exactly (including the
pinned `golangci-lint v2.12.2` from `.toolchain/bin/`). Run it before
every push. Lefthook is the same gate set, but only fires if lefthook
itself is on `$PATH` — `make check-all` works everywhere.

All commits must pass `make check-all` (and, if installed, `lefthook
run pre-commit`).

## Project Layout

| Directory | Contents |
|-----------|----------|
| `cmd/rallish/` | `main.go` |
| `internal/adapter/` | CLI adapters (claude, kimi, fake) |
| `internal/broker/` | HTTP broker, A2A handlers, SSE |
| `internal/budget/` | Token / turn / deadline budgets |
| `internal/buildinfo/` | Build metadata (version string) |
| `internal/cli/` | Cobra commands (squash, rally, doctor, daemon, add) |
| `internal/doctor/` | Adapter + daemon health checks |
| `internal/exit/` | Exit condition evaluators |
| `internal/ipc/` | Unix domain socket transport (CLI↔Daemon) |
| `internal/logx/` | Structured logging |
| `internal/preset/` | YAML preset loader + built-ins |
| `internal/router/` | Role-based routing |
| `internal/runner/` | Polling loop that drives adapters |
| `internal/safepath/` | Path-traversal guards for user-supplied paths |
| `internal/scratch/` | Rolling scratchpad with compaction |
| `internal/session/` | In-memory session store |
| `internal/ui/` | CLI presentation SSOT — color tokens, glyphs, prompts, tables. ALL user-facing CLI output goes through here. |
| `internal/config/` | `~/.rallish/config.yaml` schema + Get / Set / Save / Resolve. |
| `pkg/contract/` | Public types (A2A, Budget, Session, Rally) |
| `skills/` | Symlink → `internal/skills/`. Vendor-neutral Agent Skills discovery path |
| `internal/skills/` | Canonical SKILL.md sources (embedded into the rallish binary via `go:embed`); installed globally by `rallish bootstrap` |

**Package surface rule:** `pkg/contract` is the only package importable by
external adapters or A2A clients. Everything under `internal/` is private
and may break without notice.

## Code Style

- Go 1.25 idioms.
- `golangci-lint` v2 config in `.golangci.yml`.
- No `fmt.Println` / `fmt.Printf` in library code (use `logx`).
- Error wrapping: `fmt.Errorf("...: %w", err)`.
- Context propagation: always pass `context.Context` as the first arg.

## CLI Presentation

User-facing CLI output is the responsibility of `internal/ui`. Never
write raw ANSI escapes elsewhere — `scripts/check-no-raw-ansi.sh`
enforces this in pre-commit. Add new colors / glyphs / prompt helpers
to the `ui` package, not to the caller. The `Theme` is single-goroutine
by design (lazily-cached bufio reader for multi-prompt wizards).

Plain `fmt.Fprintln` / `fmt.Fprintf` for cases that need no theming
(e.g. `config get` printing a raw value) must use `_, _ = fmt.Fprintln(...)`
or capture the return; errcheck is enforced.

## A2A Protocol

When modifying A2A-related code:

1. Update `pkg/contract/a2a.go` for type changes.
2. Update `internal/broker/a2a.go` for handler changes.
3. Update `docs/a2a-compatibility.md` for mapping changes.
4. Update tests in `internal/broker/a2a_test.go` (if it exists) or add them.

## Testing

- Coverage floor: 70% on `internal/session`, `internal/router`, `internal/exit`, `internal/preset`, `pkg/contract`.
- Table-driven tests preferred.
- Use `fake.NewPingPong` for turn-taking integration tests.

## CI / Tooling Rules

### golangci-lint

This repo uses **golangci-lint v2**. The following rules are non-obvious and cost hours if violated:

1. **v2 minors have breaking schema changes.** `v2.1.6` and `v2.12.2` are NOT compatible in `.golangci.yml`.
   - `run.exclude-dirs` → `linters.exclusions.paths`
   - `linters-settings` → `linters.settings`
   - `forbidigo.forbid[].p` → `forbidigo.forbid[].pattern`
2. **CI action version matters.** `golangci-lint-action@v6` does NOT support v2. Use **v7**.
3. **Go build version matters.** A golangci-lint binary built with Go 1.24 cannot lint a Go 1.25 project. When this happens, upgrade the linter binary (not the action).
4. **Never use `install-mode: goinstall` in CI.** It tries the v1 module path and fails for v2. Use binary mode (default).
5. **Keep `.golangci.yml` minimal.** Avoid linter-specific settings when possible; they drift across minors.

Current working combination (validated):
- Action: `golangci/golangci-lint-action@v7`
- Version: `latest` (tracks newest v2, currently `v2.12.2`)
- Install mode: `binary` (default)

**Local toolchain at `.toolchain/bin/golangci-lint` should match CI** — keep
it at v2.12.2 or newer. Older v2 minors (e.g. v2.1.6) do not have the
`gosec` G703 (path-traversal taint) rule, so a clean local lint can still
fail the CI pipeline. To upgrade:

```bash
curl -fsSL -o /tmp/gcl.tar.gz \
  "https://github.com/golangci/golangci-lint/releases/download/v2.12.2/golangci-lint-2.12.2-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz"
tar -xzf /tmp/gcl.tar.gz -C /tmp
mv /tmp/golangci-lint-2.12.2-*/golangci-lint .toolchain/bin/golangci-lint
```

## Commit Messages

Follow conventional commits. **Enforced by lefthook `commit-msg` hook** — a
commit without a valid prefix is rejected before it lands.

Allowed prefixes:

- `feat:` — new feature
- `fix:` — bug fix
- `refactor:` — code change that neither fixes a bug nor adds a feature
- `docs:` — documentation only
- `test:` — adding or correcting tests
- `chore:` — build / tooling changes
- `sec:` — security-relevant change (permission tightening, sandbox, allowlist)
- `ci:` — CI / GitHub Actions
- `build:` — build system / dependencies
- `perf:` — performance improvement
- `style:` — formatting only, no semantic change

Optional scope: `feat(rally): ...`, `fix(broker): ...`.

`Merge ...` / `Revert ...` are accepted as-is.

## Feature Documentation Workflow

For any non-trivial subsystem (a new `internal/*` package, a new protocol
surface, a new daemon endpoint):

1. Seed a PRD in `docs/prd-<name>.md` describing problem, decision, alternatives,
   spec, test plan, guardrails, acceptance criteria.
2. Implement.
3. Add a runbook in `docs/runbook-<name>.md` describing how to verify the
   feature end-to-end.
4. Update `CHANGELOG.md`, `CHANGELOG.ko.md`, `CHANGELOG.jp.md` in lockstep.

HTML runbooks were experimented with and removed. Use the `.md` runbooks; if a
presentation-rendered version is needed, generate it on demand.
