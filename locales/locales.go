// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

// Package locales embeds the translation files shipped with docWebHub, so a
// single binary still carries every language.
//
// The JSON files next to this one are the source of truth for all user-visible
// text. See README.md in this directory for the file format and for how to add
// or translate a language.
package locales

import "embed"

// FS holds every bundled translation file, keyed by file name (for example
// "zh-CN.json").
//
//go:embed *.json
var FS embed.FS
