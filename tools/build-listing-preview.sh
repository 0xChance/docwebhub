#!/usr/bin/env bash

# Copyright (C) 2026 docWebHub contributors
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
# Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

# Rebuilds internal/web/preview.min.js from tools/listing-preview.mjs.
#
# The output is committed, so this is only needed when the integration code or
# the pinned library versions change. It is not part of build.sh: a normal
# build must keep working without Node.js.

set -euo pipefail

cd "$(dirname "$0")/.."

PRISM="1.30.0"
MARKED="18.1.2"
ESBUILD="0.28.2"

CACHE_DIR="build/listing-preview-cache"
ENTRY="tools/listing-preview.mjs"
OUT="internal/web/preview.min.js"

for tool in node npm; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "error: rebuilding the listing preview needs $tool" >&2
        exit 1
    fi
done

mkdir -p "$CACHE_DIR"

cat > "$CACHE_DIR/package.json" <<JSON
{
  "name": "docwebhub-listing-preview",
  "private": true,
  "type": "module",
  "dependencies": {
    "marked": "${MARKED}",
    "prismjs": "${PRISM}"
  },
  "devDependencies": {
    "esbuild": "${ESBUILD}"
  }
}
JSON

npm install --prefix "$CACHE_DIR" --silent --no-audit --no-fund

# The entry imports the packages by name, so build from the cache directory
# where node_modules lives.
cp "$ENTRY" "$CACHE_DIR/entry.mjs"
(
    cd "$CACHE_DIR"
    node_modules/.bin/esbuild entry.mjs \
        --bundle \
        --minify \
        --format=iife \
        --target=es2020 \
        --legal-comments=inline \
        --banner:js='/*! docWebHub listing preview bundle: PrismJS '"${PRISM}"' and marked '"${MARKED}"', both MIT licensed. See LICENSING.md. */' \
        --outfile="../../$OUT" \
        --log-level=warning
)

echo "==> wrote $OUT"
