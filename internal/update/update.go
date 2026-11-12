// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

// Package update pulls the release manifest published on the project website
// and verifies it against a key compiled into the binary.
//
// Nothing arriving over the network is trusted until the Ed25519 signature
// checks out. The manifest then pins the SHA-256 of every script the browser
// will run, so a compromised CDN can neither fake a security advisory nor
// swap a module for different code.
//
// The scripts themselves are never embedded in the binary: they only exist on
// the website, and the app re-serves them from its own origin.
package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is where the published documents live.
const DefaultBaseURL = "https://docwebhub.app/update"

// EnvBaseURL overrides DefaultBaseURL for local development, so the release
// flow can be exercised against a file server without touching DNS.
const EnvBaseURL = "DOCWEBHUB_UPDATE_URL"

// EnvPublicKey overrides the embedded key, base64, for local development.
const EnvPublicKey = "DOCWEBHUB_UPDATE_KEY"

// Limits on what the network may hand us. A hostile response should cost the
// process nothing.
const (
	maxConfigBytes = 256 << 10
	maxScriptBytes = 1 << 20

	fetchTimeout  = 5 * time.Second
	defaultMaxAge = 5 * time.Minute
	maxRedirects  = 2
)

//go:embed keys/update.pub
var embeddedKey string

// File is a script the manifest pins by content hash.
type File struct {
	Src    string `json:"src"`
	SHA256 string `json:"sha256"`
}

// Module is a script the loader may run, plus the conditions for running it.
type Module struct {
	ID              string `json:"id"`
	Src             string `json:"src"`
	SHA256          string `json:"sha256"`
	RequiresConsent bool   `json:"requiresConsent,omitempty"`
}

// Notice is one security advisory.
type Notice struct {
	ID        string            `json:"id"`
	Level     string            `json:"level"`
	Published string            `json:"published"`
	Affected  string            `json:"affected"`
	Title     map[string]string `json:"title"`
	Summary   map[string]string `json:"summary"`
	Details   string            `json:"details"`
}

// Release is the signed manifest.
type Release struct {
	Schema       int               `json:"schema"`
	Latest       string            `json:"latest"`
	MinSupported string            `json:"minSupported"`
	ReleasedAt   string            `json:"releasedAt"`
	Notes        map[string]string `json:"notes"`
	URL          string            `json:"url"`
	CheckAfter   int               `json:"checkAfter"`
	Loader       File              `json:"loader"`
	Modules      []Module          `json:"modules"`
	Flags        map[string]any    `json:"flags"`
	Notices      []Notice          `json:"notices"`
}

// Snapshot is a verified manifest together with the scripts it pins.
type Snapshot struct {
	Release
	// Raw is the exact bytes the manifest was verified from, so the proxy can
	// hand them out unchanged instead of re-encoding a struct.
	Raw        []byte
	Loader     []byte
	LoaderFile File
	Modules    map[string][]byte
	Fetched    time.Time
}

// Client fetches, verifies and caches the manifest.
type Client struct {
	base string
	key  ed25519.PublicKey
	http *http.Client
	dir  string

	// Lang is the interface language, used to pick a translation for native
	// notifications. Set before Start.
	Lang string

	mu   sync.Mutex
	snap *Snapshot
}

// New returns a client. dir is the cache directory; an empty value disables
// the disk cache, which is what tests usually want.
func New(dir string) (*Client, error) {
	key, err := publicKey()
	if err != nil {
		return nil, err
	}

	base := strings.TrimRight(DefaultBaseURL, "/")
	if override := strings.TrimSpace(os.Getenv(EnvBaseURL)); override != "" {
		base = strings.TrimRight(override, "/")
	}

	return &Client{
		base: base,
		key:  key,
		http: &http.Client{
			Timeout: fetchTimeout,
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) > maxRedirects {
					return errors.New("too many redirects")
				}
				return nil
			},
		},
		dir: dir,
	}, nil
}

