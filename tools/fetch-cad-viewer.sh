#!/usr/bin/env bash

# Copyright (C) 2026 docWebHub contributors
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
# Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

# Builds the CAD preview assets and packs them into internal/cad/assets.zip for
# //go:embed.
#
# Unlike the Office viewer, this one is not published as a single loadable file:
# @mlightcad/cad-simple-viewer ships an ESM bundle that still imports six peer
# packages plus four three.js example modules. So the packages are installed
# here, bundled with esbuild into one module, and only the result is shipped.
#
# This step needs Node.js (18 or newer) and npm, but only when the assets change:
# the archive is cached and keyed by the version below, exactly like the Office
# viewer. Set SKIP_CAD=1 to build without CAD preview instead.

set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="1.7.6"
# DWG support. The viewer itself only parses DXF; a host that wants DWG has to
# register a converter, and LibreDWG is the only one available.
CONVERTER="3.15.20"
# Pinned to the peer dependency ranges the viewer declares, so the bundle does
# not drift when a maintainer publishes a new minor.
THREE="0.172.0"
POLYBOOL="1.1.0"
LODASH="4.17.21"
DATA_MODEL="1.15.20"
THREE_RENDERER="1.7.6"
MTEXT_RENDERER="0.13.6"
MTEXT_PARSER="1.5.5"
# Vector PDF export. Both are cut from the same release as the viewer above,
# and the plugin pins the viewer and renderer versions as peers, so they move
# together or not at all.
PDF_PLUGIN="1.7.6"
PDF_RENDERER="1.7.6"
ESBUILD="0.25.10"

# The archive is keyed on everything that changes its contents, so bumping the
# converter rebuilds a viewer whose version number alone did not move. The font
# revision covers the vendored fonts and their alias list, which are not part of
# any upstream version number either.
FONTS="3"
BUILD_ID="${VERSION}+libredwg${CONVERTER}+fonts${FONTS}+pdf${PDF_PLUGIN}"

CACHE_DIR="build/cad-cache"
STAGE_DIR="build/cad-stage"
OUT_ZIP="internal/cad/assets.zip"
VERSION_FILE="internal/cad/assets.version"

if [ -f "$OUT_ZIP" ] && [ -f "$VERSION_FILE" ] && [ "$(cat "$VERSION_FILE")" = "$BUILD_ID" ]; then
    echo "==> CAD viewer ${BUILD_ID} already packed, skipping"
    exit 0
fi

for tool in node npm zip; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "error: CAD preview needs $tool, which was not found" >&2
        echo "       install Node.js 18+ and zip, or set SKIP_CAD=1 to build without it" >&2
        exit 1
    fi
done

mkdir -p "$CACHE_DIR"

# The dependency set is expressed as a package.json so npm resolves and verifies
# the tarballs itself, including the peer graph the viewer expects.
cat > "$CACHE_DIR/package.json" <<JSON
{
  "name": "docwebhub-cad-viewer",
  "private": true,
  "type": "module",
  "dependencies": {
    "@mlightcad/cad-simple-viewer": "${VERSION}",
    "@mlightcad/libredwg-converter": "${CONVERTER}",
    "@mlightcad/data-model": "${DATA_MODEL}",
    "@mlightcad/three-renderer": "${THREE_RENDERER}",
    "@mlightcad/mtext-renderer": "${MTEXT_RENDERER}",
    "@mlightcad/mtext-parser": "${MTEXT_PARSER}",
    "@mlightcad/cad-pdf-plugin": "${PDF_PLUGIN}",
    "@mlightcad/pdf-renderer": "${PDF_RENDERER}",
    "@velipso/polybool": "${POLYBOOL}",
    "lodash-es": "${LODASH}",
    "three": "${THREE}"
  },
  "devDependencies": {
    "esbuild": "${ESBUILD}"
  }
}
JSON

cat > "$CACHE_DIR/entry.js" <<'JS'
// The page imports these names; keeping the surface tiny keeps the bundle
// tree-shakeable.
export {
  AcApDocManager,
  AcEdOpenMode,
  AcApOpenViewMode,
} from '@mlightcad/cad-simple-viewer';
// The viewer parses DXF itself and leaves DWG to the host, so the converter and
// the registration API have to be re-exported for the preview page.
export { AcDbDatabaseConverterManager, AcDbFileType } from '@mlightcad/data-model';
export { AcDbLibreDwgConverter } from '@mlightcad/libredwg-converter';
JS

