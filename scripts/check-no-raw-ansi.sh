#!/usr/bin/env bash
# Guardrail — no raw ANSI escape sequences outside internal/ui.
#
# Reason: internal/ui is the SSOT for color, glyphs, and status lines.
# Sprinkling \x1b[ codes elsewhere bypasses NO_COLOR / non-TTY detection
# and makes themed output drift. Add new colors to internal/ui, not the
# caller.

set -eu

# scope: tracked .go files outside internal/ui and outside test files.
# Using git ls-files keeps the sweep deterministic (no scanning dist/ etc.)
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

hits="$(git ls-files '*.go' \
  | grep -v '^internal/ui/' \
  | xargs grep -nP '\\x1b\[' 2>/dev/null || true)"

if [ -n "$hits" ]; then
  echo "✗ raw ANSI escape (\\x1b[) found outside internal/ui:" >&2
  echo "$hits" >&2
  echo >&2
  echo "Use ui.Theme helpers (Info/OK/Warn/Err/Detail) instead of hand-rolling escapes." >&2
  exit 1
fi
exit 0
