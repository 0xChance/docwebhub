// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package ui

var quitHook func()

// SetQuitHook registers the cleanup function that runs before the GUI terminates
// the process on its own initiative.
//
// On macOS the NSApplication quit path ends the process directly, so ui.Run never
// returns; the hook lets the web server close before the process exits, so
// in-flight requests are not interrupted. Run on Windows and Linux returns normally
// and the caller cleans up itself, so it does not need this hook.
func SetQuitHook(fn func()) {
	quitHook = fn
}

// notifyQuit runs the registered quit cleanup function, if any.
func notifyQuit() {
	if quitHook != nil {
		quitHook()
	}
}