func publicKey() (ed25519.PublicKey, error) {
	encoded := strings.TrimSpace(embeddedKey)
	if override := strings.TrimSpace(os.Getenv(EnvPublicKey)); override != "" {
		encoded = override
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("update public key is not base64: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("update public key is %d bytes, want %d", len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// BaseURL reports where the client reads from, for the startup log.
func (c *Client) BaseURL() string { return c.base }

// Current returns the last verified snapshot, or nil before the first success.
func (c *Client) Current() *Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap
}

// Start loads the disk cache and then refreshes in the background, repeating
// at the interval the manifest asks for. It returns immediately: the web UI
// must never wait on the network.
func (c *Client) Start(ctx context.Context) {
	if snap := c.loadCache(); snap != nil {
		c.store(snap)
		go c.fireNativeNotices(snap)
	}
	go c.loop(ctx)
}

func (c *Client) loop(ctx context.Context) {
	// Refresh once shortly after start rather than immediately, so the first
	// page load is not competing with the fetch on a slow link.
	delay := 3 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		snap, err := c.Refresh(ctx)
		if err != nil {
			log.Printf("update check failed: %v", err)
			delay = time.Hour
			continue
		}

		delay = defaultMaxAge
		if snap.CheckAfter > 0 {
			delay = time.Duration(snap.CheckAfter) * time.Second
		}
	}
}

// Refresh fetches and verifies the manifest, then every script it pins.
// A module that fails verification is dropped; the loader is mandatory.
func (c *Client) Refresh(ctx context.Context) (*Snapshot, error) {
	payload, err := c.fetch(ctx, "/config.json", maxConfigBytes)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	sigText, err := c.fetch(ctx, "/config.json.sig", 4<<10)
	if err != nil {
		return nil, fmt.Errorf("signature: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil {
		return nil, fmt.Errorf("signature is not base64: %w", err)
	}
	if !ed25519.Verify(c.key, payload, sig) {
		// Worth a log line: a bad signature means either a broken deploy or an
		// attempt to push a manifest, and both deserve attention.
		return nil, errors.New("manifest signature does not match")
	}

	var release Release
	if err := json.Unmarshal(payload, &release); err != nil {
		return nil, fmt.Errorf("config is not valid JSON: %w", err)
	}
	if release.Schema != 1 {
		return nil, fmt.Errorf("unsupported manifest schema %d", release.Schema)
	}

	snap := &Snapshot{Release: release, Raw: payload, Modules: map[string][]byte{}, Fetched: time.Now()}

	loader, err := c.fetchScript(ctx, release.Loader)
	if err != nil {
		return nil, fmt.Errorf("loader: %w", err)
	}
	snap.Loader = loader
	snap.LoaderFile = release.Loader

	for _, m := range release.Modules {
		body, err := c.fetchScript(ctx, File{Src: m.Src, SHA256: m.SHA256})
		if err != nil {
			// One broken module must not take the others down.
			log.Printf("update: skipping module %q: %v", m.ID, err)
			continue
		}
		snap.Modules[m.ID] = body
	}

	c.store(snap)
	c.saveCache(payload, sigText, snap)
	go c.fireNativeNotices(snap)
	return snap, nil
}

func (c *Client) fetchScript(ctx context.Context, f File) ([]byte, error) {
	if f.Src == "" {
		return nil, errors.New("manifest does not name a script")
	}
	body, err := c.fetch(ctx, "/"+strings.TrimPrefix(f.Src, "/"), maxScriptBytes)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(f.SHA256)) {
		// The manifest is signed and names this hash, so a mismatch means the
		// bytes did not come from the same deploy as the manifest.
		return nil, errors.New("content hash does not match the manifest")
	}
	return body, nil
}

func (c *Client) fetch(ctx context.Context, path string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "docWebHub")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", path, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s: response exceeds %d bytes", path, limit)
	}
	return body, nil
}

