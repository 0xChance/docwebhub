// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The numbers keep the cost of the shared resolver honest: serving a file
// should stay within a few percent of a bare http.FileServer, and the listing
// page is the one handler that allocates per entry.
func benchSite(b *testing.B, files int) string {
	b.Helper()
	root := b.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < files; i++ {
		name := filepath.Join(root, fmt.Sprintf("f%04d.html", i))
		if err := os.WriteFile(name, make([]byte, 4096), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	return root
}

func runBench(b *testing.B, h http.Handler, path string) {
	b.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec.Body.Reset()
		h.ServeHTTP(rec, req)
	}
}

func BenchmarkGetFile(b *testing.B) {
	runBench(b, newHandler(benchSite(b, 0), Options{}), "/a.txt")
}

func BenchmarkGetListing(b *testing.B) {
	runBench(b, newHandler(benchSite(b, 200), Options{}), "/")
}

// The first page still scans the folder; the next page should be mostly the
// cost of rendering 100 rows, because the scan comes from the listing cache.
func BenchmarkGetListingNextPage(b *testing.B) {
	root := benchSite(b, 1000)
	handler := newHandler(root, Options{})

	// Warm the snapshot so the benchmark measures the cache path, not the scan.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	runBench(b, handler, "/?page=1")
}
