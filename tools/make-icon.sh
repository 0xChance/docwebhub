#!/usr/bin/env bash

# Copyright (C) 2026 docWebHub contributors
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
# Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

# Builds the macOS app icon (.icns) from assets/logo.png.
# Relies on the sips and iconutil tools shipped with macOS; nothing extra to install.
set -euo pipefail

cd "$(dirname "$0")/.."

SRC="assets/logo.png"
OUT="${1:-build/AppIcon.icns}"

if [ ! -f "$SRC" ]; then
    echo "error: icon source not found: $SRC" >&2
    exit 1
fi

for tool in sips iconutil; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "error: missing $tool (only available on macOS)" >&2
        exit 1
    fi
done

width="$(sips -g pixelWidth "$SRC" | awk '/pixelWidth/ {print $2}')"
height="$(sips -g pixelHeight "$SRC" | awk '/pixelHeight/ {print $2}')"
if [ "${width:-0}" -lt 1024 ] || [ "${height:-0}" -lt 1024 ]; then
    echo "warning: icon source is ${width}x${height}, smaller than 1024x1024; large icon sizes will look blurry" >&2
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The source art is already 1024x1024, with the artwork filling about 80% of the
# canvas and transparent padding around it, matching the visual proportions of
# macOS icons. Just scale down to the standard sizes here; no extra padding needed.
sips -s format png -z 1024 1024 "$SRC" --out "$work/appicon.png" >/dev/null

iconset="$work/AppIcon.iconset"
mkdir -p "$iconset"

# The standard size list iconutil expects: <pixels>:<file name suffix>
specs=(
    16:16x16
    32:16x16@2x
    32:32x32
    64:32x32@2x
    128:128x128
    256:128x128@2x
    256:256x256
    512:256x256@2x
    512:512x512
    1024:512x512@2x
)

for spec in "${specs[@]}"; do
    size="${spec%%:*}"
    name="${spec##*:}"
    sips -z "$size" "$size" "$work/appicon.png" --out "$iconset/icon_${name}.png" >/dev/null
done

mkdir -p "$(dirname "$OUT")"
iconutil -c icns "$iconset" -o "$OUT"

echo "generated icon $OUT"
