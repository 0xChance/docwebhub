//go:build !office

// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package office

import "testing"

// A plain `go build` must produce a working program that simply has no viewer.
// The listing page relies on Enabled to fall back to "no preview".
func TestBuildWithoutAssetsIsDisabled(t *testing.T) {
	if Enabled() {
		t.Fatal("Enabled reports true in a build without the office tag")
	}

	if _, err := New(""); err == nil {
		t.Fatal("New succeeded without embedded assets")
	}
}
