//go:build !darwin && !windows && !linux

// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package openurl

// Open is not implemented on this platform.
func Open(string) error {
	return ErrUnsupported
}
