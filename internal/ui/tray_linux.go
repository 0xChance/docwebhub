//go:build linux

// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package ui

import (
	"log"
	"os"
	"path/filepath"

	"github.com/0xChance/docwebhub/assets"
)

// prepareTrayIcon writes the app icon where the StatusNotifier host can read
// it. The path is absolute on purpose: Wayland bars (Quickshell on Omarchy,
// for example) display an icon whose name is a file path, the same way other
// Ayatana indicators on this desktop do. A theme name such as "web-browser"
// often resolves to nothing.
func prepareTrayIcon() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "docwebhub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("tray icon directory: %v", err)
		return ""
	}
	path := filepath.Join(dir, "docwebhub.png")
	if err := os.WriteFile(path, assets.IconPNG, 0o644); err != nil {
		log.Printf("tray icon file: %v", err)
		return ""
	}
	return path
}
