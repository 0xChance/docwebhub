// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package web

import (
	"bytes"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// updatePrefix is the same-origin path every injected script is served from.
//
// The browser must never talk to docwebhub.app directly: a phone opening the
// site over the LAN would hand its address to the website's CDN, and a site
// that declares script-src 'self' would block the script outright. Routing
// through the app keeps visitors invisible to the website and lets the app
// cap sizes, check hashes and fall back to a disk cache when offline.
const updatePrefix = "/__docwebhub/update/"

// moduleFile mirrors the whitelist in internal/update. Anything else is a 404:
// without it the proxy would be an open door to arbitrary URLs.
var moduleFile = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.js$`)

// serveUpdate answers the proxied manifest and scripts. It is answered before
// the site handler and without the site password, like the other application
// assets: it carries no user data, and it lives under the reserved prefix so a
// site file can never shadow it.
func (s *Server) serveUpdate(w http.ResponseWriter, r *http.Request) {
	if s.update == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snap := s.update.Current()
	if snap == nil {
		// Nothing verified yet: the loader retries on the next page load, and
		// until then the page simply has no banners.
		http.NotFound(w, r)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, updatePrefix)

	var body []byte
	var contentType string
	switch {
	case rest == "v1.js":
		body, contentType = snap.Loader, "text/javascript; charset=utf-8"

	case rest == "config.json":
		body, contentType = snap.Raw, "application/json; charset=utf-8"

	case strings.HasPrefix(rest, "modules/"):
		name := strings.TrimPrefix(rest, "modules/")
		if !moduleFile.MatchString(name) {
			http.NotFound(w, r)
			return
		}
		found := false
		for _, m := range snap.Release.Modules {
			if m.Src == "modules/"+name {
				body, found = snap.Modules[m.ID], true
				break
			}
		}
		if !found {
			log.Printf("update: module %q is not in the verified manifest", name)
			http.NotFound(w, r)
			return
		}
		contentType = "text/javascript; charset=utf-8"

	default:
		http.NotFound(w, r)
		return
	}

	if len(body) == 0 {
		http.NotFound(w, r)
		return
	}

	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")
	// Matches the client's refresh interval, so the browser and the app never
	// disagree about which manifest is current.
	h.Set("Cache-Control", "public, max-age=300")

	http.ServeContent(w, r, rest, time.Time{}, bytes.NewReader(body))
}
