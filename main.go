// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/0xChance/docwebhub/internal/cad"
	"github.com/0xChance/docwebhub/internal/config"
	"github.com/0xChance/docwebhub/internal/i18n"
	"github.com/0xChance/docwebhub/internal/office"
	"github.com/0xChance/docwebhub/internal/openurl"
	"github.com/0xChance/docwebhub/internal/ui"
	"github.com/0xChance/docwebhub/internal/update"
	"github.com/0xChance/docwebhub/internal/web"
)

var version = "dev"

const (
	defaultStartPort = 9102
	defaultDistName  = "dist"
	shutdownTimeout  = 5 * time.Second
)

func main() {
	// AppKit builds the main menu and runs the event loop on the process's
	// main thread. The goroutine that runs main() starts there, but every
	// blocking call before ui.Run — reading the config, listening on the port —
	// can leave it on another thread, and the LockOSThread inside ui.Run would
	// then pin the wrong one (an assertion failure in setMainMenu:). Pin it
	// first, while it is certainly the main thread.
	runtime.LockOSThread()

	distFlag := flag.String("dist", defaultDistName, "static site root folder (overrides the config file)")
	noGUI := flag.Bool("no-gui", false, "run the web server only, without the GUI")
	openInBrowser := flag.Bool("open", false, "open the site in the default browser after start")
	langFlag := flag.String("lang", "", "interface language code, such as en or zh-CN; empty follows the config, which follows the system (overrides the config file)")
	officeDir := flag.String("office-dir", "", "serve the Office viewer from this folder instead of the embedded copy, for trying a newer upstream build")
	cadDir := flag.String("cad-dir", "", "serve the CAD viewer from this folder instead of the embedded copy, for trying a newer upstream build")
	cadFonts := flag.String("cad-fonts-dir", "", "serve CAD fonts from this folder (SHX, TTF, OTF, WOFF) so drawings open in the font they were drawn with")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("docWebHub %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
		return
	}

	setFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	// Load the translations before anything user-visible is printed; an empty
	// value (the default) follows the language of the operating system.
	i18n.LoadDefault()

	cfg, err := config.Load()
	if err != nil {
		log.Printf("could not read the config, starting with defaults: %v", err)
		cfg = config.Config{}
	}

	switch {
	case setFlags["lang"]:
		i18n.SetLanguage(*langFlag)
	default:
		i18n.SetLanguage(cfg.Language)
	}

	root, err := selectRoot(*distFlag, setFlags["dist"], cfg.Root)
	rootMissing := false
	if err != nil {
		// Without a GUI there is nobody to pick a folder, so fail fast.
		// With a GUI, open the window anyway and let the user choose one.
		if *noGUI {
			fatal("%v", err)
		}
		log.Printf("warning: %v", err)
		root = fallbackRoot(*distFlag)
		rootMissing = true
	}

	bind := listenBind(cfg)

	startPort := defaultStartPort
	if cfg.Port >= 1 && cfg.Port <= 65535 {
		startPort = cfg.Port
	}

	// The viewers are optional: a build without their tags, or with a broken
	// asset folder, simply serves no preview for those formats.
	var officeHandler http.Handler
	officeVersion := ""
	if office.Enabled() || *officeDir != "" {
		viewer, err := office.New(*officeDir)
		if err != nil {
			log.Printf("office preview disabled: %v", err)
		} else {
			officeHandler = viewer.Handler()
			officeVersion = viewer.Version()
		}
	}

	var cadHandler http.Handler
	cadVersion := ""
	if cad.Enabled() || *cadDir != "" {
		viewer, err := cad.New(*cadDir, *cadFonts)
		if err != nil {
			log.Printf("cad preview disabled: %v", err)
		} else {
			cadHandler = viewer.Handler()
			cadVersion = viewer.Version()
		}
	}

	// The release channel is optional and must never stop the app from
	// starting: a failure here means no banners, not a broken program.
	var updateClient *update.Client
	if cfg.UpdateCheckEnabled() {
		cacheDir, err := update.CacheDir()
		if err != nil {
			log.Printf("update checks disabled: %v", err)
		} else if client, err := update.New(cacheDir); err != nil {
			log.Printf("update checks disabled: %v", err)
		} else {
			client.Lang = i18n.ActiveCode()
			// Windows needs the tray window the UI layer owns before it can
			// post a balloon. The other platforms implement notifications
			// inside internal/update and ignore this.
			update.SetNotifier(ui.NotifyUpdate)
			client.Start(context.Background())
			updateClient = client
			log.Printf("update checks: %s", client.BaseURL())
		}
	}

	auth := authFromConfig(cfg)
	srv, err := web.New(root, startPort, web.Options{Bind: bind, Auth: auth, Office: officeHandler, CAD: cadHandler, Update: updateClient, Version: version})
	if err != nil && bind != web.AnyBind {
		// The saved address may be gone (a Tailscale tunnel that is down, say);
		// fall back to every interface rather than refusing to start.
		log.Printf("cannot bind %s (%v), falling back to %s", bind, err, web.AnyBind)
		bind = web.AnyBind
		srv, err = web.New(root, startPort, web.Options{Bind: bind, Auth: auth, Office: officeHandler, CAD: cadHandler, Update: updateClient, Version: version})
	}
	if err != nil && bind != web.DefaultBind {
		// Last resort: loopback always works, even without a network.
		log.Printf("cannot bind %s (%v), falling back to %s", bind, err, web.DefaultBind)
		bind = web.DefaultBind
		srv, err = web.New(root, startPort, web.Options{Bind: bind, Auth: auth, Office: officeHandler, CAD: cadHandler, Update: updateClient, Version: version})
	}
	if err != nil {
		fatal("cannot start the web server: %v", err)
	}
	if cfg.Port != 0 && srv.Port() != cfg.Port {
		log.Printf("configured port %d is unavailable, using %d instead", cfg.Port, srv.Port())
	}

	go func() {
		// net.ErrClosed joins ErrServerClosed here because a rebind closes the
		// old listener out from under this Serve call on purpose.
		if err := srv.Serve(); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			log.Printf("web server stopped unexpectedly: %v", err)
		}
	}()

	fmt.Printf("docWebHub %s\n", version)
	fmt.Printf("  %-9s %s\n", i18n.T("cli.listen"), srv.Bind())
	if rootMissing {
		fmt.Printf("  %-9s %s\n", i18n.T("cli.root"), i18n.Tf("cli.root.missing", root))
	} else {
		fmt.Printf("  %-9s %s\n", i18n.T("cli.root"), root)
	}
	fmt.Printf("  %-9s %s\n", i18n.T("cli.url"), srv.URL())
	if auth != nil {
		fmt.Printf("  %-9s %s\n", i18n.T("cli.auth"), i18n.Tf("cli.auth.on", auth.Username))
	} else {
		fmt.Printf("  %-9s %s\n", i18n.T("cli.auth"), i18n.T("cli.auth.off"))
	}
	if officeHandler != nil {
		fmt.Printf("  %-9s %s\n", i18n.T("cli.office"), officeVersion)
	} else {
		fmt.Printf("  %-9s %s\n", i18n.T("cli.office"), i18n.T("cli.office.none"))
	}
	if cadHandler != nil {
		fmt.Printf("  %-9s %s\n", i18n.T("cli.cad"), cadVersion)
	} else {
		fmt.Printf("  %-9s %s\n", i18n.T("cli.cad"), i18n.T("cli.cad.none"))
	}
	if path, err := config.Path(); err == nil {
		fmt.Printf("  %-9s %s\n", i18n.T("cli.config"), path)
	}

	if *openInBrowser {
		if err := openurl.Open(srv.URL()); err != nil {
			log.Printf("could not open the browser: %v", err)
		}
	}

	// On macOS closing the window terminates the process and ui.Run never
	// returns, so the server shutdown is hooked here instead.
	ui.SetQuitHook(func() { shutdown(srv) })

	if *noGUI {
		fmt.Println("  " + i18n.T("cli.stop"))
		waitForSignal()
	} else {
		err := ui.Run(ui.Options{
			URL:          srv.URL(),
			Port:         srv.Port(),
			Bind:         srv.Bind(),
			BindOptions:  bindOptions(),
			Root:         root,
			RootMissing:  rootMissing,
			Stats:        overviewStats(srv),
			RefreshStats: func() ui.Stats { return overviewStats(srv) },
			AuthEnabled:  auth != nil,
			AuthUsername: cfg.Username,
			ChooseRoot: func(dir string) error {
				return applyRoot(srv, &cfg, dir)
			},
			SetPort: func(port int) (string, error) {
				return applyPort(srv, &cfg, port)
			},
			SetBind: func(bind string) (string, error) {
				return applyBind(srv, &cfg, bind)
			},
			SetAuth: func(username, password string) error {
				return applyAuth(srv, &cfg, username, password)
			},
			AccessInfo: func() string {
				return formatAccessInfo(srv)
			},
			Languages: uiLanguages(),
			Language:  cfg.Language,
			SetLanguage: func(code string) []ui.BindOption {
				applyLanguage(&cfg, code)
				return bindOptions()
			},
		})
		if err != nil {
			log.Printf("GUI unavailable: %v", err)
			fmt.Println("  " + i18n.T("cli.stop.fallback"))
			waitForSignal()
		}
	}

	shutdown(srv)
}

