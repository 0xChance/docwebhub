// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

// Package openurl opens URLs with the system default handler.
package openurl

import "errors"

// ErrUnsupported reports that this platform has no browser-launch implementation.
var ErrUnsupported = errors.New("automatic browser launch is not supported on this platform")
