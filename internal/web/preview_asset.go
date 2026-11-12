// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// previewAssetPath is the script the listing page loads for syntax highlighting
// and Markdown rendering. It lives under the reserved /__docwebhub/ namespace
// so a site file can never shadow it; serveHTTP answers it before the site
// handler, like the Office and CAD viewers.
const previewAssetPath = "/__docwebhub/preview/preview.js"

// previewJS is built by tools/build-listing-preview.sh and committed, so a
// plain `go build` needs no Node.js.
//
//go:embed preview.min.js
var previewJS []byte

// previewGzip is computed once: the bundle is immutable for the life of the
// process and gzip cuts its transfer size by about two thirds.
var previewGzip = gzipPreview(previewJS)

// previewETag changes whenever the embedded bundle changes.
var previewETag = func() string {
	sum := sha256.Sum256(previewJS)
	return fmt.Sprintf(`"%x"`, sum[:8])
}()

func servePreviewAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/javascript; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Vary", "Accept-Encoding")
	h.Set("X-Content-Type-Options", "nosniff")

	body := previewJS
	etag := previewETag
	if previewGzip != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		body = previewGzip
		etag = strings.TrimSuffix(previewETag, `"`) + `-gzip"`
		h.Set("Content-Encoding", "gzip")
	}
	h.Set("ETag", etag)

	// ServeContent handles If-None-Match, HEAD and byte ranges. The zero
	// modification time keeps Last-Modified out of the response.
	http.ServeContent(w, r, "preview.js", time.Time{}, bytes.NewReader(body))
}

func gzipPreview(data []byte) []byte {
	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil
	}
	if _, err := writer.Write(data); err != nil {
		return nil
	}
	if err := writer.Close(); err != nil {
		return nil
	}
	return buf.Bytes()
}
