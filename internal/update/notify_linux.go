// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

//go:build linux

package update

import (
	"log"
	"os/exec"
)

func init() {
	showNotice = func(title, body string) bool {
		// notify-send is the least inventive option and works on both X11 and
		// Wayland through the desktop's notification daemon. A missing binary
		// must never make the app fail: the page banner still carries the
		// advisory.
		if err := exec.Command("notify-send", "-u", "critical", "-a", "docWebHub", title, body).Run(); err != nil {
			log.Printf("update: cannot show a desktop notification: %v", err)
			return false
		}
		return true
	}
}
