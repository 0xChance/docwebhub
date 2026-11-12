#!/usr/bin/env bash

# Copyright (C) 2026 docWebHub contributors
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
# Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

set -euo pipefail

cd "$(dirname "$0")"

OUT_DIR="${OUT_DIR:-build}"
VERSION="${VERSION:-dev}"
HOST_OS="$(go env GOHOSTOS)"
HOST_ARCH="$(go env GOHOSTARCH)"

targets=("$@")
if [ ${#targets[@]} -eq 0 ]; then
    # Build for this machine plus the Windows binary, so a plain ./build.sh
    # always produces something that runs locally (an Intel Mac must not get
    # only darwin/arm64). Pass explicit targets to override, e.g.
    #   ./build.sh darwin/arm64 darwin/amd64 windows/amd64
    targets=("$HOST_OS/$HOST_ARCH")
    if [ "$HOST_OS/$HOST_ARCH" != "windows/amd64" ]; then
        targets+=("windows/amd64")
    fi
fi

mkdir -p "$OUT_DIR"

# The Office viewer is compiled into the binary, so its assets have to exist
# before the first go build. The script is idempotent and caches its downloads;
# set SKIP_OFFICE=1 to build without the viewer instead.
if [ "${SKIP_OFFICE:-0}" = "1" ]; then
    echo "==> skipping the Office viewer (SKIP_OFFICE=1)"
    OFFICE_TAGS=""
else
    ./tools/fetch-office-viewer.sh
    OFFICE_TAGS="office"
fi

# Same idea for the CAD viewer, which is bundled from npm with esbuild and so
# needs Node.js the first time. Set SKIP_CAD=1 to leave it out.
if [ "${SKIP_CAD:-0}" = "1" ]; then
    echo "==> skipping the CAD viewer (SKIP_CAD=1)"
    CAD_TAGS=""
else
    ./tools/fetch-cad-viewer.sh
    CAD_TAGS="cad"
fi

for target in "${targets[@]}"; do
    os="${target%%/*}"
    arch="${target##*/}"

    if [ "$os" = "$target" ] || [ -z "$arch" ]; then
        echo "error: target must look like <os>/<arch>, e.g. darwin/arm64" >&2
        exit 1
    fi

    cgo=0
    ext=""
    ldflags="-s -w -X main.version=${VERSION}"

    case "$os" in
        darwin)
            cgo=1
            if [ "$HOST_OS" != "darwin" ]; then
                echo "error: building macOS targets requires a macOS host (cgo + AppKit)" >&2
                exit 1
            fi
            ;;
        windows)
            ext=".exe"
            ldflags="${ldflags} -H windowsgui"
            ;;
        linux)
            cgo=1
            if [ "$HOST_OS" != "linux" ]; then
                echo "error: building Linux targets requires a Linux host (cgo, GTK 3, Ayatana AppIndicator 3)" >&2
                exit 1
            fi
            # The tray icon is a StatusNotifier item. GTK 3 alone is not enough:
            # GtkStatusIcon does not show up on Wayland bars.
            if ! pkg-config --exists gtk+-3.0 ayatana-appindicator3-0.1; then
                echo "error: Linux builds need GTK 3 and Ayatana AppIndicator 3 development files." >&2
                echo "       pkg-config modules: gtk+-3.0 ayatana-appindicator3-0.1" >&2
                echo "       Arch:          gtk3 libayatana-appindicator" >&2
                echo "       Debian/Ubuntu: libgtk-3-dev libayatana-appindicator3-dev" >&2
                exit 1
            fi
            ;;
        *)
            echo "error: unsupported OS $os" >&2
            exit 1
            ;;
    esac

    case "$arch" in
        amd64 | arm64) ;;
        *)
            echo "error: unsupported arch $arch" >&2
            exit 1
            ;;
    esac

    name="docwebhub-${os}-${arch}"
    output="$OUT_DIR/${name}${ext}"
    echo "==> building ${os}/${arch} -> ${output}"
    CGO_ENABLED="$cgo" GOOS="$os" GOARCH="$arch" go build \
        -trimpath \
        -tags "$OFFICE_TAGS $CAD_TAGS" \
        -ldflags "$ldflags" \
        -o "$output" \
        .

    # macOS builds are additionally wrapped into a .app: double-clicking launches
    # it straight from Finder without opening a terminal window.
    if [ "$os" = "darwin" ]; then
        bundle="${OUT_DIR}/docWebHub.app"
        # Assemble the bundle away from the final path and rename it into place at
        # the very end. Finder (via IconServices) scans a bundle the moment its
        # directory shows up; building in place means that first scan sees a .app
        # without Info.plist and without AppIcon.icns, and it then caches the
        # generic application icon for that path forever. Rebuilding afterwards does
        # not invalidate the entry - neither touch, nor lsregister -f, nor a Finder
        # restart - so the app keeps showing no icon even though the bundle is fine
        # (a byte-identical copy elsewhere shows the correct icon). Renaming a
        # complete bundle into place keeps the first scan correct.
        staging="${OUT_DIR}/.docWebHub.app.staging"
        previous="${OUT_DIR}/.docWebHub.app.previous"
        rm -rf "$staging" "$previous"
        mkdir -p "${staging}/Contents/MacOS" "${staging}/Contents/Resources"
        cp "$output" "${staging}/Contents/MacOS/docwebhub"

        # The icon is generated on the fly from assets/logo.png (sips + iconutil).
        icon_entry=""
        if bash tools/make-icon.sh "${staging}/Contents/Resources/AppIcon.icns"; then
            icon_entry="    <key>CFBundleIconFile</key><string>AppIcon</string>"
        else
            echo "    hint: icon generation failed, using the default system icon" >&2
        fi

        cat > "${staging}/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key><string>docWebHub</string>
    <key>CFBundleDisplayName</key><string>docWebHub</string>
    <key>CFBundleIdentifier</key><string>dev.docwebhub.app</string>
    <key>CFBundleExecutable</key><string>docwebhub</string>
    <key>CFBundlePackageType</key><string>APPL</string>
    <key>CFBundleShortVersionString</key><string>${VERSION}</string>
    <key>CFBundleVersion</key><string>1.0</string>
