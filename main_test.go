// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/0xChance/docwebhub/internal/config"
	"github.com/0xChance/docwebhub/internal/i18n"
	"github.com/0xChance/docwebhub/internal/web"
)

func TestResolveDistDirRelativeToWorkingDir(t *testing.T) {
	got, err := resolveDistDir("dist")
	if err != nil {
		t.Fatalf("resolveDistDir(dist): %v", err)
	}

	want, err := filepath.Abs("dist")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("folder = %q, want the working-directory fallback %q", got, want)
	}
}

func TestResolveDistDirAbsolute(t *testing.T) {
	root := t.TempDir()

	got, err := resolveDistDir(root)
	if err != nil {
		t.Fatalf("resolveDistDir(%q): %v", root, err)
	}
	if got != root {
		t.Fatalf("folder = %q, want %q", got, root)
	}
}

func TestResolveDistDirMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-site")

	_, err := resolveDistDir(missing)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("error should mention the path tried, %q; got %q", missing, err.Error())
	}
}

func TestResolveDistDirRejectsFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "index.html")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := resolveDistDir(file); err == nil {
		t.Fatal("a file path should be rejected")
	}
}

func TestDistCandidatesPutsExecutableDirFirst(t *testing.T) {
	candidates := distCandidates("public")
	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate")
	}

	exeDir := executableDir()
	if exeDir == "" {
		t.Skip("cannot resolve the executable path in this environment")
	}
	if want := filepath.Join(exeDir, "public"); candidates[0] != want {
		t.Fatalf("first candidate = %q, want %q next to the executable", candidates[0], want)
	}

	abs, err := filepath.Abs("public")
	if err != nil {
		t.Fatal(err)
	}
	if !containsPath(candidates, abs) {
		t.Fatalf("candidates should include %q from the working directory; got %v", abs, candidates)
	}
	for _, candidate := range candidates {
		if !filepath.IsAbs(candidate) {
			t.Fatalf("candidate should be absolute: %q", candidate)
		}
	}
}

func TestSelectRootUsesSavedDir(t *testing.T) {
	saved := t.TempDir()

	got, err := selectRoot("dist", false, saved)
	if err != nil {
		t.Fatalf("selectRoot: %v", err)
	}
	if got != saved {
		t.Fatalf("folder = %q, want the saved %q", got, saved)
	}
}

func TestSelectRootFallsBackWhenSavedMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")

	got, err := selectRoot("dist", false, missing)
	if err != nil {
		t.Fatalf("selectRoot: %v", err)
	}

	want, err := filepath.Abs("dist")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("folder = %q, want the fallback %q", got, want)
	}
}

func TestSelectRootPrefersExplicitDist(t *testing.T) {
	explicit := t.TempDir()
	saved := t.TempDir()

	got, err := selectRoot(explicit, true, saved)
	if err != nil {
		t.Fatalf("selectRoot: %v", err)
	}
	if got != explicit {
		t.Fatalf("folder = %q, want the explicit %q", got, explicit)
	}
}

func TestValidateDir(t *testing.T) {
	dir := t.TempDir()
	got, err := validateDir(dir)
	if err != nil {
		t.Fatalf("validateDir: %v", err)
	}
	if got != dir {
		t.Fatalf("folder = %q, want %q", got, dir)
	}

	file := filepath.Join(dir, "index.html")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := validateDir(file); err == nil {
		t.Fatal("a file path should be rejected")
	}

	if _, err := validateDir(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing path should be rejected")
	}
}

func TestDistCandidatesIncludesExecutableParent(t *testing.T) {
	exeDir := executableDir()
	if exeDir == "" {
		t.Skip("cannot resolve the executable path in this environment")
	}

	candidates := distCandidates("site")
	want := filepath.Join(filepath.Dir(exeDir), "site")
	if !containsPath(candidates, want) {
		t.Fatalf("candidates should include %q from the parent of the executable; got %v", want, candidates)
	}
}

func TestApplyRootSwitchesServerAndSavesConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "appdata"))

	site := t.TempDir()
	if err := os.WriteFile(filepath.Join(site, "index.html"), []byte("picked"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := web.New(t.TempDir(), 9101, web.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	cfg := config.Config{}
	if err := applyRoot(srv, &cfg, site); err != nil {
		t.Fatalf("applyRoot: %v", err)
	}
	if srv.Root() != site {
		t.Fatalf("server root = %q, want %q", srv.Root(), site)
	}

	saved, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.Root != site {
		t.Fatalf("saved root = %q, want %q", saved.Root, site)
	}
}

func TestApplyRootRejectsMissingDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := t.TempDir()
	srv, err := web.New(root, 9101, web.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	cfg := config.Config{}
	if err := applyRoot(srv, &cfg, filepath.Join(home, "missing")); err == nil {
		t.Fatal("a missing folder should be rejected")
	}
	if srv.Root() != root {
		t.Fatalf("root should not change on failure; got %q", srv.Root())
	}
}

func TestAppBundleDir(t *testing.T) {
	exeDir := filepath.Join("/Applications", "docWebHub.app", "Contents", "MacOS")
	got, ok := appBundleDir(exeDir)
	if !ok {
		t.Fatalf("should be recognised as an .app bundle path: %s", exeDir)
	}
	if want := filepath.Join("/Applications", "docWebHub.app"); got != want {
		t.Fatalf("bundle = %q, want %q", got, want)
	}

	if _, ok := appBundleDir("/usr/local/bin"); ok {
		t.Fatal("an ordinary folder is not an .app bundle")
	}
	if _, ok := appBundleDir(filepath.Join(t.TempDir(), "Contents", "MacOS")); ok {
		t.Fatal("a Contents folder outside an .app should not match")
	}
}

func TestApplyPortSwitchesAndPersists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "appdata"))

	srv, err := web.New(t.TempDir(), freeTCPPort(t), web.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	port := freeTCPPort(t)
	cfg := config.Config{}
	url, err := applyPort(srv, &cfg, port)
	if err != nil {
		t.Fatalf("applyPort: %v", err)
	}
	if srv.Port() != port {
		t.Fatalf("port = %d, want %d", srv.Port(), port)
	}
	if want := "http://127.0.0.1:" + strconv.Itoa(port); url != want {
		t.Fatalf("url = %q, want %q", url, want)
	}

	saved, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.Port != port {
		t.Fatalf("saved port = %d, want %d", saved.Port, port)
	}
}

