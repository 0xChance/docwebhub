//go:build cad

// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package cad

import (
	"encoding/json"
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
	v, err := New("", "")
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

	rec := get(t, v, http.MethodGet, Prefix)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("content type = %q, want text/html", got)
	}

	body := rec.Body.String()
	// The page must load the bundle and take part in the handshake, otherwise
	// the listing pane would wait for a viewer that never answers. The export
	// button ships hidden and only shows once a drawing is actually open.
	for _, want := range []string{"./viewer.js", "./pdf.js", "lwCad", "AcApDocManager", `id="export"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}

func TestViewerServesThePageWithoutTheTrailingSlash(t *testing.T) {
	v := newViewer(t)

	rec := get(t, v, http.MethodGet, strings.TrimSuffix(Prefix, "/"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestViewerPageFollowsTheActiveLanguage(t *testing.T) {
	v := newViewer(t)

	i18n.SetLanguage("en")
	english := get(t, v, http.MethodGet, Prefix).Body.String()

	i18n.SetLanguage("zh-CN")
	chinese := get(t, v, http.MethodGet, Prefix).Body.String()
	i18n.SetLanguage("")

	if english == chinese {
		t.Fatal("the page did not change with the language")
	}
	if !strings.Contains(chinese, i18n.T("web.cad.failed")) {
		t.Error("the page does not carry the translated failure message")
	}
}

func TestViewerServesTheBundleAndWorker(t *testing.T) {
	v := newViewer(t)

	for _, name := range []string{"viewer.js", "assets/mtext-renderer-worker.js"} {
		rec := get(t, v, http.MethodGet, Prefix+name)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", name, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/javascript") {
			t.Errorf("%s: content type = %q, want text/javascript", name, got)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s: empty body", name)
		}
	}
}

func TestViewerReportsItsVersion(t *testing.T) {
	v := newViewer(t)

	rec := get(t, v, http.MethodGet, versionPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != v.Version() {
		t.Fatalf("version = %q, want %q", got, v.Version())
	}
}

func TestViewerAcceptsHead(t *testing.T) {
	v := newViewer(t)

	rec := get(t, v, http.MethodHead, Prefix)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatal("HEAD answered with a body")
	}
}

func TestViewerRejectsOtherMethods(t *testing.T) {
	v := newViewer(t)

	rec := get(t, v, http.MethodPost, Prefix)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestViewerRefusesPathTraversal(t *testing.T) {
	v := newViewer(t)

	for _, target := range []string{Prefix + "../cad.go", Prefix + "assets/../../cad.go"} {
		rec := get(t, v, http.MethodGet, target)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", target, rec.Code)
		}
	}
}

func TestViewerServesADirectoryInstead(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "viewer.js"), []byte("export const x = 1;"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("9.9.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := New(dir, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if v.Version() != "9.9.9" {
		t.Fatalf("version = %q, want 9.9.9", v.Version())
	}

	rec := get(t, v, http.MethodGet, Prefix+"viewer.js")
	if rec.Code != http.StatusOK || rec.Body.String() != "export const x = 1;" {
		t.Fatalf("override file: status %d body %q", rec.Code, rec.Body.String())
	}
}

func TestViewerServesTheShippedFontCatalog(t *testing.T) {
	v := newViewer(t)

	rec := get(t, v, http.MethodGet, Prefix+fontCatalog)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Every name a drawing is likely to ask for has to resolve to a file that is
	// actually shipped, or the viewer reports the font as missing again.
	shipped := map[string]bool{}
	var catalog []fontEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("the catalog is not JSON: %v", err)
	}
	for _, e := range catalog {
		if r := get(t, v, http.MethodGet, Prefix+fontDir+e.File); r.Code != http.StatusOK {
			t.Errorf("%s is listed but not served (status %d)", e.File, r.Code)
		}
		shipped[e.File] = true
		if len(e.Name) == 0 || e.Type == "" {
			t.Errorf("%s: incomplete entry %+v", e.File, e)
		}
	}
	if len(catalog) == 0 {
		t.Fatal("the archive ships an empty font catalog")
	}
	if !shipped["noto-sans.ttf"] || !shipped["noto-sans-sc.otf"] {
		t.Fatalf("expected both vendored fonts, got %v", shipped)
	}
}

func TestViewerServesAUserFontFolderOnTop(t *testing.T) {
	dir := t.TempDir()
	// A hand-written catalog is honoured verbatim, including the encoding a file
	// name cannot express.
	written := `[{"file":"hztxt.shx","name":["hztxt","hz"],"type":"shx","encoding":"gbk"}]`
	if err := os.WriteFile(filepath.Join(dir, "fonts.json"), []byte(written), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hztxt.shx"), []byte("shx-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A file nobody described is still picked up, named after itself.
	if err := os.WriteFile(filepath.Join(dir, "romans.shx"), []byte("romans-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Anything else in the folder is not a font and must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	v, err := New("", dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := get(t, v, http.MethodGet, Prefix+fontDir+"hztxt.shx")
	if rec.Code != http.StatusOK || rec.Body.String() != "shx-bytes" {
		t.Fatalf("user font: status %d body %q", rec.Code, rec.Body.String())
	}

	rec = get(t, v, http.MethodGet, Prefix+fontCatalog)
	var catalog []fontEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("the merged catalog is not JSON: %v", err)
	}

	byName := map[string]fontEntry{}
	for _, e := range catalog {
		for _, n := range e.Name {
			byName[strings.ToLower(n)] = e
		}
	}
	if got := byName["hztxt"]; got.File != "hztxt.shx" || got.Encoding != "gbk" || got.Type != "shx" {
		t.Errorf("hztxt = %+v, want the declared SHX entry", got)
	}
	if got := byName["romans"]; got.File != "romans.shx" || got.Type != "shx" || got.Encoding != "" {
		t.Errorf("romans = %+v, want a scanned SHX entry without an encoding", got)
	}
	// The shipped catalog keeps answering the names the folder does not claim.
	if got := byName["arial"]; !strings.HasSuffix(got.File, ".ttf") && !strings.HasSuffix(got.File, ".otf") {
		t.Errorf("arial = %+v, want the shipped substitute", got)
	}
	// And yields where the folder does: hztxt used to be one of its names.
	if got := byName["hztxt"].File; shippedFileFor(t, newViewer(t), "hztxt") != "" && got == shippedFileFor(t, newViewer(t), "hztxt") {
		t.Errorf("hztxt still resolves to the shipped font %q", got)
	}
	if _, ok := byName["readme"]; ok {
		t.Error("a non-font file was added to the catalog")
	}
}

// shippedFileFor is the file the bundled catalog maps a name onto.
func shippedFileFor(t *testing.T, v *Viewer, name string) string {
	t.Helper()
	for _, e := range v.shippedCatalog() {
		for _, n := range e.Name {
			if strings.EqualFold(n, name) {
				return e.File
			}
		}
	}
	return ""
}

func TestViewerRefusesAnEmptyFontFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New("", dir); err == nil {
		t.Fatal("a folder with no fonts was accepted")
	}
}

func TestVersionIsPinnedToTheUpstreamRelease(t *testing.T) {
	v := newViewer(t)
	if v.Version() == "unknown" {
		t.Fatal("the archive carries no VERSION file")
	}
}
