// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package web

import (
	"io/fs"
	"path/filepath"
	"sync"
	"time"
)

const (
	// countTTL is how long a walk result is reused. The overview shows a count,
	// not a live figure, so re-walking a large folder on every window open would
	// cost seconds to answer a question nobody asks twice a minute.
	countTTL = 60 * time.Second

	// countLimit stops the walk on enormous trees. Past this the number is
	// reported as "N+" rather than pretending to be exact.
	countLimit = 50000
)

// fileCount caches one walk of one root.
type fileCount struct {
	mu        sync.Mutex
	root      string
	files     int
	dirs      int
	truncated bool
	at        time.Time
}

// Count returns how many files and folders the tree holds, stopping early on
// very large trees. The second value reports whether the walk was cut short.
func (c *fileCount) count(root string) (files, dirs int, truncated bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.root == root && !c.at.IsZero() && time.Since(c.at) < countTTL {
		return c.files, c.dirs, c.truncated
	}

	files, dirs, truncated = 0, 0, false
	seen := 0
	// WalkDir does not follow symlinks, so a link out of the folder cannot pull
	// the whole filesystem into the count.
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A folder the user cannot read is not a reason to fail the count.
			if d != nil && d.IsDir() && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		seen++
		if seen > countLimit {
			truncated = true
			return fs.SkipAll
		}
		if d.IsDir() {
			dirs++
		} else {
			files++
		}
		return nil
	})

	c.root, c.files, c.dirs, c.truncated, c.at = root, files, dirs, truncated, time.Now()
	return files, dirs, truncated
}

// CountTree returns the file and folder counts for the folder currently served.
func (s *Server) CountTree() (files, dirs int, truncated bool) {
	return s.counts.count(s.Root())
}
