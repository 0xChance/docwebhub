// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package assets

import "testing"

func TestIconPNGEmbedded(t *testing.T) {
	if len(IconPNG) < 1000 {
		t.Fatalf("docwebhub.png was not embedded (len=%d)", len(IconPNG))
	}
	if string(IconPNG[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("embedded icon is not a PNG")
	}
}
