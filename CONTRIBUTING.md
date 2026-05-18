# Contributing to rallish

Thanks for considering a contribution.

## Quick start

1. Fork + clone.
2. `make setup-hooks` (installs lefthook).
3. Make your change on a feature branch.
4. `make check` (vet + golangci-lint + race tests) — must pass.
5. Open a PR against `main`.

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
