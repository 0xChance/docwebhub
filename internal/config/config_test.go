// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	want := Config{Root: "/srv/site", Port: 8123}

	if err := saveTo(path, want); err != nil {
		t.Fatalf("saveTo: %v", err)
	}

	got, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if got != want {
		t.Fatalf("config = %+v, want %+v", got, want)
	}
}

func TestLoadMissingFileReturnsZeroValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	got, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if got != (Config{}) {
		t.Fatalf("config = %+v, want zero value", got)
	}
}

func TestLoadRejectsBrokenJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := loadFrom(path); err == nil {
		t.Fatal("expected an error for malformed JSON")
	} else if !strings.Contains(err.Error(), path) {
		t.Fatalf("error should mention path %q, got %q", path, err.Error())
	}
}

func TestSaveOverwritesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	if err := saveTo(path, Config{Root: "/a"}); err != nil {
		t.Fatal(err)
	}
	if err := saveTo(path, Config{Root: "/b"}); err != nil {
		t.Fatal(err)
	}

	got, err := loadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != "/b" {
		t.Fatalf("config = %+v, want it overwritten with /b", got)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "config.json" {
			t.Fatalf("no temp files should be left behind: %s", entry.Name())
		}
	}
}

func TestPathUnderUserConfigDir(t *testing.T) {
	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if filepath.Base(path) != "config.json" {
		t.Fatalf("file name = %q, want config.json", filepath.Base(path))
	}
	if filepath.Base(filepath.Dir(path)) != "docwebhub" {
		t.Fatalf("dir = %q, want docwebhub", filepath.Dir(path))
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("path should be absolute: %q", path)
	}
}

func TestSaveAndLoadThroughUserConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "appdata"))

	path, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if !strings.HasPrefix(path, home) {
		t.Fatalf("config path %q should live under temp HOME %q", path, home)
	}

	want := Config{Root: "/tmp/site"}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("config = %+v, want %+v", got, want)
	}
}

func TestSaveAndLoadKeepsAuthFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	want := Config{Root: "/srv/site", Username: "admin", PasswordHash: "sha256$abcd$ef01"}

	if err := saveTo(path, want); err != nil {
		t.Fatalf("saveTo: %v", err)
	}

	got, err := loadFrom(path)
	if err != nil {
		t.Fatalf("loadFrom: %v", err)
	}
	if got != want {
		t.Fatalf("config = %+v, want %+v", got, want)
	}
}

func TestSaveUsesRestrictivePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveTo(path, Config{Username: "admin", PasswordHash: "sha256$aa$bb"}); err != nil {
		t.Fatalf("saveTo: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("config file mode = %o, other users must not be able to read it (the password digest is in there too)", perm)
	}
}

// Settings saved before the rename have to survive it; the app would otherwise
// start with no site folder and no password.
func TestLoadFallsBackToThePreRenameFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "appdata"))

	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	oldDir := filepath.Join(base, legacyDirNames[0])
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveTo(filepath.Join(oldDir, "config.json"), Config{Root: "/srv/old", Port: 9301}); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Root != "/srv/old" || got.Port != 9301 {
		t.Fatalf("config = %+v, want the pre-rename settings", got)
	}
}

// Once the new file exists it wins, even if the old one is still lying around.
func TestLoadPrefersTheNewFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "appdata"))

	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	oldDir := filepath.Join(base, legacyDirNames[0])
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveTo(filepath.Join(oldDir, "config.json"), Config{Root: "/srv/old"}); err != nil {
		t.Fatal(err)
	}
	if err := Save(Config{Root: "/srv/new", Port: 9400}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Root != "/srv/new" || got.Port != 9400 {
		t.Fatalf("config = %+v, want the settings under the new name", got)
	}
}

// A rename must not hide settings that sit two names back: a folder left over
// from the newest previous name, without a config file in it, would otherwise
// end the search before the older one is reached.
func TestLoadSkipsAnEmptyLegacyFolder(t *testing.T) {
	if len(legacyDirNames) < 2 {
		t.Skip("needs at least two previous names")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "appdata"))

	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(base, legacyDirNames[0]), 0o755); err != nil {
		t.Fatal(err)
	}
	oldest := filepath.Join(base, legacyDirNames[len(legacyDirNames)-1])
	if err := os.MkdirAll(oldest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveTo(filepath.Join(oldest, "config.json"), Config{Root: "/srv/oldest", Port: 9500}); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Root != "/srv/oldest" || got.Port != 9500 {
		t.Fatalf("config = %+v, want the settings two names back", got)
	}
}
