//go:build (!darwin && !linux && !windows) || (linux && !cgo) || (darwin && !cgo)

// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package ui

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

// Run degrades to printing the details and waiting for Ctrl+C when no native GUI
// backend is available.
//
// Whether the browser opens automatically is up to the caller via the -open flag;
// nothing is opened implicitly here, so the same configuration never produces two
// browser tabs.
func Run(opts Options) error {
	log.Printf("no native GUI on this platform; the web server keeps running")
	log.Printf("  root: %s", opts.Root)
	log.Printf("  url:  %s", opts.URL)
	log.Printf("  press Ctrl+C to stop; add -open to launch the browser automatically")

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	signal.Stop(ch)
	return nil
}