func (c *Client) store(snap *Snapshot) {
	c.mu.Lock()
	c.snap = snap
	c.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Disk cache
//
// The cache is what keeps the feature working after an online run on a machine
// that later loses its route out. It lives under the user cache directory, not
// next to the binary: on macOS the app sits in a signed bundle that must not be
// written to, and caches are disposable in a way settings are not.

// CacheDir returns the directory for the proxied scripts.
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "docwebhub", "update-cache"), nil
}

func (c *Client) loadCache() *Snapshot {
	if c.dir == "" {
		return nil
	}

	payload, err := os.ReadFile(filepath.Join(c.dir, "config.json"))
	if err != nil {
		return nil
	}
	sigText, err := os.ReadFile(filepath.Join(c.dir, "config.json.sig"))
	if err != nil {
		return nil
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil || !ed25519.Verify(c.key, payload, sig) {
		return nil
	}

	var release Release
	if err := json.Unmarshal(payload, &release); err != nil || release.Schema != 1 {
		return nil
	}

	snap := &Snapshot{Release: release, Raw: payload, Modules: map[string][]byte{}, Fetched: time.Now()}
	loader, err := c.readCached(release.Loader)
	if err != nil {
		return nil
	}
	snap.Loader = loader
	snap.LoaderFile = release.Loader
	for _, m := range release.Modules {
		if body, err := c.readCached(File{Src: m.Src, SHA256: m.SHA256}); err == nil {
			snap.Modules[m.ID] = body
		}
	}
	return snap
}

func (c *Client) readCached(f File) ([]byte, error) {
	body, err := os.ReadFile(filepath.Join(c.dir, filepath.FromSlash(f.Src)))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(f.SHA256)) {
		return nil, errors.New("cached script no longer matches the manifest")
	}
	return body, nil
}

func (c *Client) saveCache(payload, sig []byte, snap *Snapshot) {
	if c.dir == "" {
		return
	}
	if err := os.MkdirAll(filepath.Join(c.dir, "modules"), 0o755); err != nil {
		log.Printf("update cache: %v", err)
		return
	}
	c.writeAtomic("config.json", payload)
	c.writeAtomic("config.json.sig", sig)

	if snap.Loader != nil {
		c.writeAtomic(filepath.FromSlash(snap.LoaderFile.Src), snap.Loader)
	}
	for _, m := range snap.Release.Modules {
		if body, ok := snap.Modules[m.ID]; ok {
			c.writeAtomic(filepath.FromSlash(m.Src), body)
		}
	}
}

