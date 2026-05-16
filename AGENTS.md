# AGENTS.md — Coding Guidelines for rallish

> This file is for AI agents working on this codebase.

## Build & Test

```bash
make check   # go vet + golangci-lint + go test -race
make build   # go build -o dist/rallish
make test    # go test -race ./...
```

All commits must pass `lefthook run pre-commit` (go-fmt, go-vet, go-test -race, go-lint).

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
- Version: `latest` (tracks newest v2)
- Install mode: `binary` (default)

## Commit Messages

Follow conventional commits:

- `feat:` — new feature
- `fix:` — bug fix
- `refactor:` — code change that neither fixes a bug nor adds a feature
- `docs:` — documentation only
- `test:` — adding or correcting tests
- `chore:` — build / tooling changes
- `sec:` — security-relevant change (permission tightening, sandbox, allowlist)

## Feature Documentation Workflow

For any non-trivial subsystem (a new `internal/*` package, a new protocol
surface, a new daemon endpoint):

1. Seed a PRD in `docs/prd-<name>.md` describing problem, decision, alternatives,
   spec, test plan, guardrails, acceptance criteria.
2. Implement.
3. Add a runbook in `docs/runbook-<name>.md` describing how to verify the
   feature end-to-end.
4. Update `CHANGELOG.md`, `CHANGELOG.ko.md`, `CHANGELOG.jp.md` in lockstep.

HTML runbooks in `docs/*.html` are optional presentation mirrors of the `.md`
source. The `.md` file is canonical; the HTML may lag.