// uiLanguages lists the bundled translations for the language picker.
func uiLanguages() []ui.LangOption {
	langs := i18n.Languages()
	options := make([]ui.LangOption, 0, len(langs))
	for _, lang := range langs {
		options = append(options, ui.LangOption{Code: lang.Code, Name: lang.Name})
	}
	return options
}

// applyLanguage switches the interface language and persists it.
func applyLanguage(cfg *config.Config, code string) {
	i18n.SetLanguage(code)
	cfg.Language = code
	if err := config.Save(*cfg); err != nil {
		log.Printf("could not save the config: %v (the new language is still active)", err)
	}
	fmt.Printf("  %s\n", i18n.Tf("cli.language.changed", code))
}

// listenBind decides the startup listen address. An empty config means every
// interface: opening the folder on a phone is the point, and loopback can never
// do that.
func listenBind(cfg config.Config) string {
	if cfg.Bind != "" {
		return cfg.Bind
	}
	return web.AnyBind
}

// bindOptions builds the entries of the "Bind" dropdown.
func bindOptions() []ui.BindOption {
	options := []ui.BindOption{
		{Label: i18n.T("ui.bind.all"), Value: web.AnyBind},
		{Label: i18n.T("ui.bind.local"), Value: web.DefaultBind},
	}
	for _, addr := range web.LocalAddresses() {
		label := i18n.Tf("ui.bind.interface", addr.Interface, addr.IP)
		if web.IsTailscale(addr.IP) {
			label = i18n.Tf("ui.bind.tailscale", label)
		}
		options = append(options, ui.BindOption{Label: label, Value: addr.IP})
	}
	return options
}

