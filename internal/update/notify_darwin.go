// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

//go:build darwin

package update

import (
	"log"
	"os/exec"
)

func init() {
	showNotice = func(title, body string) bool {
		// osascript reaches Notification Center without linking
		// UserNotifications, and it respects Do Not Disturb. The script is built
		// from quoted Go strings, so no shell escaping is involved.
		script := `display notification "` + escapeAppleScript(body) + `" with title "` + escapeAppleScript(title) + `"`
		if err := exec.Command("osascript", "-e", script).Run(); err != nil {
			log.Printf("update: cannot show a notification: %v", err)
			return false
		}
		return true
	}
}

// escapeAppleScript keeps a quote in a translation from ending the string.
func escapeAppleScript(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch r {
		case '"', '\\':
			out = append(out, '\\', r)
		case '\n', '\r':
			out = append(out, ' ')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}
