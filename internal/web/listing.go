// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package web

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/0xChance/docwebhub/internal/i18n"
)

//go:embed listing.html
var listingHTML string

var listingTemplate = template.Must(template.New("listing").Parse(listingHTML))

const (
	// listingPageSize bounds the rows one response renders. A large directory is
	// still read once, but the template only walks the visible page.
	listingPageSize = 100
	// listingCacheTTL keeps a scanned listing around while the user clicks
	// "load more". The directory mtime is checked as well, so an added or removed
	// file is noticed even inside the TTL.
	listingCacheTTL = time.Minute
	// The cache holds listings, not file contents. These limits bound the
	// strings kept for pages left open in a browser.
	listingCacheMaxEntries = 50000
	listingCacheMaxDirs    = 32
)

// listingSnapshot is one directory scan. The slice is immutable after put, so
// readers only need the cache mutex to look it up, not to render it.
type listingSnapshot struct {
	entries []listingEntry
	modTime time.Time
	created time.Time
}

// listingCache keeps the last few scans so "load more" does not turn into
// O(entries) again for every page. It is per site handler: switching the root
// throws it away with the old handler.
type listingCache struct {
	mu         sync.Mutex
	snapshots  map[string]*listingSnapshot
	total      int
	maxEntries int
	maxDirs    int
	ttl        time.Duration
}

func newListingCache() *listingCache {
	return &listingCache{
		snapshots:  make(map[string]*listingSnapshot),
		maxEntries: listingCacheMaxEntries,
		maxDirs:    listingCacheMaxDirs,
		ttl:        listingCacheTTL,
	}
}

// get returns a cached listing when the directory still has the same mtime.
// A miss, an expired snapshot, or a changed directory all report nil.
func (c *listingCache) get(path string, modTime time.Time) []listingEntry {
	c.mu.Lock()
	defer c.mu.Unlock()

	snapshot := c.snapshots[path]
	if snapshot == nil {
		return nil
	}
	if time.Since(snapshot.created) > c.ttl || !snapshot.modTime.Equal(modTime) {
		c.removeLocked(path)
		return nil
	}
	return snapshot.entries
}

