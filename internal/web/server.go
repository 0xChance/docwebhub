// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

// Package web serves a static site over a loopback HTTP listener.
package web

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/0xChance/docwebhub/internal/cad"
	"github.com/0xChance/docwebhub/internal/office"
	"github.com/0xChance/docwebhub/internal/update"
)

// DefaultBind is the safe listen address: only reachable from this machine.
// The application chooses AnyBind at startup, but a caller that does not ask
// for anything specific still gets loopback.
const DefaultBind = "127.0.0.1"

// AnyBind listens on every interface. The advertised URL is then the machine's
// LAN address, not the wildcard, which no browser can open.
const AnyBind = "0.0.0.0"

// ErrAddressInUse reports that the requested address is already taken. Callers
// use it to tell "someone else has the port" apart from the rarer failures,
// and to word the message in the user's language.
var ErrAddressInUse = errors.New("address already in use")

// isAddrInUse matches both the POSIX errno and the Winsock code, so the check
// needs no build tags.
func isAddrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, syscall.Errno(10048))
}

// Options control how the static site is served.
type Options struct {
	// Bind is the listen address, such as 127.0.0.1, 0.0.0.0 or a NIC's IP.
	// DefaultBind is used when empty.
	Bind string
	// Auth is nil when no access password is required; otherwise every request
	// must pass HTTP Basic Auth.
	Auth *Auth
	// Office serves the bundled document viewer under its reserved prefix. It
	// is answered before the site and without the site password; nil disables
	// the preview.
	Office http.Handler
	// CAD serves the bundled drawing viewer the same way. It shares the
	// reserved namespace with Office but has its own sub-prefix; nil disables
	// the preview.
	CAD http.Handler
	// Update serves the proxied release manifest under /__docwebhub/update/.
	// nil turns the update channel off entirely, which also removes the
	// banners from the listing page.
	Update *update.Client
	// Version is the running build's version, shown to the scripts so they can
	// compare it with the published one.
	Version string
}

// site is the handler snapshot for one "root folder + listing flag" pair.
// Switching the root replaces it wholesale so a request never observes a
// half-updated state.
type site struct {
	root    string
	handler http.Handler
	// files is the confined root handle behind handler. It is closed once the
	// snapshot is retired and the last request using it has returned.
	files *siteRoot

	mu      sync.Mutex
	users   int
	retired bool
}

// acquire marks the snapshot as in use. It reports false once the snapshot has
// been retired, in which case the caller should load the replacement.
func (s *site) acquire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retired {
		return false
	}
	s.users++
	return true
}

// release ends one use and closes the handle when this was the last one.
func (s *site) release() {
	s.mu.Lock()
	s.users--
	closeNow := s.retired && s.users == 0
	s.mu.Unlock()
	if closeNow {
		_ = s.files.Close()
	}
}

// retire stops new requests from using the snapshot and closes the handle as
// soon as the requests already inside it have returned.
func (s *site) retire() {
	s.mu.Lock()
	s.retired = true
	closeNow := s.users == 0
	s.mu.Unlock()
	if closeNow {
		_ = s.files.Close()
	}
}

// Server is a static file server running on 127.0.0.1.
type Server struct {
	mu       sync.Mutex
	listener net.Listener
	http     *http.Server
	port     int
	bind     string
	site     atomic.Pointer[site]
	auth     atomic.Pointer[Auth]
	access   *accessLog
	counts   fileCount
	office   http.Handler
	cad      http.Handler
	update   *update.Client
	version  string
}

// New finds the first free port starting at startPort and creates the static
// file server. The port is bound here, which avoids a race between the
// availability check and the actual listen.
func New(root string, startPort int, opts Options) (*Server, error) {
	bind := opts.Bind
	if bind == "" {
		bind = DefaultBind
	}

	listener, port, err := listenFrom(bind, startPort)
	if err != nil {
		return nil, err
	}

	srv := &Server{
		listener: listener,
		port:     port,
		bind:     bind,
		access:   newAccessLog(),
		office:   opts.Office,
		cad:      opts.CAD,
		update:   opts.Update,
		version:  opts.Version,
	}
	srv.site.Store(newSite(root, opts))
	srv.SetAuth(opts.Auth)
	srv.http = srv.newHTTPServer()
	return srv, nil
}

func (s *Server) newHTTPServer() *http.Server {
	return &http.Server{
		Handler: http.HandlerFunc(s.serveHTTP),
		// A slow or half-open client must not hold a goroutine forever.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		// WriteTimeout stays unset on purpose: it is an absolute deadline from
		// the start of the request, so any fixed value would cut off a long
		// media stream or a large download. IdleTimeout reaps keep-alive
		// connections that go quiet instead.
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
}

// URL returns the address the site is reachable at. It is computed on demand so
// a wildcard bind keeps showing the current LAN address after the machine moved
// to another network.
func (s *Server) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return dialAddress(s.bind, s.port)
}

