#!/usr/bin/env bash
# Local-mirror of the CI gate suite. Run this before pushing — failures
# here will fail GitHub Actions too.
#
#   bash scripts/check-all.sh
#
# Honors the repo-pinned golangci-lint at .toolchain/bin/ when present
# (matching lefthook + the Makefile), else falls back to whatever is on
# $PATH.

set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

PATH="$ROOT/.toolchain/go/bin:$ROOT/.toolchain/bin:$PATH"
export PATH

# 1. Format — scope to repo source only (skip .toolchain/ vendored tools)
echo "→ gofmt"
unfmt="$(gofmt -l cmd internal pkg 2>/dev/null || true)"
if [ -n "$unfmt" ]; then
  echo "✗ gofmt drift in:" >&2
  echo "$unfmt" >&2
  exit 1
fi
echo "  ok"

# 2. Vet
echo "→ go vet ./..."
go vet ./...
echo "  ok"

# 3. Race tests
echo "→ go test ./... -race -count=1"
go test ./... -race -count=1
echo "  ok"

# 4. Lint — use repo-pinned binary if present, else PATH
echo "→ golangci-lint run --timeout=10m"
if [ -x "$ROOT/.toolchain/bin/golangci-lint" ]; then
  "$ROOT/.toolchain/bin/golangci-lint" run --timeout=10m
elif command -v golangci-lint >/dev/null 2>&1; then
  golangci-lint run --timeout=10m
else
  echo "✗ golangci-lint not found; run 'make setup-hooks' first" >&2
  exit 1
fi
echo "  ok"

# 5. Project-specific guardrails
echo "→ scripts/check-no-raw-ansi.sh"
bash "$ROOT/scripts/check-no-raw-ansi.sh"
echo "  ok"

echo
echo "✓ all gates green — safe to push"
