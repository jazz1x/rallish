#!/bin/sh
# rallish install script — fetches the latest GitHub Release binary for the
# user's platform and drops it under $INSTALL_DIR (default /usr/local/bin,
# falls back to ~/.local/bin if not writable).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/jazz1x/rallish/main/install.sh | sh
#
# Env:
#   RALLISH_VERSION      Pin a specific version (default: latest release tag)
#   RALLISH_INSTALL_DIR  Install destination (default: /usr/local/bin → ~/.local/bin)

set -eu

REPO="jazz1x/rallish"
INSTALL_DIR="${RALLISH_INSTALL_DIR:-/usr/local/bin}"

# --- detect OS ---
case "$(uname -s)" in
  Darwin) OS=darwin ;;
  Linux)  OS=linux  ;;
  *)
    echo "rallish: unsupported OS: $(uname -s)" >&2
    echo "Build from source: https://github.com/$REPO" >&2
    exit 1
    ;;
esac

# --- detect arch ---
case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *)
    echo "rallish: unsupported arch: $(uname -m)" >&2
    exit 1
    ;;
esac

# --- resolve version ---
if [ -z "${RALLISH_VERSION:-}" ]; then
  RALLISH_VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name"' \
    | head -1 \
    | sed -E 's/.*"v?([^"]+)".*/\1/')"
  if [ -z "$RALLISH_VERSION" ]; then
    echo "rallish: failed to resolve latest version from GitHub API" >&2
    echo "Set RALLISH_VERSION=x.y.z manually." >&2
    exit 1
  fi
fi

ARCHIVE="rallish_${RALLISH_VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/v${RALLISH_VERSION}/$ARCHIVE"

echo "rallish ${RALLISH_VERSION} (${OS}/${ARCH})"
echo "→ ${URL}"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT INT TERM

curl -fsSL "$URL" -o "$TMPDIR/$ARCHIVE"
tar -xzf "$TMPDIR/$ARCHIVE" -C "$TMPDIR"

# --- pick install dir ---
if [ -d "$INSTALL_DIR" ] && [ -w "$INSTALL_DIR" ]; then
  TARGET="$INSTALL_DIR"
else
  TARGET="$HOME/.local/bin"
  mkdir -p "$TARGET"
fi

install -m 0755 "$TMPDIR/rallish" "$TARGET/rallish"

echo
echo "✓ installed: $TARGET/rallish"
case ":$PATH:" in
  *":$TARGET:"*) ;;
  *)
    echo "  Add $TARGET to your PATH (e.g. in ~/.zshrc or ~/.bashrc):"
    echo "    export PATH=\"$TARGET:\$PATH\""
    ;;
esac
echo
echo "Next: run 'rallish bootstrap' once to install the rallish skill."