// Port returns the port actually in use.
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// Root returns the site root currently served.
func (s *Server) Root() string {
	return s.site.Load().root
}

// SetRoot switches the site root without disturbing the listening socket.
// The previous snapshot is retired only after the new one is visible, so a
// request that races with the switch either finishes on the old root or starts
// on the new one, and the old handle is closed once its last request returns.
func (s *Server) SetRoot(root string) {
	previous := s.site.Swap(newSite(root, Options{Office: s.office}))
	if previous != nil {
		previous.retire()
	}
}

// SetAuth sets the access password; nil or an empty password disables the check.
//
// A copy is kept, so later changes to the caller's struct do not affect the
// running server.
func (s *Server) SetAuth(auth *Auth) {
	if !auth.Enabled() {
		s.auth.Store(nil)
		return
	}

	copied := *auth
	s.auth.Store(&copied)
}

// AuthEnabled reports whether the browser is currently asked for credentials.
func (s *Server) AuthEnabled() bool {
	return s.auth.Load().Enabled()
}

// AuthUser returns the current username, empty when no password is required.
func (s *Server) AuthUser() string {
	if auth := s.auth.Load(); auth.Enabled() {
		return auth.Username
	}
	return ""
}

// SetPort moves the server to the given port and returns the new URL.
func (s *Server) SetPort(port int) (string, error) {
	return s.rebind(s.Bind(), port)
}

// SetBind moves the server to the given listen address on the same port and
// returns the new URL.
func (s *Server) SetBind(bind string) (string, error) {
	return s.rebind(bind, s.Port())
}

// Bind returns the current listen address.
func (s *Server) Bind() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bind
}

// rebind listens on the target address and only switches over once that succeeds.
//
// When the port changes, the old and new listeners can coexist and the old one
// is retired afterwards. Changing the address on the same port means the old
// listener still holds the port, so it has to be stopped first; if that fails
// the server rolls back to the original address.
func (s *Server) rebind(bind string, port int) (string, error) {
	if bind == "" {
		bind = DefaultBind
	}
	if bind != "localhost" && net.ParseIP(bind) == nil {
		return "", fmt.Errorf("invalid bind address: %s", bind)
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("port must be between 1 and 65535: %d", port)
	}

	s.mu.Lock()
	samePort := s.port == port
	sameBind := s.bind == bind
	currentURL := dialAddress(s.bind, s.port)
	previousBind := s.bind
	previous := s.http
	previousListener := s.listener
	s.mu.Unlock()

	// Applying the address that is already being served is a no-op. Without
	// this, the listen below fails because this very process owns the port.
	if samePort && sameBind {
		return currentURL, nil
	}

	addr := net.JoinHostPort(bind, strconv.Itoa(port))
	listener, err := listenOn(bind, addr)
	if err == nil {
		url := s.startServing(listener, bind, port)
		s.drain(previous, previousListener)
		return url, nil
	}

	if !samePort || bind == previousBind {
		return "", bindError(addr, err)
	}

	// Same port, new address: the old listener holds the port, so stop it and
	// retry. Shutdown closes the listener before it returns, but the kernel can
	// keep the port reserved for a moment longer — most often because a client
	// connection from the previous address is still winding down — so the bind
	// is retried briefly instead of reporting a failure the user would fix by
	// clicking the button again.
	s.stopServing()
	var retryErr error
	for attempt := 0; attempt < 20; attempt++ {
		listener, retryErr = listenOn(bind, addr)
		if retryErr == nil {
			break
		}
		if !isAddrInUse(retryErr) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if retryErr != nil {
		// Roll back so the site does not go completely dark.
		if restore, restoreErr := listenOn(previousBind, net.JoinHostPort(previousBind, strconv.Itoa(port))); restoreErr == nil {
			s.startServing(restore, previousBind, port)
		}
		return "", bindError(addr, retryErr)
	}

	url := s.startServing(listener, bind, port)
	return url, nil
}

// bindError keeps the platform's English text out of the window: the common
// case gets a sentinel the caller can translate, and the rest keeps its detail.
func bindError(addr string, err error) error {
	if isAddrInUse(err) {
		return fmt.Errorf("%w: %s", ErrAddressInUse, addr)
	}
	return fmt.Errorf("cannot bind %s: %w", addr, err)
}

// startServing takes over a new listener and updates the advertised details.
func (s *Server) startServing(listener net.Listener, bind string, port int) string {
	s.mu.Lock()
	s.listener = listener
	s.bind = bind
	s.port = port
	s.http = s.newHTTPServer()
	current, url := s.http, dialAddress(bind, port)
	s.mu.Unlock()

	go func() {
		if err := current.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			log.Printf("serve failed: %v", err)
		}
	}()

	return url
}

