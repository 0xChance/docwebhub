//go:build cad

// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package cad

import _ "embed"

// assets holds the viewer archive produced by tools/fetch-cad-viewer.sh.
// Building with -tags cad requires that file to exist; the build error points at
// the missing archive rather than letting a build silently ship without the
// feature.
//
//go:embed assets.zip
var assets []byte
