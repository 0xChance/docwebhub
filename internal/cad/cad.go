// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

// Package cad serves the bundled CAD viewer: the static assets taken from
// assets.zip plus the preview page that drives them.
//
// The viewer is @mlightcad/cad-simple-viewer bundled into one ES module by
// tools/fetch-cad-viewer.sh. The assets are embedded in the binary (see
// embed_cad.go), so the program still ships as a single file. A build without
// the "cad" tag gets an empty implementation: Enabled reports false, the listing
// page falls back to "no preview", and nothing else changes.
//
// Everything here lives under Prefix, which the site never sees. The bytes are
// the application's own code and carry no user data, which is why the preview is
// reachable without the site password — see internal/web.
package cad

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/0xChance/docwebhub/internal/i18n"
)

// Prefix is the reserved namespace the viewer lives under. Requests below it are
// answered by this package and never reach the site folder.
const Prefix = "/__docwebhub/cad/"

const (
	pagePath    = Prefix
	versionPath = Prefix + "version"
	assetPrefix = Prefix
)

// Viewer serves one set of viewer assets, either from the embedded archive or
// from a directory given with -cad-dir.
type Viewer struct {
	version string
	files   map[string]*asset
}

// asset is one file, reachable either from the archive, from disk, or from bytes
// the server generated while starting up.
type asset struct {
	file *zip.File // embedded archive
	disk string    // directory override
	data []byte    // generated here, such as the merged font catalog
	name string
}

// The viewer resolves every text style name through this catalog before it draws
// a single glyph, fetching it from <baseUrl>/fonts/.
const (
	fontDir     = "data/fonts/"
	fontCatalog = fontDir + "fonts.json"
)

// fontTypes maps the extensions a CAD font folder is scanned for onto the type
// name the viewer uses. It parses SHX stroke fonts itself and hands outline
// fonts to opentype.js, which reads TrueType, CFF and WOFF.
var fontTypes = map[string]string{
	".shx":  "shx",
	".ttf":  "mesh",
	".otf":  "mesh",
	".woff": "mesh",
}

// shxEncoding is the double-byte encoding a few well-known SHX big fonts need.
// Without it a Chinese or Japanese SHX decodes to nonsense, and the file name is
// the only hint: the file itself does not say. Anything unlisted is left plain,
// which is right for the single-byte Western shapes.
var shxEncoding = map[string]string{
	"hztxt":    "gbk",
	"gbcbig":   "gbk",
	"gbhzfs":   "gbk",
	"tssdchn":  "gbk",
	"yjkchn":   "gbk",
	"pkpmchn":  "gbk",
	"intecad":  "gbk",
	"zjdz":     "gbk",
	"chineset": "big5",
	"extfont":  "shift-jis",
	"extfont2": "shift-jis",
	"bigfont":  "shift-jis",
}

// fontEntry is one catalog record, in the shape the viewer's FontInfo expects.
type fontEntry struct {
	File     string   `json:"file"`
	Name     []string `json:"name"`
	Type     string   `json:"type"`
	Encoding string   `json:"encoding,omitempty"`
}

// Enabled reports whether this build carries the viewer assets.
func Enabled() bool { return len(assets) > 0 }

