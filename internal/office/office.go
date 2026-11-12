// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

// Package office serves the bundled Office document viewer: the static assets
// taken from assets.zip plus the preview page that drives them.
//
// The assets are embedded in the binary (see embed_office.go), so the program
// still ships as a single file. A build without the "office" tag gets an empty
// implementation: Enabled reports false, the listing page falls back to "no
// preview", and nothing else changes.
//
// Everything here lives under Prefix, which the site never sees. The bytes are
// the application's own code and carry no user data, which is why the preview
// is reachable without the site password — see internal/web.
package office

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/0xChance/docwebhub/internal/i18n"
)

// Prefix is the reserved namespace the viewer lives under. Requests below it
// are answered by this package and never reach the site folder.
const Prefix = "/__docwebhub/"

const (
	pagePath    = Prefix + "office/"
	versionPath = Prefix + "office/version"
	assetPrefix = Prefix + "office/"
)

// Viewer serves one set of viewer assets, either from the embedded archive or
// from a directory given with -office-dir.
type Viewer struct {
	version string
	files   map[string]*asset
}

// asset is one file, reachable either from the archive or from disk.
type asset struct {
	file *zip.File // embedded archive
	disk string    // directory override
	name string
}

// Enabled reports whether this build carries the viewer assets.
func Enabled() bool { return len(assets) > 0 }

// New prepares a viewer. An empty dir uses the embedded archive; a non-empty
// one serves that directory instead, for trying a newer upstream build without
// recompiling.
func New(dir string) (*Viewer, error) {
	v := &Viewer{files: make(map[string]*asset)}

	switch {
	case dir != "":
		if err := v.loadDir(dir); err != nil {
			return nil, err
		}
	case len(assets) == 0:
		return nil, errors.New("this build has no Office viewer (rebuild with -tags office)")
	default:
		if err := v.loadArchive(assets); err != nil {
			return nil, err
		}
	}

	v.version = v.readVersion()
	return v, nil
}

// Version returns the upstream viewer version baked into this build.
func (v *Viewer) Version() string { return v.version }

// Handler serves the preview page and every asset below Prefix.
func (v *Viewer) Handler() http.Handler { return http.HandlerFunc(v.serveHTTP) }

func (v *Viewer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch {
	case r.URL.Path == assetPrefix[:len(assetPrefix)-1]:
		// Redirect so relative imports inside the page resolve against a
		// directory URL.
		http.Redirect(w, r, pagePath, http.StatusMovedPermanently)
	case r.URL.Path == pagePath:
		v.servePage(w, r)
	case r.URL.Path == versionPath:
		v.serveVersion(w, r)
	default:
		v.serveAsset(w, r, strings.TrimPrefix(r.URL.Path, assetPrefix))
	}
}

func (v *Viewer) servePage(w http.ResponseWriter, r *http.Request) {
	// Rendered per request so the page picks up the interface language that is
	// active now, not the one that was active when the server started.
	page, err := renderPage(v.version, i18n.ActiveCode())
	if err != nil {
		http.Error(w, "cannot render the preview page", http.StatusInternalServerError)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Content-Length", fmt.Sprint(len(page)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(page)
}

func (v *Viewer) serveVersion(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Access-Control-Allow-Origin", "*")
	_, _ = io.WriteString(w, v.version+"\n")
}

func (v *Viewer) serveAsset(w http.ResponseWriter, r *http.Request, name string) {
	cleaned, ok := cleanName(name)
	if !ok {
		http.NotFound(w, r)
		return
	}

	data, ok := v.read(cleaned)
	if !ok {
		http.NotFound(w, r)
		return
	}

	h := w.Header()
	h.Set("Content-Type", contentType(cleaned))
	h.Set("Cache-Control", cacheControl(cleaned))
	h.Set("Access-Control-Allow-Origin", "*")
	// The assets only change when the binary does, so the version plus the file
	// name is a perfect validator. Serving without a modification time keeps a
	// stale entry from being answered with 304: the timestamps inside the npm
	// archive are all the same fixed date.
	h.Set("Etag", `"`+v.version+`-`+cleaned+`"`)
	http.ServeContent(w, r, cleaned, time.Time{}, bytes.NewReader(data))
}

// cleanName rejects anything that is not a plain relative path inside the
// archive. The archive itself is trusted, but the name arrives from the URL.
func cleanName(name string) (string, bool) {
	if name == "" || strings.Contains(name, "..") {
		return "", false
	}
	cleaned := strings.TrimPrefix(path.Clean("/"+name), "/")
	if cleaned == "" || cleaned == "." {
		return "", false
	}
	return cleaned, true
}

// hashed matches the bundler's content-hashed chunk names, which may be cached
// forever; everything else has to be revalidated.
var hashed = regexp.MustCompile(`^.*-[A-Za-z0-9_-]{8}\.(?:m?js|css)$`)

func cacheControl(name string) string {
	if hashed.MatchString(path.Base(name)) {
		return "private, max-age=31536000, immutable"
	}
	return "no-cache"
}

// contentType is pinned instead of taken from mime.TypeByExtension: the host's
// mime database is not guaranteed to know .wasm, and a wrong type there makes
// the browser refuse to instantiate the module.
func contentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".wasm":
		return "application/wasm"
	case ".mjs", ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}

func (v *Viewer) read(name string) ([]byte, bool) {
	e, ok := v.files[name]
	if !ok {
		return nil, false
	}

	if e.file == nil {
		data, err := os.ReadFile(e.disk)
		return data, err == nil
	}

	rc, err := e.file.Open()
	if err != nil {
		return nil, false
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	return data, err == nil
}

func (v *Viewer) readVersion() string {
	if data, ok := v.read("VERSION"); ok {
		return strings.TrimSpace(string(data))
	}
	return "unknown"
}

func (v *Viewer) loadArchive(data []byte) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("read the embedded viewer archive: %w", err)
	}

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.TrimPrefix(path.Clean("/"+f.Name), "/")
		if name == "" || name == "." {
			continue
		}
		v.files[name] = &asset{file: f, name: name}
	}
	return nil
}

// loadDir serves a directory instead of the archive. The path comes from the
// command line, not from a request, so a plain containment check is enough.
func (v *Viewer) loadDir(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("office asset folder: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("office asset path is not a folder: %s", abs)
	}

	return filepath.WalkDir(abs, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(abs, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		v.files[name] = &asset{disk: p, name: name}
		return nil
	})
}

var pageTemplate = template.Must(template.New("office").Parse(viewerHTML))

func renderPage(version, lang string) ([]byte, error) {
	// The page carries its own toolbar, so a few strings have to be in the
	// active language; everything else in the document comes from the viewer.
	var buf bytes.Buffer
	text := map[string]string{
		"Version":    version,
		"Lang":       lang,
		"ZoomIn":     i18n.T("web.office.zoomIn"),
		"ZoomOut":    i18n.T("web.office.zoomOut"),
		"FitWidth":   i18n.T("web.office.fitWidth"),
		"FitPage":    i18n.T("web.office.fitPage"),
		"PageLabel":  i18n.T("web.office.page"),
		"PagesLabel": i18n.T("web.office.pages"),
		// The export button and the progress it reports while the pages are
		// being rasterised.
		"ExportPdf":    i18n.T("web.office.exportPdf"),
		"Exporting":    i18n.T("web.office.exporting"),
		"ExportFailed": i18n.T("web.office.exportFailed"),
	}
	if err := pageTemplate.Execute(&buf, text); err != nil {
		return nil, fmt.Errorf("render the office preview page: %w", err)
	}
	return buf.Bytes(), nil
}
