//go:build darwin

// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package openurl

import "os/exec"

// Open opens rawURL in the system default browser.
func Open(rawURL string) error {
	return exec.Command("open", rawURL).Start()
}
