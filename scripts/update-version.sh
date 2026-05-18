#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
VERSION_FILE="${ROOT_DIR}/VERSION"

if [[ ! -f "$VERSION_FILE" ]]; then
    echo "VERSION file not found at $VERSION_FILE" >&2
    exit 1
fi

VERSION="$(tr -d '[:space:]' < "$VERSION_FILE")"
if [[ -z "$VERSION" ]]; then
    echo "VERSION file is empty" >&2
    exit 1
fi

# Update README badges
for f in README.md README.ko.md README.jp.md; do
    path="${ROOT_DIR}/${f}"
    if [[ -f "$path" ]]; then
        sed -i.bak -E "s/version-[0-9]+\.[0-9]+\.[0-9]+/version-${VERSION}/g" "$path"
        rm -f "${path}.bak"
        echo "Updated $f -> ${VERSION}"
    fi
done

echo "Version propagated: ${VERSION}"
