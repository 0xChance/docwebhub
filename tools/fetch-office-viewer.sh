#!/usr/bin/env bash

# Copyright (C) 2026 docWebHub contributors
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
# Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

# Fetches the Office document viewer and packs it into internal/office/assets.zip
# for //go:embed. Both packages are taken from the npm registry at a pinned
# version and verified against the registry's own integrity hash, because the
# result ends up inside the binary and there is no later opportunity to notice
# that the wrong bytes were shipped.
#
# The legacy package declares an exact peer dependency on the core package, so
# the two versions must move together.

set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="0.89.7"

CORE_NAME="@silurus/ooxml"
CORE_TARBALL="ooxml-${VERSION}.tgz"
CORE_URL="https://registry.npmjs.org/@silurus/ooxml/-/${CORE_TARBALL}"
CORE_SHA512="GZGQRiPjOqlWjqPv4DE1etTgdHt9walWvuONn0eWcdgMDEMSBA/SV/tPOFoIWTTcMxPb7JnYGkg66Z64lu4p9w=="

LEGACY_NAME="@silurus/ooxml-legacy-office"
LEGACY_TARBALL="ooxml-legacy-office-${VERSION}.tgz"
LEGACY_URL="https://registry.npmjs.org/@silurus/ooxml-legacy-office/-/${LEGACY_TARBALL}"
LEGACY_SHA512="sl1ayvJZ3x3jcvmg5P59gc1wof6WRVnadiV2rDqDQGdyG04W8WEb5j9lEGPElkJjHx3bK2hZ8ifvKt8ma0ZL3w=="

CACHE_DIR="build/office-cache"
STAGE_DIR="build/office-stage"
OUT_ZIP="internal/office/assets.zip"

mkdir -p "$CACHE_DIR"

# already built for this version?
if [ -f "$OUT_ZIP" ] && [ -f "internal/office/assets.version" ] &&
    [ "$(cat internal/office/assets.version)" = "$VERSION" ]; then
    echo "==> office viewer ${VERSION} already packed, skipping"
    exit 0
fi

fetch() {
    local url=$1 dest=$2 want=$3 label=$4
    if [ ! -f "$dest" ]; then
        echo "==> downloading ${label} ${VERSION}"
        curl -fsSL --retry 2 --max-time 180 -o "$dest.part" "$url"
        mv "$dest.part" "$dest"
    fi

    local got
    got=$(openssl dgst -sha512 -binary "$dest" | openssl base64 -A)
    if [ "$got" != "$want" ]; then
        echo "error: ${label} failed integrity check" >&2
        echo "  want sha512-${want}" >&2
        echo "  got  sha512-${got}" >&2
        rm -f "$dest"
        exit 1
    fi
}

fetch "$CORE_URL" "$CACHE_DIR/$CORE_TARBALL" "$CORE_SHA512" "$CORE_NAME"
fetch "$LEGACY_URL" "$CACHE_DIR/$LEGACY_TARBALL" "$LEGACY_SHA512" "$LEGACY_NAME"

rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR/legacy"

# Both packages ship the same layout: package/dist/<files>. The legacy tree goes
# into its own folder so its relative .wasm references keep resolving and the
# core chunks stay at the root, where the viewer page imports them from.
tar -xzf "$CACHE_DIR/$CORE_TARBALL" -C "$STAGE_DIR" --strip-components=2 package/dist
tar -xzf "$CACHE_DIR/$LEGACY_TARBALL" -C "$STAGE_DIR/legacy" --strip-components=2 package/dist

# Type declarations are only useful to TypeScript consumers.
rm -rf "$STAGE_DIR/types" "$STAGE_DIR/legacy/types"

echo "$VERSION" >"$STAGE_DIR/VERSION"

# -X drops extra file attributes so the same inputs always produce the same zip.
rm -f "$OUT_ZIP"
( cd "$STAGE_DIR" && zip -q -9 -X -r "$OLDPWD/$OUT_ZIP" . )
echo "$VERSION" >internal/office/assets.version

size=$(wc -c <"$OUT_ZIP" | tr -d ' ')
files=$(unzip -l "$OUT_ZIP" | tail -1 | awk '{print $2}')
echo "==> packed ${files} files into ${OUT_ZIP} ($((size / 1024)) KB)"