// drain gives in-flight requests on the old listener 2 seconds to finish.
//
// The old listener is closed explicitly before the graceful wait begins:
// Shutdown only closes listeners its Serve goroutine has already registered,
// and there is a window — between New and Serve, or between startServing and
// the goroutine actually running — where that has not happened yet, in which
// case Shutdown leaves the socket open and the port is never released. The
// close below is a no-op when Serve did register (and even when it later
// closes its own once-close wrapper), because closing a closed listener is
// harmless.
func (s *Server) drain(previous *http.Server, previousListener net.Listener) {
	if previous == nil {
		return
	}

	go func() {
		if previousListener != nil {
			_ = previousListener.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = previous.Shutdown(ctx)
	}()
}

// stopServing stops the current listener immediately, for a same-port address change.
// The listener is closed directly for the same reason drain does it: Shutdown
// alone cannot reach a listener whose Serve goroutine has not started yet, so
// relying on it alone leaks the port and the same-port retries below never
// succeed.
func (s *Server) stopServing() {
	s.mu.Lock()
	previous := s.http
	listener := s.listener
	s.mu.Unlock()

	if listener != nil {
		_ = listener.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = previous.Shutdown(ctx)
}

// Serve handles requests until the server is shut down.
func (s *Server) Serve() error {
	s.mu.Lock()
	httpServer, listener := s.http, s.listener
	s.mu.Unlock()
	return httpServer.Serve(listener)
}

// Shutdown closes the web server gracefully and releases the site handle.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	httpServer := s.http
	s.mu.Unlock()
	err := httpServer.Shutdown(ctx)
	if current := s.site.Load(); current != nil {
		current.retire()
	}
	return err
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	// The listing's highlighter/Markdown bundle is application code, like the
	// viewers, so it answers before the site (and without the site password).
	// It lives under the reserved prefix to keep a site folder from shadowing it.
	if r.URL.Path == previewAssetPath {
		servePreviewAsset(w, r)
		return
	}

	// The release manifest and its scripts are proxied through the app so the
	// browser never reaches the website itself. Answered before the site and
	// without the password, like the other application assets: the content
	// carries no user data and lives under the reserved prefix.
	if strings.HasPrefix(r.URL.Path, updatePrefix) {
		s.serveUpdate(w, r)
		return
	}

	// The bundled viewers live under a reserved prefix and are answered before
	// the site. They are the application's own code and carry no user data, so
	// they need no password — and the preview pages run in an iframe that
	// cannot reach the site anyway. Keeping them out of the access log matters
	// too: one preview would otherwise evict dozens of entries from the
	// 200-record ring.
	if strings.HasPrefix(r.URL.Path, office.Prefix) {
		switch {
		case s.cad != nil && strings.HasPrefix(r.URL.Path, cad.Prefix):
			s.cad.ServeHTTP(w, r)
		case s.office != nil:
			s.office.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
		return
	}

	start := time.Now()
	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

	if s.authorize(recorder, r) {
		// A root switch can retire the snapshot between Load and acquire, so
		// retry until the current one is marked in use. SetRoot publishes the
		// replacement before retiring the old snapshot, which guarantees that
		// the next Load returns a live snapshot.
		for {
			current := s.site.Load()
			if current == nil || current.acquire() {
				if current != nil {
					current.handler.ServeHTTP(recorder, r)
					current.release()
				}
				break
			}
			// SetRoot publishes the replacement before retiring the snapshot,
			// so a different pointer is live and the loop can retry. The same
			// retired pointer means Shutdown ran and no replacement will
			// appear; answer without touching a closed root.
			if s.site.Load() == current {
				break
			}
		}
	}

	s.access.add(AccessRecord{
		Time:       start,
		Method:     r.Method,
		Path:       r.URL.RequestURI(),
		RemoteAddr: r.RemoteAddr,
		Status:     recorder.status,
		Duration:   time.Since(start),
		UserAgent:  r.UserAgent(),
	})
}

func newSite(root string, opts Options) *site {
	handler, files := newHandlerWithRoot(root, opts)
	return &site{
		root:    root,
		handler: handler,
		files:   files,
	}
}

func newHandler(root string, opts Options) http.Handler {
	handler, _ := newHandlerWithRoot(root, opts)
	return handler
}

// newHandlerWithRoot also returns the confined root handle so the server can
// close it when the snapshot is retired. A handler built outside a Server
// (tests) simply leaves the handle to its finalizer.
func newHandlerWithRoot(root string, opts Options) (http.Handler, *siteRoot) {
	// One resolver behind every branch: the preview and the listing page both
	// refuse a symlink that leaves the site folder.
	site := newSiteRoot(root)
	files := http.FileServerFS(site)
	// "Load more" reuses the scan for the page it belongs to instead of reading
	// the directory and statting every entry again.
	listings := newListingCache()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Nothing is cached by default: rebuild the site, refresh, see the
		// latest. Media is the exception, see cacheControlFor.
		w.Header().Set("Cache-Control", cacheControlFor(r.URL.Path))

		dirPath, isDir := site.dir(r.URL.Path)
		if !isDir {
			files.ServeHTTP(w, r)
			return
		}

		// Always append the trailing slash so relative links resolve against this directory.
		if !strings.HasSuffix(r.URL.Path, "/") {
			redirect := (&url.URL{Path: r.URL.Path + "/", RawQuery: r.URL.RawQuery}).String()
			http.Redirect(w, r, redirect, http.StatusMovedPermanently)
			return
		}

		if site.file(path.Join(dirPath, "index.html")) {
			files.ServeHTTP(w, r)
			return
		}

		// FileServer only knows index.html; index.htm has to be forwarded explicitly.
		if site.file(path.Join(dirPath, "index.htm")) {
			indexed := r.Clone(r.Context())
			indexed.URL.Path = path.Join(dirPath, "index.htm")
			files.ServeHTTP(w, indexed)
			return
		}

		// No index page: show the file list. This is the whole point of the
		// preview, so it is not optional.
		serveListing(w, r, site, dirPath, opts, listings)
	})

	return handler, site
}

