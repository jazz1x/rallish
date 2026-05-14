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
| `internal/cli/` | Cobra commands (start, doctor) |
| `internal/exit/` | Exit condition evaluators |
| `internal/ipc/` | Daemon IPC (start / resume) |
| `internal/logx/` | Structured logging |
| `internal/preset/` | YAML preset loader + built-ins |
| `internal/router/` | Role-based routing |
| `internal/scratch/` | Rolling scratchpad with compaction |
| `internal/session/` | In-memory session store |
| `pkg/contract/` | Public types (A2A, Budget, Session) |

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

## Commit Messages

Follow conventional commits:

- `feat:` — new feature
- `fix:` — bug fix
- `refactor:` — code change that neither fixes a bug nor adds a feature
- `docs:` — documentation only
- `test:` — adding or correcting tests
- `chore:` — build / tooling changes
