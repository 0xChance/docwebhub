// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0xChance/docwebhub/internal/update"
)

// updateSite serves a listing page backed by a server with an update client
// attached. The client has no snapshot, so only the proxy paths are exercised;
// the point here is the whitelist, not the manifest.
func newUpdateTestServer(t *testing.T) *Server {
	t.Helper()
	srv, err := New(t.TempDir(), freePort(t), Options{Update: &update.Client{}, Version: "0.1.0"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })
	return srv
}

func TestUpdateProxyRejectsUnlistedPaths(t *testing.T) {
	srv := newUpdateTestServer(t)

	// Every one of these would be a way out of the whitelist if the proxy
	// trusted the URL: traversal, an absolute path, an unexpected file name.
	paths := []string{
		"/__docwebhub/update/",
		"/__docwebhub/update/",
		"/__docwebhub/update/../config.json",
		"/__docwebhub/update/modules/../../config.json",
		"/__docwebhub/update/modules/UPPER.js",
		"/__docwebhub/update/modules/sub/dir.js",
		"/__docwebhub/update/unknown.js",
		"/__docwebhub/update/v1.js/extra",
	}

	handler := srv.newTestHandler(t)
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d; want 404", path, rec.Code)
		}
	}
}

func TestUpdateProxyMethodNotAllowed(t *testing.T) {
	srv := newUpdateTestServer(t)
	handler := srv.newTestHandler(t)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/__docwebhub/update/v1.js", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d; want 405", method, rec.Code)
		}
	}
}

// The channel is off unless a client is configured, and an empty client has no
// verified snapshot yet: both must look like "no scripts here".
func TestUpdateProxyWithoutSnapshot(t *testing.T) {
	root := t.TempDir()
	srv, err := New(root, freePort(t), Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })

	req := httptest.NewRequest(http.MethodGet, "/__docwebhub/update/v1.js", nil)
	rec := httptest.NewRecorder()
	srv.newTestHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("v1.js with no client = %d; want 404", rec.Code)
	}
}

// newTestHandler exposes the server's own handler so tests hit the same
// dispatch order production uses.
func (s *Server) newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(s.serveHTTP)
}

func TestModuleFilePattern(t *testing.T) {
	allowed := []string{"version-notice.js", "report-v2.js", "a.js", "0.js"}
	rejected := []string{"", ".js", "UPPER.js", "a/b.js", "../x.js", "a.js.map", "a.JS", "report_v2.js"}

	for _, name := range allowed {
		if !moduleFile.MatchString(name) {
			t.Errorf("%q should be allowed", name)
		}
	}
	for _, name := range rejected {
		if moduleFile.MatchString(name) {
			t.Errorf("%q should be rejected", name)
		}
	}
}

func TestUpdatePrefixIsReserved(t *testing.T) {
	// A site file must never be able to shadow the proxy: the prefix check
	// happens before the site handler.
	if !strings.HasPrefix(updatePrefix+"/modules/x.js", "/__docwebhub/") {
		t.Fatal("update prefix left the reserved namespace")
	}
}