// overviewStats turns the server's counters into the strings the window shows.
// All formatting happens here so the UI package stays free of clocks and
// locales.
func overviewStats(srv *web.Server) ui.Stats {
	stats := ui.Stats{}

	if started := srv.Started(); !started.IsZero() {
		stats.Uptime = formatUptime(time.Since(started))
	}

	now := time.Now()
	for _, d := range srv.Devices() {
		stats.Devices = append(stats.Devices, ui.StatDevice{
			Addr: d.Addr,
			Seen: formatSeen(now.Sub(d.Last)),
			Hits: d.Hits,
		})
		if len(stats.Devices) >= 5 {
			break // the window names the few that matter, not a full log
		}
	}

	stats.Hourly = srv.HourlyRequests()
	stats.Recent = srv.RecentFiles()
	stats.Requests = srv.TotalRequests()

	files, dirs, truncated := srv.CountTree()
	stats.Files, stats.Dirs, stats.Truncated = files, dirs, truncated
	return stats
}

// formatUptime renders a duration the way a status line would: minutes below an
// hour, hours and minutes below a day, days above that.
func formatUptime(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	mins := int(d.Minutes())
	if mins < 60 {
		return fmt.Sprintf("%dm", mins)
	}
	hours, mins := mins/60, mins%60
	if hours < 24 {
		if mins == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	days := hours / 24
	return fmt.Sprintf("%dd %dh", days, hours%24)
}

// formatSeen keeps the "last seen" column to three characters.
func formatSeen(d time.Duration) string {
	switch {
	case d < time.Minute:
		return i18n.T("ui.overview.now")
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// applyBind switches the listen address and persists it.
func applyBind(srv *web.Server, cfg *config.Config, bind string) (string, error) {
	url, err := srv.SetBind(bind)
	if err != nil {
		return "", listenError(err, bind, srv.Port())
	}

	cfg.Bind = bind
	if err := config.Save(*cfg); err != nil {
		log.Printf("could not save the config: %v (the new bind address is still active)", err)
	}

	fmt.Printf("  bind address changed: %s\n", bind)
	return url, nil
}

// listenError words a failed bind in the active language. The Go error carries
// platform text ("bind: address already in use") that belongs in the log, not
// in a dialog, so only the translated sentence is returned.
func listenError(err error, bind string, port int) error {
	addr := net.JoinHostPort(bind, strconv.Itoa(port))
	if errors.Is(err, web.ErrAddressInUse) {
		return errors.New(i18n.Tf("ui.error.portInUse", addr))
	}

	log.Printf("cannot listen on %s: %v", addr, err)
	return errors.New(i18n.Tf("ui.error.bind", addr))
}

// authFromConfig turns the saved credentials into the form the server expects.
func authFromConfig(cfg config.Config) *web.Auth {
	if cfg.Username == "" || cfg.PasswordHash == "" {
		return nil
	}
	return &web.Auth{Username: cfg.Username, Hash: cfg.PasswordHash}
}

// resolveAuth decides the new password settings from what the user typed.
// A nil result means "no password".
//
// Three cases:
//   - both fields empty: turn the password off
//   - a password was typed: hash it
//   - only the username, unchanged: keep the saved hash, so the password does
//     not have to be retyped just to re-apply
func resolveAuth(cfg config.Config, username, password string) (*web.Auth, error) {
	username = strings.TrimSpace(username)

	switch {
	case username == "" && password == "":
		return nil, nil
	case username == "":
		return nil, errors.New(i18n.T("ui.error.username"))
	case password != "":
		hash, err := web.HashPassword(password)
		if err != nil {
			return nil, err
		}
		return &web.Auth{Username: username, Hash: hash}, nil
	case cfg.Username == username && cfg.PasswordHash != "":
		return &web.Auth{Username: username, Hash: cfg.PasswordHash}, nil
	default:
		return nil, errors.New(i18n.T("ui.error.password"))
	}
}

// applyAuth updates the access password and persists it.
func applyAuth(srv *web.Server, cfg *config.Config, username, password string) error {
	auth, err := resolveAuth(*cfg, username, password)
	if err != nil {
		return err
	}

	srv.SetAuth(auth)
	if auth == nil {
		cfg.Username = ""
		cfg.PasswordHash = ""
		fmt.Println("  access password disabled")
	} else {
		cfg.Username = auth.Username
		cfg.PasswordHash = auth.Hash
		fmt.Printf("  access password enabled: %s\n", auth.Username)
	}

	if err := config.Save(*cfg); err != nil {
		log.Printf("could not save the config: %v (the password setting is still active)", err)
	}
	return nil
}

// formatAccessInfo renders the text shown in the "Active connections" window.
func formatAccessInfo(srv *web.Server) string {
	records, total, started := srv.AccessSnapshot()

	var b strings.Builder
	fmt.Fprintf(&b, "%-11s %s\n", i18n.T("access.address"), srv.URL())
	fmt.Fprintf(&b, "%-11s %s\n", i18n.T("access.started"), started.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "%-11s %s\n", i18n.T("access.uptime"), humanDuration(time.Since(started)))
	fmt.Fprintf(&b, "%-11s %s\n", i18n.T("access.requests"), i18n.Tf("access.requests.value", total, len(records)))
	if user := srv.AuthUser(); user != "" {
		fmt.Fprintf(&b, "%-11s %s\n\n", i18n.T("access.password"), i18n.Tf("access.password.on", user))
	} else {
		fmt.Fprintf(&b, "%-11s %s\n\n", i18n.T("access.password"), i18n.T("access.password.off"))
	}

	if len(records) == 0 {
		b.WriteString(i18n.T("access.empty") + "\n")
		return b.String()
	}

	b.WriteString(i18n.T("access.order") + "\n")
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		fmt.Fprintf(&b, "%-9s %-5s %-4d %-6s %-20s %s\n",
			record.Time.Format("15:04:05"),
			record.Method,
			record.Status,
			humanDuration(record.Duration),
			record.RemoteAddr,
			record.Path,
		)
	}

	b.WriteString("\n" + i18n.T("access.note") + "\n")
	return b.String()
}

// humanDuration formats a duration as a short, readable string.
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%.0fmin", d.Minutes())
	default:
		return fmt.Sprintf("%.1fh", d.Hours())
	}
}