${icon_entry}
    <key>LSMinimumSystemVersion</key><string>11.0</string>
    <key>NSHighResolutionCapable</key><true/>
    <key>NSPrincipalClass</key><string>NSApplication</string>
</dict>
</plist>
PLIST
        # Local ad-hoc signature: gives the bundle a stable identity (otherwise the
        # linker's a.out identity sticks), and lets the system remember permissions
        # and the icon per bundle id. Failure does not prevent running locally.
        if command -v codesign >/dev/null 2>&1 &&
            codesign --force --sign - --identifier dev.docwebhub.app "$staging" >/dev/null 2>&1; then
            echo "    signed (ad-hoc)"
        else
            echo "    hint: ad-hoc signing failed; the bundle still runs locally unsigned" >&2
        fi

        # Swap the finished bundle into place. Renaming keeps the whole thing on the
        # same volume, so the visible path only ever contains a complete bundle.
        if [ -d "$bundle" ]; then
            mv "$bundle" "$previous"
        fi
        mv "$staging" "$bundle"
        rm -rf "$previous"

        echo "    bundled ${bundle}"
    fi

    # Windows artifacts also get a zip so they are easy to hand out.
    if [ "$os" = "windows" ]; then
        if command -v zip >/dev/null 2>&1; then
            archive="${name}${ext}.zip"
            echo "    packaged ${OUT_DIR}/${archive}"
            ( cd "$OUT_DIR" && rm -f "$archive" && zip -q -9 -X "$archive" "${name}${ext}" )
        else
            echo "    skipped zip: zip not found" >&2
        fi
    fi

    # Linux builds also get a desktop entry; Terminal=false keeps a terminal from
    # popping up when the app is launched from a file manager.
    if [ "$os" = "linux" ]; then
        desktop="${OUT_DIR}/docwebhub.desktop"
        exec_path="$(cd "$(dirname "$output")" && pwd)/$(basename "$output")"

        icon_entry=""
        if [ -f assets/docwebhub.png ]; then
            cp assets/docwebhub.png "${OUT_DIR}/docwebhub.png"
            icon_entry="Icon=$(cd "$OUT_DIR" && pwd)/docwebhub.png"
        fi

        cat > "$desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=docWebHub
Comment=Local static site preview
Exec="${exec_path}"
${icon_entry}
Terminal=false
Categories=Development;
DESKTOP
        echo "    wrote ${desktop} (Exec/Icon point at local build paths; edit as needed)"
    fi
done

echo "done. output: $OUT_DIR"