func (c *listingCache) put(path string, modTime time.Time, entries []listingEntry) {
	if len(entries) > c.maxEntries {
		// One directory alone would fill the whole budget. Pagination still
		// works; it just rescans for each page instead of evicting everything.
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if old := c.snapshots[path]; old != nil {
		c.total -= len(old.entries)
	}
	c.snapshots[path] = &listingSnapshot{entries: entries, modTime: modTime, created: time.Now()}
	c.total += len(entries)
	c.evictLocked()
}

func (c *listingCache) removeLocked(path string) {
	if old := c.snapshots[path]; old != nil {
		c.total -= len(old.entries)
		delete(c.snapshots, path)
	}
}

func (c *listingCache) evictLocked() {
	now := time.Now()
	for path, snapshot := range c.snapshots {
		if now.Sub(snapshot.created) > c.ttl {
			c.removeLocked(path)
		}
	}
	for (c.total > c.maxEntries || len(c.snapshots) > c.maxDirs) && len(c.snapshots) > 0 {
		oldestPath := ""
		var oldest time.Time
		for path, snapshot := range c.snapshots {
			if oldestPath == "" || snapshot.created.Before(oldest) {
				oldestPath, oldest = path, snapshot.created
			}
		}
		c.removeLocked(oldestPath)
	}
}

type listingCrumb struct {
	Name string
	Href template.URL
}

type listingEntry struct {
	Name     string
	Href     template.URL
	Kind     string
	Bytes    int64
	IsDir    bool
	Hidden   bool
	Size     string
	Modified string
}

type listingPage struct {
	DisplayPath string
	Crumbs      []listingCrumb
	Parent      template.URL
	Entries     []listingEntry
	DirCount    int
	FileCount   int
	Summary     string
	// Total is the number of entries in the directory, not just this page.
	Total int
	// HasMore and NextPage drive the "load more" button.
	HasMore  bool
	NextPage int
	// Lang is the active language tag, used for the html lang attribute.
	Lang string
	// Strings is the active translation table; the template reads it by key.
	Strings map[string]string
	// StringsJSON is the same table for the client-side scripts.
	StringsJSON template.JS
	// PreviewJS is the URL of the highlighting/Markdown bundle.
	PreviewJS template.URL
	// UpdateData is the verified manifest for the scripts, inlined so the page
	// needs no second request. Empty when the update channel is off.
	UpdateData template.JS
	// UpdateJS is the loader URL, always same-origin via the app proxy.
	UpdateJS template.URL
}

// serveListing renders the custom directory listing page. opts carries which
// viewers this build has (files without one fall back to the plain "no preview"
// state) and the update channel, whose verified manifest is inlined here so the
// browser needs no second request to render banners.
//
// A request without "page" gets the full page with the first listingPageSize
// entries. "?page=N" (N >= 1) returns just that page's rows, which the script
// appends to the table. The snapshot behind those rows is what keeps "load
// more" from re-reading and re-statting the whole folder.
func serveListing(w http.ResponseWriter, r *http.Request, fsys fs.FS, dirPath string, opts Options, cache *listingCache) {
	pageNumber, fragment, err := listingRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	fsPath := strings.TrimPrefix(dirPath, "/")
	if fsPath == "" {
		fsPath = "."
	}

	dirInfo, err := fs.Stat(fsys, fsPath)
	if err != nil {
		listingError(w, err)
		return
	}

	var entries []listingEntry
	var dirs, files int
	fresh := pageNumber == 0
	if fresh {
		entries, dirs, files, err = readListingEntries(fsys, fsPath, dirPath, opts.Office != nil, opts.CAD != nil)
	} else {
		entries = cache.get(dirPath, dirInfo.ModTime())
		if entries == nil {
			entries, dirs, files, err = readListingEntries(fsys, fsPath, dirPath, opts.Office != nil, opts.CAD != nil)
			fresh = err == nil
		}
	}
	if err != nil {
		listingError(w, err)
		return
	}
	if fresh {
		cache.put(dirPath, dirInfo.ModTime(), entries)
	}

	total := len(entries)
	start := total
	if pageNumber <= total/listingPageSize {
		start = pageNumber * listingPageSize
	}
	end := start + listingPageSize
	if end > total {
		end = total
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if fragment {
		// The total travels with every fragment: a directory that changed since
		// the first page may have a different length, and the button needs it.
		w.Header().Set("X-Listing-Total", strconv.Itoa(total))
		for _, item := range entries[start:end] {
			if err := listingTemplate.ExecuteTemplate(w, "entry-row", item); err != nil {
				log.Printf("failed to render directory entry: %v", err)
				return
			}
		}
		return
	}

	page := listingPage{
		DisplayPath: dirPath,
		Crumbs:      crumbsFor(dirPath),
		Entries:     entries[start:end],
		DirCount:    dirs,
		FileCount:   files,
		Total:       total,
		HasMore:     end < total,
		NextPage:    pageNumber + 1,
		PreviewJS:   template.URL(previewAssetPath),
	}
	if dirPath != "/" {
		parent := path.Dir(dirPath)
		if parent == "." {
			parent = "/"
		}
		page.Parent = urlOf(withSlash(parent))
	}

	page.Summary = fmt.Sprintf("%s · %s",
		countText("web.listing.folder", page.DirCount),
		countText("web.listing.file", page.FileCount))
	page.Lang = i18n.ActiveCode()
	page.Strings = i18n.Snapshot()
	// The manifest is rendered inline rather than fetched: one less round trip,
	// and the page never shows a banner-free moment before the data arrives.
	if opts.Update != nil {
		if data := opts.Update.PageDataFor(opts.Version, page.Lang, ""); data != nil {
			if raw, err := json.Marshal(data); err == nil {
				page.UpdateData = template.JS(raw)
				page.UpdateJS = template.URL(updatePrefix + "v1.js")
			}
		}
	}
	if raw, err := json.Marshal(clientStrings(page.Strings)); err == nil {
		page.StringsJSON = template.JS(raw)
	}

	if err := listingTemplate.Execute(w, page); err != nil {
		log.Printf("failed to render directory listing: %v", err)
	}
}

func listingError(w http.ResponseWriter, err error) {
	if errors.Is(err, fs.ErrNotExist) {
		// A folder reached through a symlink that leaves the site root.
		http.Error(w, "404 page not found", http.StatusNotFound)
		return
	}
	http.Error(w, "cannot read directory: "+err.Error(), http.StatusInternalServerError)
}

// listingRequest reads the optional page number. page 0 is the full page;
// anything else asks for one fragment of rows.
func listingRequest(r *http.Request) (page int, fragment bool, err error) {
	raw := r.URL.Query().Get("page")
	if raw == "" {
		return 0, false, nil
	}
	page, err = strconv.Atoi(raw)
	if err != nil || page < 1 {
		return 0, false, errors.New("invalid page")
	}
	return page, true, nil
}

// readListingEntries scans a directory once and returns the sorted rows plus
// the folder/file counts for the header summary.
func readListingEntries(fsys fs.FS, fsPath, dirPath string, officeEnabled, cadEnabled bool) ([]listingEntry, int, int, error) {
	entries, err := fs.ReadDir(fsys, fsPath)
	if err != nil {
		return nil, 0, 0, err
	}

	out := make([]listingEntry, 0, len(entries))
	dirs, files := 0, 0
	for _, entry := range entries {
		name := entry.Name()

		// Cheap lstat first. Only a symlink can point outside the site folder,
		// so only those are re-read through the resolver — which reports an
		// escaping link as missing, so the list matches what GET will serve.
		info, statErr := entry.Info()
		if statErr != nil {
			continue
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			info, statErr = fs.Stat(fsys, path.Join(fsPath, name))
			if statErr != nil {
				continue
			}
		}

		kind := previewKind(name, info.IsDir())
		if kind == "office" && !officeEnabled {
			kind = "other"
		}
		if kind == "cad" && !cadEnabled {
			kind = "other"
		}

		item := listingEntry{
			Name:     name,
			Hidden:   strings.HasPrefix(name, "."),
			IsDir:    info.IsDir(),
			Kind:     kind,
			Modified: info.ModTime().Format("2006-01-02 15:04"),
		}

		href := path.Join(dirPath, name)
		if item.IsDir {
			href = withSlash(href)
			dirs++
		} else {
			item.Bytes = info.Size()
			item.Size = humanSize(item.Bytes)
			files++
		}
		item.Href = urlOf(href)

		out = append(out, item)
	}

	sortEntries(out)
	return out, dirs, files, nil
}

// sortEntries puts folders first, hidden entries after visible ones within each
// group, and sorts the rest by name, case-insensitively.
func sortEntries(entries []listingEntry) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		if a.Hidden != b.Hidden {
			return !a.Hidden
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

func crumbsFor(dirPath string) []listingCrumb {
	crumbs := []listingCrumb{{Name: i18n.T("web.listing.root"), Href: urlOf("/")}}
	if dirPath == "/" {
		return crumbs
	}

	current := ""
	for _, segment := range strings.Split(strings.TrimPrefix(dirPath, "/"), "/") {
		if segment == "" {
			continue
		}
		current += "/" + segment
		crumbs = append(crumbs, listingCrumb{Name: segment, Href: urlOf(withSlash(current))})
	}
	return crumbs
}

func withSlash(p string) string {
	if strings.HasSuffix(p, "/") {
		return p
	}
	return p + "/"
}

// urlOf turns a path into something usable directly in href (spaces and the
// like get escaped, slashes are kept).
func urlOf(p string) template.URL {
	return template.URL((&url.URL{Path: p}).EscapedPath())
}

func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}

	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGT"[exp])
}

// Decides how the preview panel on the right renders after a file is clicked.
var (
	textExts = map[string]bool{
		".txt": true, ".json": true, ".jsonc": true,
		".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true, ".jsx": true,
		".css": true, ".scss": true, ".sass": true, ".less": true,
		".xml": true, ".yaml": true, ".yml": true, ".toml": true, ".ini": true,
		".conf": true, ".cfg": true, ".env": true, ".properties": true,
		".sh": true, ".bash": true, ".zsh": true, ".fish": true, ".bat": true, ".ps1": true,
		".py": true, ".rb": true, ".go": true, ".rs": true, ".java": true, ".kt": true,
		".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true, ".cs": true,
		".php": true, ".swift": true, ".m": true, ".mm": true, ".dart": true, ".lua": true,
		".sql": true, ".log": true, ".csv": true, ".tsv": true,
		".gitignore": true, ".gitattributes": true, ".dockerignore": true, ".editorconfig": true,
		".mk": true, ".gradle": true, ".lock": true, ".mod": true, ".sum": true,
		".vue": true, ".svelte": true, ".astro": true, ".tex": true,
	}

	imageExts = map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
		".svg": true, ".bmp": true, ".ico": true, ".avif": true, ".apng": true,
	}

	// officeExts are the formats the bundled viewer renders. The listing only
	// offers them when that viewer is actually compiled in, which is why
	// previewKind stays a pure function and the caller applies the gate.
	officeExts = map[string]bool{
		".docx": true, ".xlsx": true, ".pptx": true,
		".doc": true, ".xls": true, ".ppt": true,
	}

	// cadExts is what the bundled CAD viewer renders. DXF is parsed by the
	// viewer itself; DWG goes through the LibreDWG converter, which the viewer
	// registers and runs in a worker (see internal/cad).
	cadExts = map[string]bool{
		".dxf": true,
		".dwg": true,
	}

	audioExts = map[string]bool{
		".mp3": true, ".wav": true, ".ogg": true, ".m4a": true, ".flac": true, ".aac": true,
	}

	videoExts = map[string]bool{
		".mp4": true, ".webm": true, ".mov": true, ".m4v": true, ".ogv": true,
	}
)

