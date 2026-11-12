// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package web

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestHTTPServerTimeouts(t *testing.T) {
	srv := (&Server{}).newHTTPServer()

	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Fatalf("timeouts = header %v, read %v, idle %v; want all set",
			srv.ReadHeaderTimeout, srv.ReadTimeout, srv.IdleTimeout)
	}
	// A non-zero WriteTimeout would sever large media streams mid-flight.
	if srv.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout = %v, want 0 so long downloads are not cut off", srv.WriteTimeout)
	}
	if srv.MaxHeaderBytes < 64<<10 {
		t.Fatalf("MaxHeaderBytes = %d, want at least 64 KiB", srv.MaxHeaderBytes)
	}
}

func TestHandlerServesFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<h1>hello docWebHub</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app.css"), []byte("body { color: red; }"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := newHandler(root, Options{})

	t.Run("index", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), "hello docWebHub") {
			t.Fatalf("body does not contain index.html: %q", rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
			t.Fatalf("Cache-Control = %q, want it to contain no-store", got)
		}
	})

	t.Run("asset", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.css", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if body := rec.Body.String(); body != "body { color: red; }" {
			t.Fatalf("body = %q", body)
		}
	})
}

func TestHandlerListsDirectoryWithoutIndex(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := newHandler(root, Options{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "app.css") {
		t.Fatalf("response should list directory contents: %q", rec.Body.String())
	}
}

func TestHandlerServesNestedIndex(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "index.html"), []byte("docs home"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := newHandler(root, Options{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "docs home") {
		t.Fatalf("body = %q, want the nested index.html contents", rec.Body.String())
	}
}

func TestServerServesOverHTTP(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("served over http"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := New(root, 9101, Options{})
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, os.ErrPermission) {
			t.Skipf("cannot listen on a fixed port in this environment: %v", err)
		}
		t.Fatalf("New: %v", err)
	}
	if srv.Port() < 9101 {
		t.Fatalf("port = %d, want >= 9101", srv.Port())
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})

	go func() {
		_ = srv.Serve()
	}()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(srv.URL() + "/")
	if err != nil {
		t.Fatalf("GET %s: %v", srv.URL(), err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if string(body) != "served over http" {
		t.Fatalf("body = %q", body)
	}
}

func TestListenFromSkipsOccupiedPort(t *testing.T) {
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind a test port: %v", err)
	}
	defer occupied.Close()

	port := occupied.Addr().(*net.TCPAddr).Port
	if port >= 65535 {
		t.Skip("test port is already the maximum")
	}

	listener, got, err := listenFrom("127.0.0.1", port)
	if err != nil {
		t.Fatalf("listenFrom(127.0.0.1, %d): %v", port, err)
	}
	defer listener.Close()

	if got == port {
		t.Fatalf("must not pick the occupied port %d", port)
	}
	if got < port {
		t.Fatalf("should scan upwards from %d, got %d", port, got)
	}
}

func TestServerSetRootSwitchesContent(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	if err := os.WriteFile(filepath.Join(first, "index.html"), []byte("first site"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "index.html"), []byte("second site"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := New(first, 9101, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	if body := fetchBody(t, srv.URL()+"/"); body != "first site" {
		t.Fatalf("body before switch = %q, want first site", body)
	}

	srv.SetRoot(second)
	if srv.Root() != second {
		t.Fatalf("Root() = %q, want %q", srv.Root(), second)
	}
	if body := fetchBody(t, srv.URL()+"/"); body != "second site" {
		t.Fatalf("body after switch = %q, want second site", body)
	}
}

func fetchBody(t *testing.T, url string) string {
	t.Helper()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d", url, resp.StatusCode)
	}
	return strings.TrimSpace(string(body))
}

func TestHandlerServesIndexHTM(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "legacy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "legacy", "index.htm"), []byte("legacy htm"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := newHandler(root, Options{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/legacy/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "legacy htm") {
		t.Fatalf("body = %q, want the index.htm contents", rec.Body.String())
	}
}

func TestHandlerKeepsDirectoryRedirect(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "legacy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "legacy", "index.htm"), []byte("legacy htm"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := newHandler(root, Options{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/legacy", nil))

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want %d (keep the /dir → /dir/ redirect)", rec.Code, http.StatusMovedPermanently)
	}
	if got := rec.Header().Get("Location"); !strings.HasSuffix(got, "/") || !strings.Contains(got, "legacy") {
		t.Fatalf("Location = %q, want a redirect to the legacy/ directory", got)
	}
}

