// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

//go:build (!darwin || !cgo) && !windows

package i18n

import (
	"os"
	"strings"
)

// SystemLanguages returns the user's preferred languages, most preferred first,
// read from the POSIX locale environment.
func SystemLanguages() []string {
	var out []string

	add := func(raw string) {
		// LANGUAGE may hold a colon-separated preference list.
		for _, part := range strings.Split(raw, ":") {
			part = strings.TrimSpace(part)
			if part == "" || part == "C" || part == "POSIX" || part == "C.UTF-8" {
				continue
			}
			out = append(out, part)
		}
	}

	for _, name := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		add(os.Getenv(name))
	}
	return out
}
