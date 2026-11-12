//go:build darwin && cgo

// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package ui

/*
#cgo LDFLAGS: -framework Cocoa

void docwebhub_run_gui(void);
void docwebhub_reply_terminate(void);
*/
import "C"

import (
	"runtime"
)

//export docwebhubRequestQuit
func docwebhubRequestQuit() {
	// Shut the server down on its own goroutine so the AppKit main-thread event
	// loop is not blocked; once cleanup is done, hop back to the main thread and
	// let the app terminate.
	go func() {
		notifyQuit()
		C.docwebhub_reply_terminate()
	}()
}

// Run shows the native macOS window until the user closes it.
func Run(opts Options) error {
	// The AppKit event loop must run on the process's main thread.
	runtime.LockOSThread()
	startForm(opts)
	C.docwebhub_run_gui()
	return nil
}
