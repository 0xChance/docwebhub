//go:build !office

// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package office

// assets is empty in a build without the office tag. Enabled reports false and
// the listing page falls back to its usual "no preview" state, so a plain
// `go build` or `go test` needs no downloaded assets.
var assets []byte