// writeAtomic writes through a temporary file so a crash never leaves a
// half-written script that would fail its hash on the next start.
func (c *Client) writeAtomic(rel string, body []byte) {
	path := filepath.Join(c.dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("update cache: %v", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		log.Printf("update cache: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("update cache: %v", err)
		os.Remove(tmp)
	}
}

// ---------------------------------------------------------------------------
// Browser payload

// PageData is what gets inlined into the listing page as JSON. It carries only
// what the loader and modules need; the hashes stay on this side.
type PageData struct {
	Schema       int               `json:"schema"`
	Version      string            `json:"version"`
	Latest       string            `json:"latest"`
	MinSupported string            `json:"minSupported"`
	Notes        map[string]string `json:"notes,omitempty"`
	URL          string            `json:"url,omitempty"`
	Lang         string            `json:"lang"`
	Consent      string            `json:"consent"`
	Modules      []PageModule      `json:"modules"`
	Flags        map[string]any    `json:"flags,omitempty"`
	Notices      []Notice          `json:"notices,omitempty"`
}

// PageModule is one module as the browser sees it: an id and a same-origin URL.
type PageModule struct {
	ID              string `json:"id"`
	URL             string `json:"url"`
	RequiresConsent bool   `json:"requiresConsent,omitempty"`
}

// PageDataFor builds the payload for one page render.
//
// Advisories are filtered here rather than in the browser: the Go side already
// knows the running version, and keeping the filter in one place means a
// tampered module cannot invent an advisory that the manifest never contained.
func (c *Client) PageDataFor(version, lang, consent string) *PageData {
	snap := c.Current()
	if snap == nil {
		return nil
	}

	data := &PageData{
		Schema:       snap.Schema,
		Version:      version,
		Latest:       snap.Latest,
		MinSupported: snap.MinSupported,
		Notes:        snap.Notes,
		URL:          snap.URL,
		Lang:         lang,
		Consent:      consent,
		Flags:        snap.Flags,
	}

	for _, m := range snap.Release.Modules {
		if _, ok := snap.Modules[m.ID]; !ok {
			continue // not verified this round, so do not advertise it
		}
		url := ModuleURL(m.Src)
		if url == "" {
			log.Printf("update: dropping module %q with unexpected path %q", m.ID, m.Src)
			continue
		}
		data.Modules = append(data.Modules, PageModule{
			ID:              m.ID,
			URL:             url,
			RequiresConsent: m.RequiresConsent,
		})
	}

	for _, n := range snap.Notices {
		if n.ID == "" || n.Details == "" {
			continue
		}
		if !strings.HasPrefix(n.Details, "https://docwebhub.app/security/") {
			// The manifest is signed, but a link is the one field a user acts
			// on, so it gets its own check rather than relying on the operator.
			log.Printf("update: dropping notice %q with unexpected link %q", n.ID, n.Details)
			continue
		}
		if !affectedBy(version, n.Affected) {
			continue
		}
		data.Notices = append(data.Notices, n)
	}

	return data
}

// ---------------------------------------------------------------------------
// Native notification

// fireNativeNotices asks the OS to show one notification per urgent advisory.
// It runs even when no browser is open, which is the only reason a user who
// never opens the listing page still hears about a security problem.
//
// The filtering lives here rather than in each platform file so the rule is
// stated once: only critical and high advisories interrupt the user, and only
// when there is a title to show.
func (c *Client) fireNativeNotices(snap *Snapshot) {
	for _, n := range snap.Notices {
		if n.Level != "critical" && n.Level != "high" {
			continue
		}
		title := n.TitleFor(c.Lang)
		if title == "" {
			continue
		}
		// A platform that cannot show one now is not retried: the next refresh
		// brings the same advisory back.
		if !showNotice(title, n.BodyFor(c.Lang)) {
			return
		}
	}
}

// showNotice is replaced per platform; the default has no way to notify.
var showNotice = func(title, body string) bool { return false }

// customShow is installed by the UI layer where showing a notification needs a
// handle this package does not own. Windows is the case that matters: a tray
// balloon needs the window the tray icon belongs to.
var customShow func(title, body string) bool

// SetNotifier installs a platform notifier for builds where the UI layer owns
// the system's notification surface. Linux and macOS ship a self-contained
// implementation and ignore the override.
func SetNotifier(fn func(title, body string) bool) {
	customShow = fn
}

// Title and Body pick the best available translation for lang.
func (n Notice) TitleFor(lang string) string { return pickLang(n.Title, lang) }
func (n Notice) BodyFor(lang string) string  { return pickLang(n.Summary, lang) }

func pickLang(m map[string]string, lang string) string {
	if m == nil {
		return ""
	}
	if v := m[lang]; v != "" {
		return v
	}
	// zh-CN also covers zh-TW readers better than English does.
	if strings.HasPrefix(lang, "zh") {
		if v := m["zh-CN"]; v != "" {
			return v
		}
	}
	if v := m["en"]; v != "" {
		return v
	}
	for _, v := range m {
		if v != "" {
			return v
		}
	}
	return ""
}

// moduleNamePattern mirrors the proxy's whitelist.
var moduleNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.js$`)

// ModuleURL turns a manifest path into the same-origin URL the browser loads.
func ModuleURL(src string) string {
	if !strings.HasPrefix(src, "modules/") {
		return ""
	}
	name := strings.TrimPrefix(src, "modules/")
	if !moduleNamePattern.MatchString(name) {
		return ""
	}
	return "/__docwebhub/update/modules/" + name
}
