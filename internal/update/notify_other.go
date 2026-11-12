// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

//go:build !linux && !darwin

package update

// A system notification here needs a handle this package does not own: on
// Windows the tray icon belongs to the UI layer, and Shell_NotifyIcon cannot
// post a balloon without it. The work is delegated to whatever SetNotifier
// installed. Without one - a -no-gui build, say - only the page banner
// carries the advisory.
func init() {
	showNotice = func(title, body string) bool {
		if customShow == nil {
			return false
		}
		return customShow(title, body)
	}
}