// New prepares a viewer. An empty dir uses the embedded archive; a non-empty one
// serves that directory instead, for trying a newer upstream build without
// recompiling.
//
// fontDir is optional: when set, the fonts in it are served alongside whatever
// ships with the viewer, and their names take precedence in the catalog. It is
// the way to see a drawing in the font it was drawn with — this build cannot
// redistribute the SHX set or the Windows fonts drawings usually name.
func New(dir, fontDir string) (*Viewer, error) {
	v := &Viewer{files: make(map[string]*asset)}

	switch {
	case dir != "":
		if err := v.loadDir(dir); err != nil {
			return nil, err
		}
	case len(assets) == 0:
		return nil, errors.New("this build has no CAD viewer (rebuild with -tags cad)")
	default:
		if err := v.loadArchive(assets); err != nil {
			return nil, err
		}
	}

	if fontDir != "" {
		if err := v.addFonts(fontDir); err != nil {
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

	switch r.URL.Path {
	case pagePath, strings.TrimSuffix(pagePath, "/"):
		v.servePage(w, r)
	case versionPath:
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
	h.Set("Cache-Control", "no-cache")
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

// contentType is pinned instead of taken from mime.TypeByExtension: the viewer
// is an ES module and the worker is spawned as one, so a text/plain answer would
// make the browser refuse both.
func contentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".mjs", ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".wasm":
		return "application/wasm"
	case ".svg":
		return "image/svg+xml"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".shx":
		return "application/octet-stream"
	default:
		return "application/octet-stream"
	}
}

func (v *Viewer) read(name string) ([]byte, bool) {
	e, ok := v.files[name]
	if !ok {
		return nil, false
	}

	if e.data != nil {
		return e.data, true
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
		return fmt.Errorf("cad asset folder: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("cad asset path is not a folder: %s", abs)
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

// addFonts serves every font file in dir under data/fonts/ and rewrites the
// catalog so the viewer can find them by name.
//
// A fonts.json beside them is honoured as written: it is how to declare an
// encoding or extra aliases a file name cannot carry. Files it does not mention
// are added anyway, named after themselves. Entries that came with the viewer
// stay unless the folder claims the same name, so a drawing asking for a font
// nobody supplied still gets the open substitute it would have got otherwise.
func (v *Viewer) addFonts(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("cad font folder: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("cad font path is not a folder: %s", abs)
	}

	var entries []fontEntry
	declared := make(map[string]bool)

	if data, err := os.ReadFile(filepath.Join(abs, "fonts.json")); err == nil {
		var written []fontEntry
		if err := json.Unmarshal(data, &written); err != nil {
			return fmt.Errorf("read the font catalog in %s: %w", abs, err)
		}
		for _, e := range written {
			if e.File == "" || len(e.Name) == 0 {
				continue
			}
			base := filepath.Base(e.File)
			if _, err := os.Stat(filepath.Join(abs, base)); err != nil {
				return fmt.Errorf("cad font folder: %s is listed in fonts.json but missing", base)
			}
			if e.Type == "" {
				e.Type = fontType(base)
			}
			e.File = base
			entries = append(entries, e)
			declared[strings.ToLower(base)] = true
		}
	}

	listed, err := os.ReadDir(abs)
	if err != nil {
		return fmt.Errorf("cad font folder: %w", err)
	}
	for _, d := range listed {
		if d.IsDir() {
			continue
		}
		base := d.Name()
		kind, ok := fontTypes[strings.ToLower(filepath.Ext(base))]
		if !ok || declared[strings.ToLower(base)] {
			continue
		}
		// A style name is the file name without its extension, which is also how
		// the name is spelled inside a drawing.
		style := strings.TrimSuffix(base, filepath.Ext(base))
		e := fontEntry{File: base, Name: []string{style}, Type: kind}
		if kind == "shx" {
			e.Encoding = shxEncoding[strings.ToLower(style)]
		}
		entries = append(entries, e)
	}

	if len(entries) == 0 {
		return fmt.Errorf("cad font folder has no fonts: %s", abs)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].File < entries[j].File })

	for _, e := range entries {
		name := fontDir + e.File
		v.files[name] = &asset{disk: filepath.Join(abs, e.File), name: name}
	}

	merged, err := json.MarshalIndent(v.mergeCatalog(entries), "", "  ")
	if err != nil {
		return fmt.Errorf("write the font catalog: %w", err)
	}
	v.files[fontCatalog] = &asset{data: merged, name: fontCatalog}
	return nil
}

// mergeCatalog puts the folder's entries in front of the ones that came with the
// viewer and drops shipped names the folder now answers for. The viewer looks
// names up in lowercase, so that is how a collision is detected.
func (v *Viewer) mergeCatalog(front []fontEntry) []fontEntry {
	merged := append([]fontEntry{}, front...)

	claimed := make(map[string]bool)
	for _, e := range front {
		for _, n := range e.Name {
			claimed[strings.ToLower(n)] = true
		}
	}

	for _, e := range v.shippedCatalog() {
		kept := make([]string, 0, len(e.Name))
		for _, n := range e.Name {
			if !claimed[strings.ToLower(n)] {
				kept = append(kept, n)
			}
		}
		if len(kept) == 0 {
			continue
		}
		e.Name = kept
		merged = append(merged, e)
	}
	return merged
}

// shippedCatalog is the catalog that came with the viewer. An unreadable one is
// not an error: it only means the folder's fonts are all that can be offered.
func (v *Viewer) shippedCatalog() []fontEntry {
	data, ok := v.read(fontCatalog)
	if !ok {
		return nil
	}
	var entries []fontEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}
	return entries
}

func fontType(file string) string {
	if kind, ok := fontTypes[strings.ToLower(filepath.Ext(file))]; ok {
		return kind
	}
	return "mesh"
}

var pageTemplate = template.Must(template.New("cad").Parse(viewerHTML))

func renderPage(version, lang string) ([]byte, error) {
	// The page has almost no chrome of its own; only the two states the viewer
	// cannot report by itself (loading, and a file it refuses) need words.
	var buf bytes.Buffer
	text := map[string]string{
		"Version":      version,
		"Lang":         lang,
		"Loading":      i18n.T("web.cad.loading"),
		"Failed":       i18n.T("web.cad.failed"),
		"ExportPdf":    i18n.T("web.cad.exportPdf"),
		"ExportFailed": i18n.T("web.cad.exportFailed"),
	}
	if err := pageTemplate.Execute(&buf, text); err != nil {
		return nil, fmt.Errorf("render the CAD preview page: %w", err)
	}
	return buf.Bytes(), nil
}
