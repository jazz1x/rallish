#!/usr/bin/env bash
# release.sh — bump VERSION, propagate to badges, commit, tag, optionally push.
#
# Usage:
#   scripts/release.sh patch              # 0.0.1 → 0.0.2
#   scripts/release.sh minor              # 0.0.1 → 0.1.0
#   scripts/release.sh major              # 0.0.1 → 1.0.0
#   scripts/release.sh 1.2.3              # explicit version
#   scripts/release.sh patch --dry-run    # show what would happen
#   scripts/release.sh patch --no-push    # commit + tag locally; don't push
#
# Safety: refuses to run if the working tree is dirty. Prints branch name and
# asks for confirmation if you're not on the configured RELEASE_BRANCH
# (default: main). Aborts before push unless --yes is given.

set -euo pipefail

KIND="${1:-}"
case "${2:-}" in
  --dry-run|--no-push|--yes|"") ;;
  *) echo "unknown flag: $2" >&2; exit 2 ;;
esac
FLAG="${2:-}"

usage() {
  cat >&2 <<'USAGE'
usage: scripts/release.sh <patch|minor|major|X.Y.Z> [--dry-run | --no-push | --yes]
USAGE
}

[ -z "$KIND" ] && { usage; exit 2; }

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

RELEASE_BRANCH="${RELEASE_BRANCH:-main}"
VERSION_FILE="$ROOT_DIR/VERSION"

# --- safety: clean tree ---
if [ -n "$(git status --porcelain)" ]; then
  echo "❌ working tree not clean. commit or stash first." >&2
  git status --short >&2
  exit 1
fi

CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"
if [ "$CURRENT_BRANCH" != "$RELEASE_BRANCH" ] && [ "$FLAG" != "--yes" ]; then
  echo "⚠️  on branch '$CURRENT_BRANCH', release branch is '$RELEASE_BRANCH'."
  echo "    re-run with --yes to release from a non-release branch, or switch first."
  [ "$FLAG" = "--dry-run" ] || exit 1
fi

# --- safety: upstream tracking exists ---
UPSTREAM="$(git rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null || true)"
if [ -z "$UPSTREAM" ] && [ "$FLAG" != "--no-push" ] && [ "$FLAG" != "--dry-run" ]; then
  echo "❌ no upstream set for branch '$CURRENT_BRANCH'." >&2
  echo "   run: git push -u origin $CURRENT_BRANCH    (then re-run)" >&2
  exit 1
fi

# --- safety: refresh remote refs so we see new tags / commits ---
git fetch --tags --quiet origin 2>/dev/null || true

# --- safety: local commits all pushed ---
if [ -n "$UPSTREAM" ]; then
  AHEAD="$(git rev-list --count "$UPSTREAM"..HEAD 2>/dev/null || echo 0)"
  if [ "$AHEAD" -gt 0 ] && [ "$FLAG" != "--yes" ] && [ "$FLAG" != "--dry-run" ]; then
    echo "⚠️  $AHEAD local commit(s) not pushed to $UPSTREAM."
    echo "    the tag will move with them. re-run with --yes to acknowledge."
    exit 1
  fi
fi

# --- compute new version ---
CURRENT="$(tr -d '[:space:]' < "$VERSION_FILE")"
case "$KIND" in
  patch|minor|major)
    IFS='.' read -r MAJ MIN PAT <<<"$CURRENT"
    case "$KIND" in
      patch) PAT=$((PAT+1)) ;;
      minor) MIN=$((MIN+1)); PAT=0 ;;
      major) MAJ=$((MAJ+1)); MIN=0; PAT=0 ;;
    esac
    NEW="$MAJ.$MIN.$PAT"
    ;;
  *)
    if [[ "$KIND" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      NEW="$KIND"
    else
      echo "❌ invalid version spec: $KIND" >&2
      usage
      exit 2
    fi
    ;;
esac

# --- safety: monotonic version (refuse if NEW <= CURRENT) ---
ver_gt() {
  # returns 0 (true) when $1 > $2 in semver
  [ "$1" = "$2" ] && return 1
  printf '%s\n%s\n' "$2" "$1" | sort -V -C 2>/dev/null
}
if ! ver_gt "$NEW" "$CURRENT"; then
  echo "❌ new version $NEW is not greater than current $CURRENT." >&2
  exit 1
fi

echo "Release plan:"
echo "  current branch  : $CURRENT_BRANCH"
echo "  current version : $CURRENT"
echo "  next version    : $NEW"
echo "  tag             : v$NEW"

# Local tag exists?
if git rev-parse -q --verify "refs/tags/v$NEW" >/dev/null; then
  echo "❌ tag v$NEW already exists locally." >&2
  exit 1
fi
# Remote tag exists? (fetch above refreshed)
if git ls-remote --tags origin "v$NEW" 2>/dev/null | grep -q "refs/tags/v$NEW"; then
  echo "❌ tag v$NEW already exists on origin." >&2
  exit 1
fi

if [ "$FLAG" = "--dry-run" ]; then
  echo "(dry-run) would update VERSION, propagate to badges, commit, tag v$NEW"
  echo "(dry-run) would then push origin $CURRENT_BRANCH --follow-tags (unless --no-push)"
  exit 0
fi

# --- apply ---
echo "$NEW" > "$VERSION_FILE"
bash "$ROOT_DIR/scripts/update-version.sh"

git add VERSION README.md README.ko.md README.jp.md
git commit -m "release: v$NEW"
git tag -a "v$NEW" -m "Release v$NEW"

echo "✓ committed + tagged v$NEW (local)"

if [ "$FLAG" = "--no-push" ]; then
  echo "  --no-push: skipping git push. To push later:"
  echo "      git push origin $CURRENT_BRANCH --follow-tags"
  exit 0
fi

if [ "$FLAG" != "--yes" ]; then
  printf "Push to origin/%s now? [y/N] " "$CURRENT_BRANCH"
  read -r ANS
  case "$ANS" in
    y|Y|yes) ;;
    *) echo "  not pushed. To push later: git push origin $CURRENT_BRANCH --follow-tags"; exit 0 ;;
  esac
fi

git push origin "$CURRENT_BRANCH" --follow-tags
echo "✓ pushed v$NEW to origin/$CURRENT_BRANCH"
echo "  release.yml will run goreleaser; check Actions tab for progress."