func TestHandlerIndexHTMLWinsOverHTM(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "both"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "both", "index.html"), []byte("html wins"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "both", "index.htm"), []byte("htm loses"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := newHandler(root, Options{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/both/", nil))

	if !strings.Contains(rec.Body.String(), "html wins") {
		t.Fatalf("body = %q, want index.html to take precedence", rec.Body.String())
	}
}

func TestServerSetPortSwitches(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("port test"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := New(root, freePort(t), Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	before := srv.URL()
	if body := fetchBody(t, before+"/"); body != "port test" {
		t.Fatalf("body before switch = %q", body)
	}

	port := freePort(t)
	url, err := srv.SetPort(port)
	if err != nil {
		t.Fatalf("SetPort(%d): %v", port, err)
	}
	if srv.Port() != port {
		t.Fatalf("Port() = %d, want %d", srv.Port(), port)
	}
	if want := fmt.Sprintf("http://127.0.0.1:%d", port); url != want {
		t.Fatalf("URL = %q, want %q", url, want)
	}
	if body := fetchBody(t, url+"/"); body != "port test" {
		t.Fatalf("body after switch = %q", body)
	}

	oldAddr := strings.TrimPrefix(before, "http://")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp4", oldAddr, 200*time.Millisecond)
		if dialErr != nil {
			return // old port has been released
		}
		conn.Close()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("old port %s is still accepting connections", oldAddr)
}

// Pressing Apply without touching the port must not try to rebind: this
// process already owns the port, so the listen would fail.
func TestServerSetPortToCurrentIsANoop(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("still here"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := New(root, freePort(t), Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	before := srv.URL()
	port := srv.Port()

	url, err := srv.SetPort(port)
	if err != nil {
		t.Fatalf("re-applying the current port %d: %v", port, err)
	}
	if url != before || srv.URL() != before {
		t.Fatalf("URL = %q / %q, want %q", url, srv.URL(), before)
	}
	if body := fetchBody(t, before+"/"); body != "still here" {
		t.Fatalf("site after re-applying the same port = %q", body)
	}
}

func TestServerSetPortRejectsBusyPort(t *testing.T) {
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind a test port: %v", err)
	}
	defer occupied.Close()
	busyPort := occupied.Addr().(*net.TCPAddr).Port

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("still here"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := New(root, freePort(t), Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	before := srv.URL()
	if _, err := srv.SetPort(busyPort); err == nil {
		t.Fatalf("an occupied port %d should return an error", busyPort)
	} else if !errors.Is(err, ErrAddressInUse) {
		t.Fatalf("error = %v, want ErrAddressInUse", err)
	}
	if srv.URL() != before {
		t.Fatalf("URL after failure = %q, want %q", srv.URL(), before)
	}
	if body := fetchBody(t, before+"/"); body != "still here" {
		t.Fatalf("original server unusable after failure, body = %q", body)
	}
}

func TestServerSetPortRejectsInvalid(t *testing.T) {
	srv, err := New(t.TempDir(), 9101, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	for _, port := range []int{0, -1, 65536} {
		if _, err := srv.SetPort(port); err == nil {
			t.Fatalf("port %d should be rejected", port)
		}
	}
}

func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot get a free port: %v", err)
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port
}

func TestListingPageRendersEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := newHandler(root, Options{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"<table", "readme.md", "docs/", "1 folder · 2 files", "Root"} {
		if !strings.Contains(body, want) {
			t.Fatalf("page is missing %q\n%s", want, body)
		}
	}
	if strings.Index(body, "docs/") > strings.Index(body, "readme.md") {
		t.Fatal("folders should be listed before files")
	}
	if strings.Contains(body, "Up one level") {
		t.Fatal("the root folder must not show the up-one-level link")
	}
}

func TestListingShowsParentLink(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}

	handler := newHandler(root, Options{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs/", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "Up one level") {
		t.Fatalf("a subdirectory should show the up-one-level link\n%s", body)
	}
	if !strings.Contains(body, `href="/docs/"`) && !strings.Contains(body, `href="/docs"`) {
		t.Fatalf("breadcrumb is missing the directory link\n%s", body)
	}
}

func TestListingEscapesNames(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "<script>.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := newHandler(root, Options{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	if strings.Contains(body, "<script>.txt") {
		t.Fatal("file name was not escaped, injection risk")
	}
	if !strings.Contains(body, "&lt;script&gt;.txt") {
		t.Fatalf("escaped file name not found\n%s", body)
	}
}

func TestListingRedirectsToTrailingSlash(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}

	handler := newHandler(root, Options{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/docs/" {
		t.Fatalf("Location = %q, want /docs/", got)
	}
}

func TestListingEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	handler := newHandler(root, Options{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/empty/", nil))

	if !strings.Contains(rec.Body.String(), "This folder is empty") {
		t.Fatalf("empty directory is missing the hint\n%s", rec.Body.String())
	}
}

func TestListingPaginatesLargeDirectory(t *testing.T) {
	root := t.TempDir()
	const total = 250
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("file-%03d.txt", i)
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	handler := newHandler(root, Options{})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "file-099.txt") {
		t.Fatal("the first page is missing its last entry")
	}
	if strings.Contains(body, "file-100.txt") {
		t.Fatal("the first page rendered entries past the page size")
	}
	for _, want := range []string{`id="listing-more"`, `data-next="1"`, `data-total="250"`, "Load more"} {
		if !strings.Contains(body, want) {
			t.Fatalf("first page is missing %q", want)
		}
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?page=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("page 1 status = %d", rec.Code)
	}
	if got := rec.Header().Get("X-Listing-Total"); got != "250" {
		t.Fatalf("X-Listing-Total = %q, want 250", got)
	}
	fragment := rec.Body.String()
	if strings.Contains(fragment, "<html") || strings.Contains(fragment, "<table") {
		t.Fatalf("page 1 is a full document, want rows only: %s", fragment)
	}
	if !strings.Contains(fragment, "file-100.txt") || !strings.Contains(fragment, "file-199.txt") {
		t.Fatalf("page 1 has the wrong rows: %s", fragment)
	}
	if strings.Contains(fragment, "file-099.txt") || strings.Contains(fragment, "file-200.txt") {
		t.Fatalf("page 1 escaped its range: %s", fragment)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?page=2", nil))
	last := rec.Body.String()
	if !strings.Contains(last, "file-249.txt") || strings.Contains(last, "file-199.txt") {
		t.Fatalf("page 2 has the wrong rows: %s", last)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?page=9", nil))
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("out-of-range page = %d, body %q; want 200 and no rows", rec.Code, rec.Body.String())
	}

	for _, raw := range []string{"x", "0", "-1"} {
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?page="+raw, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("page=%s status = %d, want 400", raw, rec.Code)
		}
	}
}

func TestListingCacheBoundsAndInvalidates(t *testing.T) {
	cache := newListingCache()
	now := time.Now()
	entries := []listingEntry{{Name: "a"}}

	cache.put("/", now, entries)
	if got := cache.get("/", now); len(got) != 1 {
		t.Fatalf("cache hit = %d entries, want 1", len(got))
	}
	if got := cache.get("/", now.Add(time.Second)); got != nil {
		t.Fatal("a changed directory mtime must not reuse the snapshot")
	}

	cache.put("/", now, entries)
	if got := cache.get("/", now); len(got) != 1 {
		t.Fatal("replacing a snapshot should not double-count it")
	}
	cache.ttl = time.Nanosecond
	time.Sleep(time.Millisecond)
	if got := cache.get("/", now); got != nil {
		t.Fatal("an expired snapshot must not be reused")
	}

	bounded := newListingCache()
	bounded.maxEntries = 2
	bounded.put("/small", now, []listingEntry{{Name: "a"}, {Name: "b"}})
	bounded.put("/big", now, []listingEntry{{Name: "a"}, {Name: "b"}, {Name: "c"}})
	if got := bounded.get("/big", now); got != nil {
		t.Fatal("a directory larger than the budget should not be cached")
	}
	bounded.put("/other", now, []listingEntry{{Name: "d"}})
	if got := bounded.get("/small", now); got != nil {
		t.Fatal("the oldest snapshot should be evicted first")
	}
	if got := bounded.get("/other", now); len(got) != 1 {
		t.Fatal("the newest snapshot should still be cached")
	}
}

func TestListingLoadsPreviewBundle(t *testing.T) {
	handler := newHandler(t.TempDir(), Options{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	want := `src="` + previewAssetPath + `"`
	if !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("listing is missing the preview bundle script (%s)", want)
	}
}

func TestPreviewAssetServed(t *testing.T) {
	srv := &Server{}

	rec := httptest.NewRecorder()
	srv.serveHTTP(rec, httptest.NewRequest(http.MethodGet, previewAssetPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("Content-Type = %q, want JavaScript", ct)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" || rec.Body.Len() == 0 {
		t.Fatalf("asset response is missing its body or ETag (etag %q, %d bytes)", etag, rec.Body.Len())
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, previewAssetPath, nil)
	req.Header.Set("If-None-Match", etag)
	srv.serveHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, previewAssetPath, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	srv.serveHTTP(rec, req)
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	gzipETag := rec.Header().Get("ETag")
	reader, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	if !strings.Contains(string(body), "LWPreview") {
		t.Fatal("decompressed bundle does not look like the listing preview script")
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, previewAssetPath, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("If-None-Match", gzipETag)
	srv.serveHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("conditional gzip status = %d, want 304", rec.Code)
	}
}

func TestPreviewKind(t *testing.T) {
	cases := []struct {
		name string
		dir  bool
		want string
	}{
		{"docs", true, "dir"},
		{"notes.md", false, "markdown"},
		{"README.markdown", false, "markdown"},
		{"main.go", false, "text"},
		{"Makefile", false, "text"},
		{".env", false, "text"},
		{"shot.PNG", false, "image"},
		{"doc.pdf", false, "pdf"},
		{"page.html", false, "html"},
		{"clip.mp4", false, "video"},
		{"voice.mp3", false, "audio"},
		{"plan.dxf", false, "cad"},
		{"PLAN.DXF", false, "cad"},
		{"plan.dwg", false, "cad"},
		{"PLAN.DWG", false, "cad"},
		{"archive.zip", false, "other"},
		{"binary.bin", false, "other"},
	}

	for _, tc := range cases {
		if got := previewKind(tc.name, tc.dir); got != tc.want {
			t.Errorf("previewKind(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestListingCarriesPreviewMetadata(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"notes.md":    "hi",
		"shot.png":    "x",
		"doc.pdf":     "x",
		"page.html":   "<h1>x</h1>",
		"archive.zip": "x",
		"Makefile":    "all:",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	handler := newHandler(root, Options{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	expect := map[string]string{
		"/notes.md":    "markdown",
		"/shot.png":    "image",
		"/doc.pdf":     "pdf",
		"/page.html":   "html",
		"/archive.zip": "other",
		"/Makefile":    "text",
		"/sub/":        "dir",
	}
	for href, kind := range expect {
		want := `href="` + href + `" data-kind="` + kind + `"`
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s\n%s", want, body)
		}
	}

	if !strings.Contains(body, `data-name="notes.md" data-size="2 B" data-bytes="2"`) {
		t.Fatalf("file size and byte count were not carried into the page\n%s", body)
	}
	if !strings.Contains(body, `id="preview-body"`) {
		t.Fatal("preview panel is missing")
	}
}

func TestAccessLogKeepsRecentEntries(t *testing.T) {
	records := newAccessLog()
	for i := 0; i < accessLimit+5; i++ {
		records.add(AccessRecord{Path: fmt.Sprintf("/%d", i)})
	}

	entries, total, started := records.snapshot()
	if len(entries) != accessLimit {
		t.Fatalf("kept entries = %d, want %d", len(entries), accessLimit)
	}
	if total != int64(accessLimit+5) {
		t.Fatalf("total requests = %d, want %d", total, accessLimit+5)
	}
	if entries[0].Path != "/5" {
		t.Fatalf("first entry = %q, want /5 (the oldest should be evicted)", entries[0].Path)
	}
	if last := fmt.Sprintf("/%d", accessLimit+4); entries[len(entries)-1].Path != last {
		t.Fatalf("last entry = %q, want %q", entries[len(entries)-1].Path, last)
	}
	if started.IsZero() {
		t.Fatal("start time should not be the zero value")
	}
}

type readFromWriter struct {
	header   http.Header
	body     strings.Builder
	status   int
	readFrom int
}

func (w *readFromWriter) Header() http.Header         { return w.header }
func (w *readFromWriter) Write(p []byte) (int, error) { return w.body.Write(p) }
func (w *readFromWriter) WriteHeader(code int)        { w.status = code }
func (w *readFromWriter) ReadFrom(r io.Reader) (int64, error) {
	w.readFrom++
	return io.Copy(&w.body, r)
}

func TestStatusRecorderForwardsReadFrom(t *testing.T) {
	inner := &readFromWriter{header: make(http.Header)}
	recorder := &statusRecorder{ResponseWriter: inner}

	n, err := recorder.ReadFrom(strings.NewReader("hello"))
	if err != nil || n != 5 {
		t.Fatalf("ReadFrom = %d, %v; want 5, nil", n, err)
	}
	if inner.readFrom != 1 {
		t.Fatalf("inner ReadFrom calls = %d, want 1 (the wrapper must not copy itself)", inner.readFrom)
	}
	if inner.body.String() != "hello" {
		t.Fatalf("body = %q, want hello", inner.body.String())
	}
	if recorder.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.status)
	}
	if recorder.Unwrap() != http.ResponseWriter(inner) {
		t.Fatal("Unwrap did not return the original writer")
	}
}

func TestStatusRecorderReadFromFallsBack(t *testing.T) {
	inner := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: inner}

	if _, err := recorder.ReadFrom(strings.NewReader("fallback")); err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if inner.Body.String() != "fallback" {
		t.Fatalf("body = %q, want fallback", inner.Body.String())
	}
	if recorder.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.status)
	}
}

func TestServeHTTPDoesNotSpinOnRetiredSite(t *testing.T) {
	srv := &Server{access: newAccessLog()}
	current := newSite(t.TempDir(), Options{})
	srv.site.Store(current)
	current.retire()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.serveHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serveHTTP spun on a retired snapshot instead of returning")
	}
}

func TestServerRecordsAccess(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("hit"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := New(root, freePort(t), Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	fetchBody(t, srv.URL()+"/")

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(srv.URL() + "/missing")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	entries, total, _ := srv.AccessSnapshot()
	if total != 2 {
		t.Fatalf("total requests = %d, want 2", total)
	}
	if len(entries) != 2 {
		t.Fatalf("record count = %d, want 2", len(entries))
	}

	first, last := entries[0], entries[1]
	if first.Path != "/" || first.Status != http.StatusOK || first.Method != http.MethodGet {
		t.Fatalf("first record = %+v", first)
	}
	if !strings.HasPrefix(first.RemoteAddr, "127.0.0.1:") {
		t.Fatalf("client address = %q, want a loopback address", first.RemoteAddr)
	}
	if last.Path != "/missing" || last.Status != http.StatusNotFound {
		t.Fatalf("last record = %+v", last)
	}
}

func TestServerSetBindAllInterfaces(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("bind test"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := New(root, freePort(t), Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	before := srv.URL()

	url, err := srv.SetBind("0.0.0.0")
	if err != nil {
		t.Fatalf("SetBind(0.0.0.0): %v", err)
	}
	if srv.Bind() != "0.0.0.0" {
		t.Fatalf("Bind() = %q, want 0.0.0.0", srv.Bind())
	}
	wantHost := PreferredAddress()
	if wantHost == "" {
		wantHost = DefaultBind
	}
	wantURL := "http://" + net.JoinHostPort(wantHost, strconv.Itoa(srv.Port()))
	if url != wantURL {
		t.Fatalf("URL after wildcard bind = %q, want the LAN address %q", url, wantURL)
	}
	if url == before && wantHost != DefaultBind {
		t.Fatalf("URL did not change after switching to every interface: %q", url)
	}
	if body := fetchBody(t, url+"/"); body != "bind test" {
		t.Fatalf("body after switch = %q", body)
	}

	if _, err := srv.SetBind(DefaultBind); err != nil {
		t.Fatalf("SetBind(127.0.0.1): %v", err)
	}
	if srv.Bind() != DefaultBind {
		t.Fatalf("Bind() after switching back = %q", srv.Bind())
	}
	if body := fetchBody(t, srv.URL()+"/"); body != "bind test" {
		t.Fatalf("body after switching back = %q", body)
	}
}

func TestServerSetBindRejectsInvalid(t *testing.T) {
	srv, err := New(t.TempDir(), freePort(t), Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	if _, err := srv.SetBind("not-an-address"); err == nil {
		t.Fatal("an invalid address should return an error")
	}
	if srv.Bind() != DefaultBind {
		t.Fatalf("Bind() after failure = %q, want %q", srv.Bind(), DefaultBind)
	}
}

func TestDialAddress(t *testing.T) {
	// A wildcard bind has to be advertised as an address a browser can open.
	wildcardHost := PreferredAddress()
	if wildcardHost == "" {
		wildcardHost = DefaultBind
	}

	cases := []struct {
		bind string
		port int
		want string
	}{
		{"127.0.0.1", 9101, "http://127.0.0.1:9101"},
		{"0.0.0.0", 8080, "http://" + net.JoinHostPort(wildcardHost, "8080")},
		{"192.168.1.5", 80, "http://192.168.1.5:80"},
		{"100.88.1.2", 9104, "http://100.88.1.2:9104"},
	}

	for _, tc := range cases {
		if got := dialAddress(tc.bind, tc.port); got != tc.want {
			t.Errorf("dialAddress(%q, %d) = %q, want %q", tc.bind, tc.port, got, tc.want)
		}
	}
}

// A wildcard bind must be advertised with an address other devices can reach.
func TestServerAdvertisesLANAddress(t *testing.T) {
	lan := PreferredAddress()
	if lan == "" {
		t.Skip("this machine has no LAN address")
	}

	srv, err := New(t.TempDir(), freePort(t), Options{Bind: AnyBind})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	if got := srv.URL(); got != "http://"+net.JoinHostPort(lan, strconv.Itoa(srv.Port())) {
		t.Fatalf("URL = %q, want the LAN address %q", got, lan)
	}
}

func TestPreferredAddressIsUsable(t *testing.T) {
	ip := PreferredAddress()
	if ip == "" {
		t.Skip("this machine has no LAN address")
	}

	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() == nil {
		t.Fatalf("PreferredAddress = %q, want an IPv4 address", ip)
	}
	if parsed.IsLoopback() || parsed.IsLinkLocalUnicast() || !parsed.IsGlobalUnicast() {
		t.Fatalf("PreferredAddress = %q, want a routable unicast address", ip)
	}
}

func TestScoreAddress(t *testing.T) {
	physical := scoreAddress("en0", net.ParseIP("192.168.1.10"))
	public := scoreAddress("en0", net.ParseIP("203.0.113.9"))
	virtual := scoreAddress("utun4", net.ParseIP("192.168.1.10"))
	tailscale := scoreAddress("utun3", net.ParseIP("100.88.1.2"))

	if !(physical > public && public > virtual && virtual > tailscale) {
		t.Fatalf("scores out of order: physical=%d public=%d virtual=%d tailscale=%d",
			physical, public, virtual, tailscale)
	}
}

func TestIsTailscale(t *testing.T) {
	cases := map[string]bool{
		"100.64.0.1":      true,
		"100.127.255.254": true,
		"100.128.0.1":     false,
		"192.168.1.10":    false,
		"":                false,
	}

	for ip, want := range cases {
		if got := IsTailscale(ip); got != want {
			t.Errorf("IsTailscale(%q) = %v, want %v", ip, got, want)
		}
	}
}

func TestLocalAddressesSkipsLoopback(t *testing.T) {
	for _, addr := range LocalAddresses() {
		if strings.HasPrefix(addr.IP, "127.") {
			t.Fatalf("must not include a loopback address: %+v", addr)
		}
		if addr.Interface == "" {
			t.Fatalf("interface name must not be empty: %+v", addr)
		}
	}
}

func TestServerRequiresBasicAuth(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("protected site"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash, err := HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	srv, err := New(root, freePort(t), Options{Auth: &Auth{Username: "admin", Hash: hash}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	resp, body := basicRequest(t, srv.URL()+"/", "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status without credentials = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if got := resp.Header.Get("WWW-Authenticate"); !strings.HasPrefix(got, "Basic ") {
		t.Fatalf("WWW-Authenticate = %q, want a Basic prefix", got)
	}
	if !strings.Contains(body, "Sign in required") {
		t.Fatalf("unexpected 401 page body: %q", body)
	}

	resp, _ = basicRequest(t, srv.URL()+"/", "admin", "wrong")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status with a wrong password = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	resp, body = basicRequest(t, srv.URL()+"/", "admin", "s3cret")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status with valid credentials = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if strings.TrimSpace(body) != "protected site" {
		t.Fatalf("body with valid credentials = %q", body)
	}

	// Rejected requests land in the access log too, so it is easy to see who is trying passwords.
	records, total, _ := srv.AccessSnapshot()
	if total != 3 {
		t.Fatalf("total requests = %d, want 3", total)
	}
	rejected := 0
	for _, record := range records {
		if record.Status == http.StatusUnauthorized {
			rejected++
		}
	}
	if rejected != 2 {
		t.Fatalf("401 records = %d, want 2", rejected)
	}
}

func TestServerSetAuthToggles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("toggle site"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := New(root, freePort(t), Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	if srv.AuthEnabled() {
		t.Fatal("auth should be disabled by default")
	}
	if resp, _ := basicRequest(t, srv.URL()+"/", "", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("status when auth is off = %d, want 200", resp.StatusCode)
	}

	hash, err := HashPassword("pw")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	auth := &Auth{Username: "u", Hash: hash}
	srv.SetAuth(auth)
	if !srv.AuthEnabled() || srv.AuthUser() != "u" {
		t.Fatalf("after enabling: AuthEnabled = %v, AuthUser = %q", srv.AuthEnabled(), srv.AuthUser())
	}

	// A copy is stored, so mutating the passed struct must not affect the running server.
	auth.Username = "hacked"
	if srv.AuthUser() != "u" {
		t.Fatalf("AuthUser = %q, want u", srv.AuthUser())
	}

	if resp, _ := basicRequest(t, srv.URL()+"/", "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status without credentials after enabling = %d, want 401", resp.StatusCode)
	}
	if resp, _ := basicRequest(t, srv.URL()+"/", "u", "pw"); resp.StatusCode != http.StatusOK {
		t.Fatalf("status with valid credentials = %d, want 200", resp.StatusCode)
	}

	srv.SetAuth(nil)
	if srv.AuthEnabled() {
		t.Fatal("passing nil should disable the password")
	}
	if resp, _ := basicRequest(t, srv.URL()+"/", "", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("status after disabling = %d, want 200", resp.StatusCode)
	}
}

// basicRequest issues a single GET; the Authorization header is omitted when
// no credentials are given.
func basicRequest(t *testing.T, url, username, password string) (*http.Response, string) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

// Media is the only response the browser may keep. A discarded buffer shows up
// as a stall in the middle of a seek, not as a few milliseconds of local disk.
func TestCacheControlByFileType(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"clip.mp4", "song.mp3", "page.html", "note.txt", "poster.png"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	handler := newHandler(root, Options{})

	cases := []struct{ path, want string }{
		{"/clip.mp4", mediaCacheControl},
		{"/song.mp3", mediaCacheControl},
		{"/page.html", defaultCacheControl},
		{"/note.txt", defaultCacheControl},
		{"/poster.png", defaultCacheControl},
		{"/", defaultCacheControl},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if got := rec.Header().Get("Cache-Control"); got != tc.want {
			t.Errorf("GET %s Cache-Control = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// Seeking has to work through a range request, and the cacheable header must
// still let a replaced file through: revalidation answers with a 304.
func TestMediaSeekingAndRevalidation(t *testing.T) {
	root := t.TempDir()
	body := strings.Repeat("0", 256*1024)
	if err := os.WriteFile(filepath.Join(root, "clip.mp4"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(root, Options{})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/clip.mp4", nil))
	if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("Accept-Ranges = %q, want bytes", got)
	}
	lastModified := rec.Header().Get("Last-Modified")
	if lastModified == "" {
		t.Fatal("no Last-Modified, so a browser could never revalidate")
	}

	seek := httptest.NewRequest(http.MethodGet, "/clip.mp4", nil)
	seek.Header.Set("Range", "bytes=1024-2047")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, seek)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("range request = %d, want 206", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 1024-2047/262144" {
		t.Fatalf("Content-Range = %q", got)
	}

	revalidate := httptest.NewRequest(http.MethodGet, "/clip.mp4", nil)
	revalidate.Header.Set("If-Modified-Since", lastModified)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, revalidate)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("revalidation = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("304 carried %d bytes", rec.Body.Len())
	}
}

// The bundled viewer lives under a reserved prefix. It must be reachable
// without the site password — its assets are the application's own code — while
// the site itself stays protected.
func TestOfficePrefixSkipsAuthButTheSiteDoesNot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("protected site"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash, err := HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	viewer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "viewer")
	})

	srv, err := New(root, freePort(t), Options{
		Auth:   &Auth{Username: "admin", Hash: hash},
		Office: viewer,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	resp, body := basicRequest(t, srv.URL()+"/__docwebhub/office/", "", "")
	if resp.StatusCode != http.StatusOK || body != "viewer" {
		t.Fatalf("office request without credentials = %d %q, want 200 \"viewer\"", resp.StatusCode, body)
	}

	resp, _ = basicRequest(t, srv.URL()+"/", "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("site request without credentials = %d, want 401", resp.StatusCode)
	}
}

// One preview asks for dozens of small files; letting those into the ring
// buffer would push every real request out of the inspector.
func TestOfficeRequestsStayOutOfTheAccessLog(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	viewer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "viewer")
	})

	srv, err := New(root, freePort(t), Options{Office: viewer})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	fetch := func(path string) {
		resp, err := http.Get(srv.URL() + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	for i := 0; i < 5; i++ {
		fetch("/__docwebhub/office/docx.mjs")
	}
	if _, total, _ := srv.AccessSnapshot(); total != 0 {
		t.Fatalf("access log recorded %d office requests, want 0", total)
	}

	fetch("/a.txt")
	if _, total, _ := srv.AccessSnapshot(); total != 1 {
		t.Fatalf("access log total = %d after one site request, want 1", total)
	}
}

// A folder named __docwebhub in the site must not shadow the viewer.
func TestOfficePrefixWinsOverASiteDirectory(t *testing.T) {
	root := t.TempDir()
	shadow := filepath.Join(root, "__docwebhub", "office")
	if err := os.MkdirAll(shadow, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shadow, "index.html"), []byte("from the site"), 0o644); err != nil {
		t.Fatal(err)
	}

	viewer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "from the viewer")
	})

	srv, err := New(root, freePort(t), Options{Office: viewer})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	resp, err := http.Get(srv.URL() + "/__docwebhub/office/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "from the viewer" {
		t.Fatalf("body = %q, want the viewer to win", body)
	}
}

// Office files only advertise a preview when the viewer is part of the build.
func TestListingOffersOfficeOnlyWhenAvailable(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.docx", "b.xlsx", "c.pptx", "d.doc", "e.xls", "f.ppt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	withViewer := newHandler(root, Options{
		Office: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	rec := httptest.NewRecorder()
	withViewer.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if strings.Count(body, `data-kind="office"`) != 6 {
		t.Fatalf("with a viewer, %d files advertise office, want 6", strings.Count(body, `data-kind="office"`))
	}
	// The client script looks these up by the trimmed key, so a rename in the
	// locale files would otherwise show up only as empty text in the browser.
	if !strings.Contains(body, `"office.loading"`) {
		t.Fatal("the listing page does not carry the office strings")
	}

	without := newHandler(root, Options{})
	rec = httptest.NewRecorder()
	without.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(rec.Body.String(), `data-kind="office"`) {
		t.Fatal("without a viewer, a file still advertises the office preview")
	}
}

// The CAD viewer shares the reserved namespace with the Office one; its own
// sub-prefix has to reach it and not the document viewer.
func TestCADPrefixReachesItsOwnViewer(t *testing.T) {
	root := t.TempDir()
	officeHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "office")
	})
	cadHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "cad")
	})

	srv, err := New(root, freePort(t), Options{Office: officeHandler, CAD: cadHandler})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	for _, tc := range []struct{ path, want string }{
		{"/__docwebhub/cad/", "cad"},
		{"/__docwebhub/cad/viewer.js", "cad"},
		{"/__docwebhub/office/", "office"},
	} {
		resp, err := http.Get(srv.URL() + tc.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != tc.want {
			t.Errorf("%s answered %q, want %q", tc.path, body, tc.want)
		}
	}
}

// A build with the CAD tag but no Office viewer still has to answer the CAD
// prefix instead of falling through to the site.
func TestCADWorksWithoutTheOfficeViewer(t *testing.T) {
	root := t.TempDir()
	cadHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "cad")
	})

	srv, err := New(root, freePort(t), Options{CAD: cadHandler})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	resp, err := http.Get(srv.URL() + "/__docwebhub/cad/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "cad" {
		t.Fatalf("body = %q, want the CAD viewer", body)
	}
}

// CAD files only advertise a preview when the viewer is part of the build.
func TestListingOffersCADOnlyWhenAvailable(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"plan.dxf", "detail.DXF", "plan.dwg"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	withViewer := newHandler(root, Options{
		CAD: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
	})
	rec := httptest.NewRecorder()
	withViewer.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if got := strings.Count(body, `data-kind="cad"`); got != 3 {
		t.Fatalf("with a viewer, %d files advertise cad, want 3", got)
	}
	// The client script looks these up by the trimmed key, so a rename in the
	// locale files would otherwise show up only as empty text in the browser.
	for _, key := range []string{`"cad.loading"`, `"cad.failed"`, `"cad.unreadable"`, `"cad.tooLarge"`} {
		if !strings.Contains(body, key) {
			t.Errorf("the listing page does not carry %s", key)
		}
	}

	without := newHandler(root, Options{})
	rec = httptest.NewRecorder()
	without.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(rec.Body.String(), `data-kind="cad"`) {
		t.Fatal("without a viewer, a file still advertises the CAD preview")
	}
}