// selectRoot decides which folder to serve.
//
// Priority: an explicit -dist flag > the folder saved in the config > the
// default dist/ folder next to the executable.
func selectRoot(dist string, distSet bool, saved string) (string, error) {
	if distSet {
		return resolveDistDir(dist)
	}

	if saved != "" {
		if info, err := os.Stat(saved); err == nil && info.IsDir() {
			return filepath.Clean(saved), nil
		}
		log.Printf("saved root folder is not usable: %s, falling back to the default", saved)
	}

	return resolveDistDir(defaultDistName)
}

// applyRoot switches the served folder and persists it.
func applyRoot(srv *web.Server, cfg *config.Config, dir string) error {
	abs, err := validateDir(dir)
	if err != nil {
		return err
	}

	srv.SetRoot(abs)
	cfg.Root = abs
	if err := config.Save(*cfg); err != nil {
		log.Printf("could not save the config: %v (the new root is still active)", err)
	}

	fmt.Printf("  root changed: %s\n", abs)
	return nil
}

// applyPort switches the listening port and persists it.
func applyPort(srv *web.Server, cfg *config.Config, port int) (string, error) {
	url, err := srv.SetPort(port)
	if err != nil {
		return "", listenError(err, srv.Bind(), port)
	}

	cfg.Port = port
	if err := config.Save(*cfg); err != nil {
		log.Printf("could not save the config: %v (the new port is still active)", err)
	}

	fmt.Printf("  port changed: %s\n", url)
	return url, nil
}

