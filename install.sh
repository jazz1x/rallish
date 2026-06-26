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
#
# NOTE: this is the public curl entry point and is an intentional mirror of the
# bundled installer at internal/skills/rallish/scripts/install-binary.sh (that
# copy is go:embed'd into the binary for `rallish bootstrap`). go:embed cannot
# reach outside internal/skills, so the two files cannot share one source —
# keep them in sync when editing either. (Previously a symlink, which broke the
# curl path: raw.githubusercontent.com does not resolve symlinks, it returns the
# link's target path as text.)

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

# Guess the user's shell profile so we can print an exact copy-paste command.
RC_FILE=""
SHELL_NAME=""
if [ -n "${SHELL:-}" ]; then
  SHELL_NAME="$(basename "$SHELL")"
fi
if [ -z "$SHELL_NAME" ]; then
  SHELL_NAME="$(basename "$(ps -p "${PPID:-}" -o comm= 2>/dev/null || echo sh)")"
fi

case "$SHELL_NAME" in
  zsh)  RC_FILE="$HOME/.zshrc" ;;
  bash) RC_FILE="$HOME/.bashrc" ;;
  fish) RC_FILE="$HOME/.config/fish/config.fish" ;;
  *)    RC_FILE="$HOME/.profile" ;;
esac

case ":$PATH:" in
  *":$TARGET:"*)
    echo
    echo "✓ $TARGET is already on your PATH."
    ;;
  *)
    echo
    echo "⚠ $TARGET is not on your PATH yet."
    echo "  Add it by running this command (copy-paste into your terminal):"
    echo
    echo "    echo 'export PATH=\"$TARGET:\$PATH\"' >> $RC_FILE"
    echo
    echo "  Then reload your shell config:"
    echo
    echo "    source $RC_FILE"
    ;;
esac

echo
echo "Next steps:"
echo "  1. Run 'rallish bootstrap' once to install the rallish skill."
echo "  2. Start the broker:   rallish daemon"
echo "  3. Start a session:    rallish squash --preset pair-review --task \"...\""
echo