// previewKind decides from the extension whether the browser can preview the file directly.
func previewKind(name string, isDir bool) string {
	if isDir {
		return "dir"
	}

	ext := strings.ToLower(path.Ext(name))
	switch {
	case ext == ".pdf":
		return "pdf"
	case ext == ".html" || ext == ".htm":
		return "html"
	case imageExts[ext]:
		return "image"
	case audioExts[ext]:
		return "audio"
	case videoExts[ext]:
		return "video"
	case officeExts[ext]:
		return "office"
	case cadExts[ext]:
		return "cad"
	case ext == ".md" || ext == ".markdown":
		return "markdown"
	case textExts[ext]:
		return "text"
	case ext == "":
		// Files without an extension, like Makefile or LICENSE, are treated as text.
		return "text"
	case strings.HasPrefix(name, ".") && strings.Count(name, ".") == 1:
		// For dotfiles such as .env or .gitignore, path.Ext returns the whole name.
		return "text"
	}

	return "other"
}

// clientKeys are the keys the browser-side scripts need; the template only
// ships those to the client, not the whole translation table.
var clientKeys = []string{
	"web.listing.pick", "web.listing.supports", "web.listing.openTab",
	"web.listing.preview", "web.listing.loading", "web.listing.unreadable",
	"web.listing.imageError", "web.listing.noPreview", "web.listing.tooLarge",
	"web.listing.office.loading", "web.listing.office.tooLarge",
	"web.listing.office.unreadable", "web.listing.office.failed",
	"web.listing.office.encrypted", "web.listing.office.unsupported",
	"web.listing.office.download",
	"web.listing.cad.loading", "web.listing.cad.tooLarge",
	"web.listing.cad.unreadable", "web.listing.cad.failed",
}

// clientStrings picks the script keys out of the full translation table.
func clientStrings(all map[string]string) map[string]string {
	out := make(map[string]string, len(clientKeys))
	for _, key := range clientKeys {
		out[strings.TrimPrefix(key, "web.listing.")] = all[key]
	}
	return out
}

// countText renders a count with the right singular/plural form. The base is a
// key prefix: base + ".one" for exactly 1, base + ".other" otherwise.
func countText(base string, count int) string {
	key := base + ".other"
	if count == 1 {
		key = base + ".one"
	}
	return i18n.Tf(key, count)
}