// fallbackRoot returns a placeholder path when no site folder exists, so the
// GUI has something to show until the user picks a real one.
func fallbackRoot(dir string) string {
	if candidates := distCandidates(dir); len(candidates) > 0 {
		return candidates[0]
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// resolveDistDir locates the static site folder.
//
// A relative path is tried next to the executable first, then relative to the
// working directory. That makes the default "dist/ next to the binary" work for
// a double-clicked build, while `go run .` still finds the project folder.
func resolveDistDir(dir string) (string, error) {
	candidates := distCandidates(dir)

	tried := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil {
			tried = append(tried, candidate)
			continue
		}
		if !info.IsDir() {
			return "", fmt.Errorf("static site path is not a folder: %s", candidate)
		}
		return candidate, nil
	}

	if len(tried) == 0 {
		return "", fmt.Errorf("cannot resolve static site folder %q", dir)
	}
	return "", fmt.Errorf("static site folder not found (tried %s); create it, pass -dist, or place it next to the executable",
		strings.Join(tried, ", "))
}

// distCandidates lists the absolute paths to try, in priority order.
//
// Order: next to the executable, one level up, then the working directory.
// The middle step exists for output folders such as build/ or bin/: build.sh
// writes binaries there while the site folder stays in the project root, so a
// double-clicked build has to look one level up.
func distCandidates(dir string) []string {
	if filepath.IsAbs(dir) {
		return []string{filepath.Clean(dir)}
	}

	var candidates []string
	add := func(candidate string) {
		candidate = filepath.Clean(candidate)
		if !containsPath(candidates, candidate) {
			candidates = append(candidates, candidate)
		}
	}

	if exeDir := executableDir(); exeDir != "" {
		add(filepath.Join(exeDir, dir))
		add(filepath.Join(filepath.Dir(exeDir), dir))

		// Inside a macOS .app the binary lives in Contents/MacOS, so also look
		// in Resources and next to the bundle; that keeps "app + site folder"
		// distributions working.
		if bundle, ok := appBundleDir(exeDir); ok {
			add(filepath.Join(bundle, "Contents", "Resources", dir))
			add(filepath.Join(filepath.Dir(bundle), dir))
			add(filepath.Join(filepath.Dir(filepath.Dir(bundle)), dir))
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		add(abs)
	}
	return candidates
}

// appBundleDir reports whether dir sits inside a macOS .app's Contents/MacOS
// and returns the bundle path if so.
func appBundleDir(exeDir string) (string, bool) {
	contents := filepath.Dir(exeDir)
	if filepath.Base(contents) != "Contents" {
		return "", false
	}
	bundle := filepath.Dir(contents)
	if filepath.Ext(bundle) != ".app" {
		return "", false
	}
	return bundle, true
}

// executableDir returns the folder holding the executable, or "" if unknown.
func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

func containsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}

// validateDir checks that dir exists and is readable, returning an absolute path.
func validateDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("cannot resolve folder %q: %w", dir, err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("cannot access folder %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a folder: %s", abs)
	}
	return abs, nil
}

func waitForSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	signal.Stop(ch)
}

func shutdown(srv *web.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("failed to stop the web server: %v", err)
	}
}

func fatal(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "docwebhub: %s\n", message)
	appendLog(message)
	os.Exit(1)
}

// appendLog appends the message to docwebhub.log in the config folder.
// A double-clicked .app has no terminal, so this file is the only way to see
// why startup failed.
func appendLog(message string) {
	dir, err := config.Dir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	file, err := os.OpenFile(filepath.Join(dir, "docwebhub.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()

	fmt.Fprintf(file, "%s docwebhub: %s\n", time.Now().Format(time.RFC3339), message)
}
