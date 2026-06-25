#!/usr/bin/env bash
# Per-package coverage floor check.
# Keeps the documented floors in docs/test-plan.md §4 from silently regressing.
# Adapters and doctor are intentionally listed at their current floors because
# real-subprocess paths require gated integration tests (see docs/test-plan.md §6.1).

set -euo pipefail

python3 - "$@" <<'PY'
import subprocess, sys

floors = {
    "github.com/jazz1x/rallish/pkg/contract": 85,
    "github.com/jazz1x/rallish/internal/router": 80,
    "github.com/jazz1x/rallish/internal/exit": 80,
    "github.com/jazz1x/rallish/internal/budget": 80,
    "github.com/jazz1x/rallish/internal/cycle": 80,
    "github.com/jazz1x/rallish/internal/cycle/gates": 80,
    "github.com/jazz1x/rallish/internal/session": 75,
    "github.com/jazz1x/rallish/internal/preset": 75,
    "github.com/jazz1x/rallish/internal/ipc": 75,
    "github.com/jazz1x/rallish/internal/broker": 75,
    "github.com/jazz1x/rallish/internal/ui": 75,
    "github.com/jazz1x/rallish/internal/safepath": 75,
    "github.com/jazz1x/rallish/internal/scratch": 70,
    "github.com/jazz1x/rallish/internal/adapter": 60,
    "github.com/jazz1x/rallish/internal/adapter/claude": 35,
    "github.com/jazz1x/rallish/internal/adapter/kimi": 19,
    "github.com/jazz1x/rallish/internal/doctor": 25,
}

result = subprocess.run(
    ["go", "test", "-cover", "./..."],
    capture_output=True,
    text=True,
)
if result.returncode != 0:
    print(result.stdout)
    print(result.stderr, file=sys.stderr)
    sys.exit(result.returncode)

coverage = {}
for line in result.stdout.splitlines():
    parts = line.split()
    if len(parts) < 4 or parts[0] != "ok":
        continue
    pkg = parts[1]
    try:
        idx = parts.index("coverage:")
        cov_token = parts[idx + 1]
        coverage[pkg] = float(cov_token.replace("%", ""))
    except (ValueError, IndexError):
        continue

failed = 0
for pkg, floor in sorted(floors.items()):
    cov = coverage.get(pkg)
    if cov is None:
        print(f"✗ {pkg}: no coverage data")
        failed += 1
        continue
    if cov < floor:
        print(f"✗ {pkg}: coverage {cov:.1f}% < floor {floor}%")
        failed += 1
    else:
        print(f"✓ {pkg}: coverage {cov:.1f}% >= floor {floor}%")

sys.exit(1 if failed else 0)
PY
