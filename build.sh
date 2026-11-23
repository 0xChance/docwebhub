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
# Packing changes into a staging directory, so every path that is used after
# that point has to be absolute.
OUT_DIR="$(cd "$OUT_DIR" && pwd)"

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

archives=()

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
            # cgo ties the binary to the C toolchain's architecture, not just
            # its operating system. Without this check the build falls through
            # to the host gcc, which then fails hundreds of lines deep with
            # "no such instruction" from the target's assembly.
            if [ "$arch" != "$HOST_ARCH" ]; then
                echo "error: building linux/$arch on a $HOST_ARCH host is a cross build" >&2
                echo "       it needs a $arch C toolchain plus $arch GTK 3 and Ayatana" >&2
                echo "       AppIndicator development files, which this script does not set up." >&2
                echo "       Build on a $arch machine, or in a $arch container." >&2
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

    # Linux builds also get a desktop entry; Terminal=false keeps a terminal from
    # popping up when the app is launched from a file manager.
    if [ "$os" = "linux" ]; then
        desktop="${OUT_DIR}/docwebhub.desktop"
        if [ -f assets/docwebhub.png ]; then
            cp assets/docwebhub.png "${OUT_DIR}/docwebhub.png"
        fi

        # Placeholders, not this machine's paths. The file ships inside the zip
        # next to the binary, and .desktop cannot use a path relative to
        # itself, so whoever installs it has to fill these in. See
        # docs/packaging.md.
        cat > "$desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=docWebHub
Comment=Local static site preview
Exec=/path/to/docwebhub
Icon=/path/to/docwebhub.png
Terminal=false
Categories=Development;
DESKTOP
        echo "    wrote ${desktop} (Exec/Icon are placeholders; edit after unzipping)"
    fi

    # ---- packaging -------------------------------------------------------
    # One zip per platform and architecture, named so the file is still
    # identifiable after it leaves the release page:
    #   docwebhub-<version>-<platform>-<arch>.zip
    # "macos" rather than Go's "darwin": the name is for whoever downloads it.
    platform="$os"
    if [ "$os" = "darwin" ]; then
        platform="macos"
    fi
    archive="${OUT_DIR}/docwebhub-${VERSION}-${platform}-${arch}.zip"

    case "$os" in
        darwin)
            # ditto, not zip: a .app is a bundle whose symlinks and permission
            # bits zip(1) would flatten, leaving an app macOS refuses to start.
            if command -v ditto >/dev/null 2>&1; then
                rm -f "$archive"
                ditto -c -k --keepParent "$bundle" "$archive"
                archives+=("$archive")
                echo "    packaged ${archive}"
            else
                echo "    skipped zip: ditto not found" >&2
            fi
            ;;
        *)
            if ! command -v zip >/dev/null 2>&1; then
                echo "    skipped zip: zip not found" >&2
            else
                # The binary loses its platform suffix inside the zip: the
                # archive name already says which platform it is.
                stage="${OUT_DIR}/.pack-${os}-${arch}"
                rm -rf "$stage"
                mkdir -p "$stage"
                case "$os" in
                    linux)
                        cp "$output" "$stage/docwebhub"
                        chmod +x "$stage/docwebhub"
                        cp "${OUT_DIR}/docwebhub.desktop" "$stage/docwebhub.desktop"
                        files="docwebhub docwebhub.desktop"
                        if [ -f "${OUT_DIR}/docwebhub.png" ]; then
                            cp "${OUT_DIR}/docwebhub.png" "$stage/docwebhub.png"
                            files="$files docwebhub.png"
                        fi
                        ;;
                    windows)
                        cp "$output" "$stage/docwebhub.exe"
                        files="docwebhub.exe"
                        ;;
                esac
                ( cd "$stage" && rm -f "$archive" && zip -q -9 -X "$archive" $files )
                rm -rf "$stage"
                archives+=("$archive")
                echo "    packaged ${archive}"
            fi
            ;;
    esac
done

# One checksum file for everything this run produced. macOS has no sha256sum,
# so fall back to shasum; a machine with neither simply skips it.
if [ ${#archives[@]} -gt 0 ]; then
    hashcmd=""
    if command -v sha256sum >/dev/null 2>&1; then
        hashcmd="sha256sum"
    elif command -v shasum >/dev/null 2>&1; then
        hashcmd="shasum -a 256"
    fi
    if [ -n "$hashcmd" ]; then
        : > "$OUT_DIR/SHA256SUMS"
        for a in "${archives[@]}"; do
            ( cd "$OUT_DIR" && $hashcmd "$(basename "$a")" ) >> "$OUT_DIR/SHA256SUMS"
        done
        echo "==> wrote ${OUT_DIR}/SHA256SUMS"
    else
        echo "    skipped checksums: neither sha256sum nor shasum found" >&2
    fi
fi

echo "done. output: $OUT_DIR"
