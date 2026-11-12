// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

// Package assets embeds the images shipped with docWebHub, so a single binary
// still carries its own icon.
package assets

import _ "embed"

// IconPNG is the application icon, used by the Linux tray among other things.
//
//go:embed docwebhub.png
var IconPNG []byte