// defaultCacheControl makes every refresh show the current file on disk.
const defaultCacheControl = "no-store, must-revalidate"

// mediaCacheControl lets the browser keep what it already downloaded, so
// scrubbing and resuming a video do not fetch the same bytes again. "no-cache"
// rather than a max-age is deliberate: the browser may store the response but
// has to revalidate it, and the file server answers that with a 304 — so a
// re-exported video still replaces the old one on the next refresh.
const mediaCacheControl = "private, no-cache"

// cacheControlFor picks the header for one request. Media files use the
// preview pane's own extension lists: they are the ones the browser streams
// through a <video>/<audio> element, where a discarded buffer means a visible
// stall rather than a few milliseconds of local disk.
func cacheControlFor(urlPath string) string {
	if isMediaPath(urlPath) {
		return mediaCacheControl
	}
	return defaultCacheControl
}

// isMediaPath reports whether urlPath names a file the browser plays inline.
func isMediaPath(urlPath string) bool {
	ext := strings.ToLower(path.Ext(urlPath))
	return videoExts[ext] || audioExts[ext]
}

func listenFrom(bind string, startPort int) (net.Listener, int, error) {
	if startPort < 1 || startPort > 65535 {
		return nil, 0, fmt.Errorf("start port out of range: %d", startPort)
	}

	var lastErr error
	for port := startPort; port <= 65535; port++ {
		addr := net.JoinHostPort(bind, strconv.Itoa(port))
		listener, err := listenOn(bind, addr)
		if err == nil {
			return listener, port, nil
		}
		lastErr = err
	}

	return nil, 0, fmt.Errorf("no free port found starting at %s:%d: %w", bind, startPort, lastErr)
}

// listenOn binds the way the address implies. A bare "tcp" on a wildcard gives
// Go a dual-stack socket, which can sit alongside another IPv4 listener on the
// same port and silently hand IPv4 traffic to it; tcp4/tcp6 make the conflict
// visible instead.
func listenOn(bind, addr string) (net.Listener, error) {
	network := "tcp"
	if ip := net.ParseIP(bind); ip != nil {
		if ip.To4() != nil {
			network = "tcp4"
		} else {
			network = "tcp6"
		}
	}
	return net.Listen(network, addr)
}

// dialAddress returns a browser-friendly URL. A wildcard bind is advertised as
// the machine's LAN address, because 0.0.0.0 is not something a browser can
// open; loopback is the last resort when no network is up.
func dialAddress(bind string, port int) string {
	host := bind
	if host == "" || host == AnyBind || host == "::" {
		host = PreferredAddress()
		if host == "" {
			host = DefaultBind
		}
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
}