func TestApplyPortRejectsBusyPort(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind a test port: %v", err)
	}
	defer occupied.Close()
	busy := occupied.Addr().(*net.TCPAddr).Port

	srv, err := web.New(t.TempDir(), freeTCPPort(t), web.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	before := srv.Port()
	cfg := config.Config{}
	if _, err := applyPort(srv, &cfg, busy); err == nil {
		t.Fatalf("busy port %d should be rejected", busy)
	}
	if srv.Port() != before {
		t.Fatalf("port changed to %d on failure; want to keep %d", srv.Port(), before)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot get a free port: %v", err)
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port
}

func TestFormatAccessInfo(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := web.New(root, freeTCPPort(t), web.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(srv.URL() + "/hello")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	text := formatAccessInfo(srv)
	for _, want := range []string{"Address", "Requests", "Password", "GET", "404", "/hello", "127.0.0.1:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("access info is missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, srv.URL()) {
		t.Fatalf("access info should contain the address %q:\n%s", srv.URL(), text)
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{500 * time.Microsecond, "500µs"},
		{12 * time.Millisecond, "12ms"},
		{1500 * time.Millisecond, "1.5s"},
		{90 * time.Second, "2min"},
		{3 * time.Hour, "3.0h"},
	}

	for _, tc := range cases {
		if got := humanDuration(tc.in); got != tc.want {
			t.Errorf("humanDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBindOptionsIncludesDefaults(t *testing.T) {
	options := bindOptions()
	if len(options) < 2 {
		t.Fatalf("too few options: %d", len(options))
	}
	// Every interface comes first because that is what the app starts on.
	if options[0].Value != web.AnyBind {
		t.Fatalf("first option = %q, want every interface %q", options[0].Value, web.AnyBind)
	}
	if options[1].Value != web.DefaultBind {
		t.Fatalf("second option = %q, want localhost %q", options[1].Value, web.DefaultBind)
	}
	for _, option := range options {
		if option.Label == "" || option.Value == "" {
			t.Fatalf("option is missing a label or a value: %+v", option)
		}
		if strings.HasPrefix(option.Value, "127.") && option.Value != web.DefaultBind {
			t.Fatalf("loopback addresses should not be listed separately: %+v", option)
		}
	}
}

func TestApplyBindSwitchesAndPersists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "appdata"))

	srv, err := web.New(t.TempDir(), freeTCPPort(t), web.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	cfg := config.Config{}
	if _, err := applyBind(srv, &cfg, "0.0.0.0"); err != nil {
		t.Fatalf("applyBind: %v", err)
	}
	if srv.Bind() != "0.0.0.0" {
		t.Fatalf("Bind() = %q, want 0.0.0.0", srv.Bind())
	}

	saved, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.Bind != "0.0.0.0" {
		t.Fatalf("saved bind = %q, want 0.0.0.0", saved.Bind)
	}
}

func TestAuthFromConfig(t *testing.T) {
	if auth := authFromConfig(config.Config{}); auth != nil {
		t.Fatalf("no saved credentials should yield nil; got %+v", auth)
	}
	if auth := authFromConfig(config.Config{Username: "admin"}); auth != nil {
		t.Fatal("a username without a hash should not enable auth")
	}

	auth := authFromConfig(config.Config{Username: "admin", PasswordHash: "sha256$aa$bb"})
	if auth == nil || auth.Username != "admin" || auth.Hash != "sha256$aa$bb" {
		t.Fatalf("unexpected result: %+v", auth)
	}
}

func TestResolveAuth(t *testing.T) {
	saved := config.Config{Username: "admin", PasswordHash: "sha256$cafe$beef"}

	t.Run("empty disables it", func(t *testing.T) {
		auth, err := resolveAuth(saved, "   ", "")
		if err != nil {
			t.Fatalf("resolveAuth: %v", err)
		}
		if auth != nil {
			t.Fatalf("auth should be disabled; got %+v", auth)
		}
	})

	t.Run("password only", func(t *testing.T) {
		if _, err := resolveAuth(saved, "", "pw"); err == nil {
			t.Fatal("a password without a username should fail")
		}
	})

	t.Run("new username without a password", func(t *testing.T) {
		if _, err := resolveAuth(saved, "other", ""); err == nil {
			t.Fatal("a new username requires a password")
		}
	})

	t.Run("reuse the saved password", func(t *testing.T) {
		auth, err := resolveAuth(saved, " admin ", "")
		if err != nil {
			t.Fatalf("resolveAuth: %v", err)
		}
		if auth == nil || auth.Hash != saved.PasswordHash {
			t.Fatalf("should reuse the saved hash; got %+v", auth)
		}
		if auth.Username != "admin" {
			t.Fatalf("username should be trimmed; got %q", auth.Username)
		}
	})

	t.Run("set a new password", func(t *testing.T) {
		auth, err := resolveAuth(saved, "admin", "new-secret")
		if err != nil {
			t.Fatalf("resolveAuth: %v", err)
		}
		if auth == nil || auth.Hash == saved.PasswordHash {
			t.Fatalf("should produce a new hash; got %+v", auth)
		}
		if !auth.Verify("admin", "new-secret") {
			t.Fatal("the new hash should verify")
		}
		if auth.Verify("admin", "old-secret") {
			t.Fatal("the old password should not verify")
		}
	})
}

func TestApplyAuthPersists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "appdata"))

	srv, err := web.New(t.TempDir(), freeTCPPort(t), web.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	cfg := config.Config{}
	if err := applyAuth(srv, &cfg, "admin", "secret"); err != nil {
		t.Fatalf("applyAuth: %v", err)
	}
	if !srv.AuthEnabled() || srv.AuthUser() != "admin" {
		t.Fatalf("auth is not enabled: enabled=%v user=%q", srv.AuthEnabled(), srv.AuthUser())
	}

	saved, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.Username != "admin" || saved.PasswordHash == "" {
		t.Fatalf("credentials were not saved: %+v", saved)
	}
	if strings.Contains(saved.PasswordHash, "secret") {
		t.Fatalf("the config should not contain the plaintext password: %s", saved.PasswordHash)
	}

	// Empty password with an unchanged username: keep the saved hash.
	if err := applyAuth(srv, &cfg, "admin", ""); err != nil {
		t.Fatalf("applyAuth reuse: %v", err)
	}
	if !srv.AuthEnabled() {
		t.Fatal("reuse should not disable auth")
	}

	// Both empty: turn it off.
	if err := applyAuth(srv, &cfg, "", ""); err != nil {
		t.Fatalf("applyAuth disable: %v", err)
	}
	if srv.AuthEnabled() {
		t.Fatal("auth should be off")
	}
	saved, err = config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if saved.Username != "" || saved.PasswordHash != "" {
		t.Fatalf("clearing should empty the saved credentials: %+v", saved)
	}
}

// Pressing Apply with the port untouched used to fail, because the running
// listener owns it. The action is a no-op now.
func TestApplyPortWithCurrentPortSucceeds(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "appdata"))

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("still here"), 0o644); err != nil {
		t.Fatal(err)
	}

	startPort := freeTestPort(t)
	srv, err := web.New(root, startPort, web.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	go func() { _ = srv.Serve() }()

	cfg := config.Config{}
	url, err := applyPort(srv, &cfg, srv.Port())
	if err != nil {
		t.Fatalf("applying the current port %d: %v", srv.Port(), err)
	}
	if url != srv.URL() {
		t.Fatalf("url = %q, want %q", url, srv.URL())
	}
	if cfg.Port != srv.Port() {
		t.Fatalf("saved port = %d, want %d", cfg.Port, srv.Port())
	}

	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatalf("site after re-applying the same port: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

// The dialog must not leak the platform's English text.
//
// The expected wording is read from the translation table instead of being
// spelled out here, so the test carries no literal from any one language: it
// asserts the message came from i18n and kept the address, which is the part
// that would break if listenError ever returned the raw error again.
func TestListenErrorIsLocalized(t *testing.T) {
	i18n.SetLanguage("zh-CN")
	t.Cleanup(func() { i18n.SetLanguage("") })

	err := listenError(fmt.Errorf("%w: 127.0.0.1:9301", web.ErrAddressInUse), "127.0.0.1", 9301)
	msg := err.Error()
	if strings.Contains(msg, "address already in use") || strings.Contains(msg, "cannot bind") {
		t.Fatalf("message still contains Go/platform text: %q", msg)
	}
	if !strings.Contains(msg, "127.0.0.1:9301") {
		t.Fatalf("message = %q, want it to name the address that clashed", msg)
	}
	if want := i18n.Tf("ui.error.portInUse", "127.0.0.1:9301"); msg != want {
		t.Fatalf("message = %q, want the translated sentence %q", msg, want)
	}
}

// freeTestPort finds a port that is free right now.
func freeTestPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot get a free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// The app listens on every interface unless the user saved something else.
func TestListenBindDefaultsToAllInterfaces(t *testing.T) {
	if got := listenBind(config.Config{}); got != web.AnyBind {
		t.Fatalf("default bind = %q, want %q", got, web.AnyBind)
	}
	if got := listenBind(config.Config{Bind: "127.0.0.1"}); got != "127.0.0.1" {
		t.Fatalf("saved bind = %q, want it kept", got)
	}
}
