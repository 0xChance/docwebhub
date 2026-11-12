// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package web

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// siteRoot is the single place that turns a URL path into a path on disk.
//
// The static preview (http.FileServerFS) and the listing page both read
// through it, so a symlink that points outside the site folder
// is refused by every branch instead of only one.
//
// The confinement comes from os.Root: it resolves a path in the kernel and
// stops at the first symlink that would leave the folder. That keeps the
// common case at one stat — a userspace filepath.EvalSymlinks walk costs about
// ten times as much and allocates 40 times more — and it removes the window
// between "check" and "open" that such a walk always leaves open.
type siteRoot struct {
	root string // absolute path as configured, used to build request paths
	// resolved is root with its own symlinks resolved. The folder being served
	// often sits behind one (/tmp on macOS, an external volume, a symlinked
	// home), and only the resolved form can be compared with a symlink target.
	resolved string
	// self is the confined handle. It is nil when the folder cannot be opened
	// (it was deleted, or the path is not a directory); every lookup then
	// reports "not found" and the handler answers 404.
	//
	// The server closes the handle explicitly once a retired snapshot has no
	// requests left; the finalizer is only the backstop for handlers used
	// outside the server (tests).
	self     *os.Root
	closeOne sync.Once
	closeErr error
}

func newSiteRoot(root string) *siteRoot {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = filepath.Clean(root)
	}

	s := &siteRoot{root: abs, resolved: abs}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		s.resolved = resolved
	}
	if handle, err := os.OpenRoot(abs); err == nil {
		s.self = handle
	}
	return s
}

// Close releases the confined handle. It is safe to call more than once.
// Callers must first make sure no request can still reach this root.
func (s *siteRoot) Close() error {
	s.closeOne.Do(func() {
		if s.self != nil {
			s.closeErr = s.self.Close()
			s.self = nil
		}
	})
	return s.closeErr
}

// Root returns the folder being served, without resolving symlinks.
func (s *siteRoot) Root() string { return s.root }

// rel turns a URL path into the name os.Root expects: slash separated,
// relative, and free of "..".
func (s *siteRoot) rel(urlPath string) (string, error) {
	cleaned := path.Clean("/" + strings.TrimPrefix(urlPath, "/"))
	rel := strings.TrimPrefix(cleaned, "/")
	if rel == "" {
		return ".", nil
	}
	if !fs.ValidPath(rel) {
		return "", os.ErrNotExist
	}
	return rel, nil
}

// open returns the path on disk and its metadata. A path that a symlink would
// take outside the folder is reported as missing, so it is hidden rather than
// refused.
func (s *siteRoot) open(urlPath string) (string, os.FileInfo, error) {
	if s.self == nil {
		return "", nil, os.ErrNotExist
	}

	name, err := s.rel(urlPath)
	if err != nil {
		return "", nil, err
	}

	info, statErr := s.self.Stat(name)
	if statErr == nil {
		return filepath.Join(s.root, filepath.FromSlash(name)), info, nil
	}

	// os.Root refuses an absolute symlink even when it points back inside the
	// folder, and linking an asset that lives elsewhere by absolute path is a
	// normal way to preview it. Pay for the walk only when the fast path said
	// no; a path that really is outside stays refused below.
	if real, info, ok := s.absoluteLink(name); ok {
		return real, info, nil
	}
	return "", nil, s.hide(statErr, name)
}

// Open implements fs.FS for http.FileServerFS and the listing page.
func (s *siteRoot) Open(name string) (fs.File, error) {
	if s.self == nil {
		return nil, os.ErrNotExist
	}

	file, err := s.self.Open(name)
	if err == nil {
		return file, nil
	}
	if real, _, ok := s.absoluteLink(name); ok {
		return os.Open(real)
	}
	return nil, s.hide(err, name)
}

// absoluteLink resolves the one case os.Root will not take: a symlink written
// with an absolute target that still lands inside the folder.
func (s *siteRoot) absoluteLink(name string) (string, os.FileInfo, bool) {
	if name == "." || !fs.ValidPath(name) {
		return "", nil, false
	}

	real, err := filepath.EvalSymlinks(filepath.Join(s.root, filepath.FromSlash(name)))
	if err != nil || !withinRoot(s.resolved, real) {
		return "", nil, false
	}

	info, err := os.Stat(real)
	if err != nil {
		return "", nil, false
	}
	return real, info, true
}

// hide reports a path that exists but was refused as missing, which is what the
// rest of the app expects for something it will not serve.
//
// os.Root signals an escaping symlink with its own error rather than
// fs.ErrNotExist, and the file server would turn that into a 500. If the path
// is on disk, the refusal can only come from the confinement; a genuinely
// absent path keeps its own error so the 404 stays accurate.
func (s *siteRoot) hide(err error, name string) error {
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		return err
	}
	if _, lstatErr := os.Lstat(filepath.Join(s.root, filepath.FromSlash(name))); lstatErr == nil {
		return os.ErrNotExist
	}
	return err
}

// withinRoot reports whether target stays inside root. Both must be resolved
// paths, so the comparison is purely lexical.
func withinRoot(root, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// dir reports whether urlPath points to an existing directory, and returns the
// cleaned URL path for it.
func (s *siteRoot) dir(urlPath string) (string, bool) {
	_, info, err := s.open(urlPath)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return path.Clean("/" + urlPath), true
}

// file reports whether name exists and is not a directory.
func (s *siteRoot) file(name string) bool {
	_, info, err := s.open(name)
	return err == nil && !info.IsDir()
}
