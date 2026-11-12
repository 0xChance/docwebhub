// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// escapingSite builds a site folder holding one real file, plus a symlink to a
// file outside the root and a symlink to a directory outside the root.
func escapingSite(t *testing.T) (root, outside string) {
	t.Helper()

	root = t.TempDir()
	outside = t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "inside.txt"), []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "file-link")); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "dir-link")); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	return root, outside
}

// The static preview used to follow every symlink, so a link to a folder
// handed out the whole target tree. Both branches now share one resolver.
func TestEscapingSymlinkIsHiddenOnGet(t *testing.T) {
	root, _ := escapingSite(t)
	handler := newHandler(root, Options{})

	cases := []string{
		"/file-link",              // link to a file outside the root
		"/dir-link/",              // link to a folder outside the root
		"/dir-link/secret.txt",    // file reached through such a folder
		"/dir-link/../inside.txt", // cleaned back inside, must still work
	}
	want := []int{
		http.StatusNotFound,
		http.StatusNotFound,
		http.StatusNotFound,
		http.StatusOK,
	}

	for i, path := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want[i] {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, want[i])
		}
		if strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("GET %s leaked the target content: %q", path, rec.Body.String())
		}
	}
}

// The listing sits on the same branch, so it must not name what GET refuses.
func TestEscapingSymlinkIsOmittedFromListing(t *testing.T) {
	root, _ := escapingSite(t)
	handler := newHandler(root, Options{})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "inside.txt") {
		t.Fatalf("listing lost the real file: %s", body)
	}
	for _, name := range []string{"file-link", "dir-link"} {
		if strings.Contains(body, name) {
			t.Errorf("listing shows %q, which cannot be served", name)
		}
	}
}

// A link that stays inside the folder is a legitimate way to reuse a file, and
// both branches must keep serving it.
func TestSymlinkInsideRootIsServed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "page.html"), []byte("<p>ok</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, "alias.txt")); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "sub"), filepath.Join(root, "sub-link")); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}

	handler := newHandler(root, Options{})

	for _, path := range []string{"/alias.txt", "/sub-link/page.html"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Errorf("GET %s = %d, want 200 with a body", path, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if body := rec.Body.String(); !strings.Contains(body, "sub-link") {
		t.Errorf("listing dropped the in-root folder link: %s", body)
	}
}

// The folder being served is often reached through a symlink itself (/tmp on
// macOS, a volume, a symlinked home). Resolving the root must not make every
// request look like an escape.
func TestSiteRootBehindSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "page.html"), []byte("<p>ok</p>"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(t.TempDir(), "site")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}

	handler := newHandler(link, Options{})

	for _, path := range []string{"/", "/page.html"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s through a symlinked root = %d, want 200", path, rec.Code)
		}
	}
}

// A retired snapshot keeps its handle open while a request is still inside it,
// then closes it without waiting for the garbage collector.
func TestSiteRetireWaitsForRequests(t *testing.T) {
	current := newSite(t.TempDir(), Options{})
	t.Cleanup(func() { _ = current.files.Close() })

	if !current.acquire() {
		t.Fatal("a fresh snapshot is already retired")
	}

	current.retire()
	if current.files.self == nil {
		t.Fatal("handle closed while a request was still using it")
	}
	if current.acquire() {
		t.Fatal("a retired snapshot accepted a new request")
	}

	current.release()
	if current.files.self != nil {
		t.Fatal("handle not closed after the last request returned")
	}
}

func TestServerSetRootClosesOldSnapshot(t *testing.T) {
	srv := &Server{}
	old := newSite(t.TempDir(), Options{})
	srv.site.Store(old)

	srv.SetRoot(t.TempDir())

	if old.files.self != nil {
		t.Fatal("switching roots left the old handle open")
	}
	if srv.Root() == old.root {
		t.Fatal("SetRoot did not publish the new root")
	}
}
