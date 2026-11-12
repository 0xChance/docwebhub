// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/0xChance/docwebhub/internal/i18n"
)

// Auth is one set of access credentials: a username plus a salted password digest.
//
// Only the digest is stored, so the plaintext password appears neither in the
// config file nor in memory. The digest is "random salt + SHA-256", which is
// fast enough to verify on every request; it deters casual access from the same
// network, not offline brute force — so do not pick a weak password.
type Auth struct {
	Username string
	// Hash looks like sha256$<salt>$<digest> and is produced by HashPassword.
	Hash string
}

// Enabled reports whether this credential set is complete and usable. nil counts as disabled.
func (a *Auth) Enabled() bool {
	return a != nil && a.Username != "" && a.Hash != ""
}

// HashPassword derives a digest for the password using a random salt.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password must not be empty")
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate random salt: %w", err)
	}

	return "sha256$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(digestPassword(salt, password)), nil
}

// Verify checks the username and password. A wrong username or password both
// return false, and the comparison is constant-time.
func (a *Auth) Verify(username, password string) bool {
	if !a.Enabled() {
		return true
	}

	parts := strings.Split(a.Hash, "$")
	if len(parts) != 3 || parts[0] != "sha256" {
		return false
	}

	salt, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}

	digestOK := subtle.ConstantTimeCompare(digestPassword(salt, password), want) == 1
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(a.Username)) == 1
	return digestOK && userOK
}

func digestPassword(salt []byte, password string) []byte {
	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(password))
	return h.Sum(nil)
}

// authorize checks the Basic Auth credentials on the request; it returns true
// when no password is required or when the check passes.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	auth := s.auth.Load()
	if !auth.Enabled() {
		return true
	}

	if username, password, ok := r.BasicAuth(); ok && auth.Verify(username, password) {
		return true
	}

	// Without this header the browser never shows its sign-in prompt.
	w.Header().Set("WWW-Authenticate", `Basic realm="docWebHub", charset="UTF-8"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	if err := unauthorizedTemplate.Execute(w, map[string]string{
		"Lang":  i18n.ActiveCode(),
		"Title": i18n.T("web.auth.title"),
		"Text":  i18n.T("web.auth.text"),
		"Hint":  i18n.T("web.auth.hint"),
	}); err != nil {
		log.Printf("failed to render the sign-in page: %v", err)
	}
	return false
}

// unauthorizedTemplate is served when the check fails; its styling matches the
// directory listing. The texts come from the active translation.
var unauthorizedTemplate = template.Must(template.New("unauthorized").Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
  :root { color-scheme: light dark; --fg:#1d2129; --muted:#5c6470; --card:#fff; --line:#e3e6eb; }
  @media (prefers-color-scheme: dark) {
    :root { --fg:#eceff3; --muted:#9aa3ad; --card:#1c1f24; --line:#2c3038; }
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center;
    padding: 32px; background: var(--card); color: var(--fg);
    font: 14px/1.6 -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif;
  }
  .panel { width: min(420px, 100%); border: 1px solid var(--line); border-radius: 12px; padding: 26px 28px; }
  h1 { margin: 0 0 8px; font-size: 17px; }
  p { margin: 0; color: var(--muted); font-size: 13px; }
  code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
</style>
</head>
<body>
  <div class="panel">
    <h1>{{.Title}}</h1>
    <p>{{.Text}}</p>
    <p>{{.Hint}}</p>
  </div>
</body>
</html>
`))
