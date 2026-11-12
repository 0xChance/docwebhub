//go:build office

// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package office

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xChance/docwebhub/internal/i18n"
)

func newViewer(t *testing.T) *Viewer {
	t.Helper()
	v, err := New("")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return v
}

func get(t *testing.T, v *Viewer, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	v.Handler().ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestViewerServesThePreviewPage(t *testing.T) {
	v := newViewer(t)
	rec := get(t, v, http.MethodGet, pagePath)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "DocxScrollViewer") || !strings.Contains(body, "lwOffice") {
		t.Fatalf("the page does not look like the viewer: %s", body)
	}
}

func TestViewerServesAssetsWithPinnedTypes(t *testing.T) {
	v := newViewer(t)

	cases := []struct{ name, want string }{
		{"docx_parser_bg.wasm", "application/wasm"},
		{"docx.mjs", "text/javascript; charset=utf-8"},
		{"legacy/doc.mjs", "text/javascript; charset=utf-8"},
		{"VERSION", "application/octet-stream"},
	}

	for _, tc := range cases {
		rec := get(t, v, http.MethodGet, assetPrefix+tc.name)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d", tc.name, rec.Code)
			continue
		}
		if got := rec.Header().Get("Content-Type"); got != tc.want {
			t.Errorf("%s: Content-Type = %q, want %q", tc.name, got, tc.want)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s: empty body", tc.name)
		}
	}
}

// zip.Reader files do not implement io.Seeker, which is why the handler reads
// the entry into memory instead of mounting the archive with FileServerFS. A
// range request proves the choice still works.
func TestViewerSupportsRangeRequests(t *testing.T) {
	v := newViewer(t)

	req := httptest.NewRequest(http.MethodGet, assetPrefix+"docx_parser_bg.wasm", nil)
	req.Header.Set("Range", "bytes=0-1023")
	rec := httptest.NewRecorder()
	v.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); !strings.HasPrefix(got, "bytes 0-1023/") {
		t.Fatalf("Content-Range = %q", got)
	}
	if rec.Body.Len() != 1024 {
		t.Fatalf("body = %d bytes, want 1024", rec.Body.Len())
	}
}

func TestViewerMarksHashedChunksImmutable(t *testing.T) {
	v := newViewer(t)

	// Find a hashed chunk from the archive rather than hard-coding a name that
	// changes with every upstream release.
	var hashedName string
	for name := range v.files {
		if hashed.MatchString(name) {
			hashedName = name
			break
		}
	}
	if hashedName == "" {
		t.Skip("the archive has no content-hashed chunks")
	}

	if got := get(t, v, http.MethodGet, assetPrefix+hashedName).Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("hashed chunk Cache-Control = %q, want immutable", got)
	}
	if got := get(t, v, http.MethodGet, assetPrefix+"docx.mjs").Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("entry Cache-Control = %q, want no-cache", got)
	}
}

func TestViewerRejectsUnknownAndIllegalPaths(t *testing.T) {
	v := newViewer(t)

	for _, target := range []string{
		assetPrefix + "nope.mjs",
		assetPrefix + "../office.go",
		assetPrefix + "%2e%2e/office.go",
	} {
		if rec := get(t, v, http.MethodGet, target); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", target, rec.Code)
		}
	}
}

func TestViewerRejectsOtherMethods(t *testing.T) {
	v := newViewer(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := get(t, v, method, pagePath)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
		if rec.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("%s: Allow = %q", method, rec.Header().Get("Allow"))
		}
	}
}

func TestViewerRedirectsToThePageDirectory(t *testing.T) {
	v := newViewer(t)
	rec := get(t, v, http.MethodGet, strings.TrimSuffix(pagePath, "/"))

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != pagePath {
		t.Fatalf("Location = %q, want %q", got, pagePath)
	}
}

func TestViewerReportsItsVersion(t *testing.T) {
	v := newViewer(t)

	rec := get(t, v, http.MethodGet, versionPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if strings.TrimSpace(string(body)) != v.Version() {
		t.Fatalf("body = %q, version = %q", body, v.Version())
	}
	if v.Version() == "unknown" || v.Version() == "" {
		t.Fatalf("version = %q, the archive should carry one", v.Version())
	}
}

// -office-dir serves a directory instead of the archive, for trying a newer
// upstream build without recompiling.
func TestViewerServesFromADirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "legacy"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "VERSION"), []byte("9.9.9\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "docx.mjs"), []byte("export {};"), 0o644)
	os.WriteFile(filepath.Join(dir, "legacy", "doc.mjs"), []byte("export {};"), 0o644)

	v, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if v.Version() != "9.9.9" {
		t.Fatalf("version = %q, want 9.9.9", v.Version())
	}

	rec := get(t, v, http.MethodGet, assetPrefix+"legacy/doc.mjs")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.String() != "export {};" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

// The toolbar and the thumbnail rail live in the page itself; their labels have
// to follow the interface language, not the browser's.
func TestViewerPageCarriesTheZoomToolbar(t *testing.T) {
	i18n.SetLanguage("zh-CN")
	t.Cleanup(func() { i18n.SetLanguage("") })

	body := get(t, newViewer(t), http.MethodGet, pagePath).Body.String()
	// Expected labels are read from the table rather than spelled out, so this
	// file carries no translation of its own. Switching to a non-default
	// language is what makes the assertion meaningful: an untranslated toolbar
	// would still match the English text.
	for _, want := range []string{
		`id="toolbar"`, `id="thumbs"`, `id="zoom-in"`, `id="fit-page"`,
		i18n.T("web.office.zoomIn"), i18n.T("web.office.zoomOut"), i18n.T("web.office.pages"),
		// The PDF export is part of the same toolbar and has to be translated
		// with it, including the progress it reports per page.
		`id="export"`, i18n.T("web.office.exportPdf"),
		i18n.T("web.office.exportFailed"), i18n.T("web.office.exporting"),
	} {
		if want == "" {
			t.Fatal("a label key is missing from the translation table")
		}
		if !strings.Contains(body, want) {
			t.Errorf("the preview page is missing %q", want)
		}
	}
}
