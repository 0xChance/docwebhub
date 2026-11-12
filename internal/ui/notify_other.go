// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

//go:build !windows

package ui

// NotifyUpdate is the entry point main uses to show a security advisory as a
// system notification. Only Windows routes it through here: a tray balloon
// needs the window the tray icon belongs to, and that window lives in this
// package. Linux and macOS have self-contained implementations in
// internal/update, so this stub exists only to keep main platform-neutral.
func NotifyUpdate(title, body string) bool { return false }
