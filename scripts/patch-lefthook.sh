#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOOKS_DIR="${ROOT_DIR}/.git/hooks"
TOOLCHAIN_LEFTHOOK="${ROOT_DIR}/.toolchain/bin/lefthook"

if [[ ! -f "$TOOLCHAIN_LEFTHOOK" ]]; then
    echo "lefthook not found at $TOOLCHAIN_LEFTHOOK" >&2
    exit 1
fi

patched=0
for hook in "$HOOKS_DIR"/*; do
    if [[ -f "$hook" ]] && grep -q "call_lefthook" "$hook" 2>/dev/null; then
        if grep -q '.toolchain/bin/lefthook' "$hook"; then
            continue
        fi
        awk '
        /elif go tool lefthook -h/ {
            print "    elif test -f \"$dir/.toolchain/bin/lefthook\""
            print "    then"
            print "      PATH=\"${dir}/.toolchain/go/bin:${dir}/.toolchain/bin:${PATH}\" \"$dir/.toolchain/bin/lefthook\" \"$@\""
        }
        { print }
        ' "$hook" > "${hook}.tmp"
        mv "${hook}.tmp" "$hook"
        chmod +x "$hook"
        echo "Patched $(basename "$hook")"
        ((patched++)) || true
    fi
done

if [[ "$patched" -eq 0 ]]; then
    echo "No lefthook hooks needed patching."
fi
