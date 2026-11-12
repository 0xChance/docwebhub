//go:build linux && cgo

// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package ui

/*
#cgo pkg-config: gtk+-3.0 ayatana-appindicator3-0.1
#include <stdlib.h>

int docwebhub_run_gui(void);
void docwebhub_set_tray_icon(const char *path);
*/
import "C"

import (
	"errors"
	"runtime"
	"unsafe"
)

// Run shows the native GTK window until the user closes it.
func Run(opts Options) error {
	// GTK must run on the main thread.
	runtime.LockOSThread()
	startForm(opts)

	cIcon := C.CString(prepareTrayIcon())
	defer C.free(unsafe.Pointer(cIcon))
	C.docwebhub_set_tray_icon(cIcon)

	if C.docwebhub_run_gui() != 0 {
		return errors.New("cannot initialize GTK; no usable graphical display")
	}
	return nil
}