cat > "$CACHE_DIR/pdf-entry.js" <<'JS'
// A second entry, so the PDF backend (pdf-lib and friends) stays out of the
// initial download. esbuild splits both entries onto one shared chunk, which
// keeps AcApDocManager a single instance: the plugin reads the current document
// from it, and a duplicated copy would see no document at all.
//
// Imported from lib/ rather than the package root on purpose. The root resolves
// to a pre-bundled dist that also contains the PDF *import* path and pdf.js,
// which esbuild cannot shake back out; the per-file ESM build drops both.
export { AcApPdfConvertor } from '@mlightcad/cad-pdf-plugin/lib/AcApPdfConvertor.js';
export { resolveAcApPdfExportOptions } from '@mlightcad/cad-pdf-plugin/lib/AcApPdfExportOptions.js';
JS

echo "==> installing the CAD viewer packages"
(cd "$CACHE_DIR" && npm install --no-audit --no-fund --legacy-peer-deps --silent)

echo "==> bundling the CAD viewer"
# The build script is copied in so that "import 'esbuild'" resolves against the
# node_modules tree npm just created here. The output directory is emptied
# first: chunk names carry a content hash, so a rebuild otherwise leaves the
# previous build's chunks behind to be packed as well.
rm -rf "$CACHE_DIR/out"
cp tools/cad-viewer-build.mjs "$CACHE_DIR/build.mjs"
node "$CACHE_DIR/build.mjs" "$CACHE_DIR" "$CACHE_DIR/out"

if [ ! -s "$CACHE_DIR/out/viewer.js" ] || [ ! -s "$CACHE_DIR/out/pdf.js" ]; then
    echo "error: the bundle was not produced" >&2
    exit 1
fi

rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR/assets"
# Everything the build emitted: the two entries plus the shared chunk they both
# import. Names are fixed except for the chunk hash, so copy the directory
# rather than listing files.
cp "$CACHE_DIR"/out/*.js "$STAGE_DIR/"

# The viewer spawns this worker from a path relative to the page, so it has to
# sit at assets/mtext-renderer-worker.js next to viewer.js.
cp "$CACHE_DIR/node_modules/@mlightcad/cad-simple-viewer/dist/mtext-renderer-worker.js" \
    "$STAGE_DIR/assets/mtext-renderer-worker.js"

# The DWG converter runs in its own module worker, which locates the wasm as
# "libredwg-web.wasm" next to itself, so both files have to keep their names
# and share a directory. Keeping the 10 MB wasm out of viewer.js also means a
# browser only downloads it when a .dwg is actually opened.
cp "$CACHE_DIR/node_modules/@mlightcad/libredwg-converter/dist/libredwg-parser-worker.js" \
    "$STAGE_DIR/assets/libredwg-parser-worker.js"
cp "$CACHE_DIR/node_modules/@mlightcad/libredwg-web/wasm/libredwg-web.wasm" \
    "$STAGE_DIR/assets/libredwg-web.wasm"

# Fonts are looked up as <baseUrl>/fonts/<name>, and the viewer asks for the
# catalog at <baseUrl>/fonts/fonts.json before any text is drawn. The names
# drawings use belong to Autodesk's SHX set and to Microsoft's system fonts,
# neither of which may be redistributed, so instead of shipping them the catalog
# maps every name onto two open fonts vendored in tools/cad-fonts: a Latin one
# and a Chinese one. See the comment at the top of aliases.json.
mkdir -p "$STAGE_DIR/data/fonts"
cp tools/cad-fonts/noto-sans.ttf tools/cad-fonts/noto-sans-sc.otf "$STAGE_DIR/data/fonts/"
node tools/cad-fonts/make-catalog.mjs "$STAGE_DIR/data/fonts/fonts.json"

echo "$BUILD_ID" > "$STAGE_DIR/VERSION"

rm -f "$OUT_ZIP"
mkdir -p "$(dirname "$OUT_ZIP")"
(cd "$STAGE_DIR" && zip -q -9 -X -r "$(cd "$OLDPWD" && pwd)/$OUT_ZIP" .)

echo "$BUILD_ID" > "$VERSION_FILE"
echo "==> packed CAD viewer ${BUILD_ID} into ${OUT_ZIP} ($(du -h "$OUT_ZIP" | cut -f1))"
