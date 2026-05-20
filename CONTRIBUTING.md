# Contributing to rallish

Thanks for considering a contribution.

## Quick start

1. Fork + clone.
2. `make setup-hooks` (installs lefthook).
3. Make your change on a feature branch — never on `main`.
4. `make check-all` (gofmt + vet + race tests + golangci-lint +
   no-raw-ANSI sweep) — must pass. Mirrors CI exactly.
5. Open a PR against `main`.

> **Touching CLI output?** All user-facing rendering goes through
> `internal/ui` (color tokens, glyphs, prompts, tables). Don't roll
> your own ANSI — the pre-commit hook will reject it. See
> [AGENTS.md §CLI Presentation](AGENTS.md#cli-presentation).

## Conventions

- Conventional commits (`feat:`, `fix:`, `docs:`, `test:`, `chore:`,
  `sec:`, `ci:`, `build:`, `perf:`, `style:`, `refactor:`). The
  `commit-msg` lefthook hook enforces this.
- See [AGENTS.md](AGENTS.md) for project layout, coding rules, and the
  Feature Documentation Workflow (PRD → implementation → runbook →
  CHANGELOG).
- Three-language doc parity (EN / KO / JP) — see existing READMEs and
  CHANGELOGs.

## Reporting bugs

GitHub Issues. Please run `rallish doctor` and paste the output. If
the bug is security-sensitive, see [SECURITY.md](SECURITY.md) for
private disclosure.

## Releases

Maintainers: `make release-{patch,minor,major}`. See
[RELEASING.md](RELEASING.md).
