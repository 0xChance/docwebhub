#!/usr/bin/env bash

# Copyright (C) 2026 docWebHub contributors
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
# Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

# Generates the Windows resource objects (rsrc_windows_*.syso): app icon, version info, manifest.
#
# Requires go-winres: go install github.com/tc-hib/go-winres@latest
# The outputs are binary and already committed; regenerate only when changing the icon or version.
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${VERSION:-1.0.0}"
OUT="${OUT:-rsrc}"
ICON_PNG="${ICON_PNG:-assets/docwebhub.png}"

if ! command -v go-winres >/dev/null 2>&1; then
    echo "error: go-winres not found; run go install github.com/tc-hib/go-winres@latest first" >&2
    exit 1
fi

if [ ! -f "$ICON_PNG" ]; then
    echo "error: icon not found: $ICON_PNG" >&2
    exit 1
fi

# The version must be numeric (e.g. 1.0.0); values like "dev" are rejected.
go-winres simply \
    --arch amd64,arm64 \
    --out "$OUT" \
    --icon "$ICON_PNG" \
    --manifest gui \
    --product-name "docWebHub" \
    --file-description "docWebHub - local static site preview" \
    --product-version "$VERSION" \
    --file-version "$VERSION"

ls -l "${OUT}"_windows_*.syso
