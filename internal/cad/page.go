// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package cad

import _ "embed"

// viewerHTML is the preview page. It is a template: the interface language and
// the upstream version are filled in per request.
//
//go:embed viewer.html
var viewerHTML string
