// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

// Package config reads and writes docWebHub's user configuration.
//
// The config file lives in the system's user config directory:
//   - macOS:   ~/Library/Application Support/docwebhub/config.json
//   - Windows: %AppData%\docwebhub\config.json
//   - Linux:   $XDG_CONFIG_HOME/docwebhub/config.json (defaults to ~/.config)
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Config is the user configuration persisted to disk.
type Config struct {
	// Root is the web root the user picked last time; empty means use the default directory.
	Root string `json:"root,omitempty"`
	// Port is the port the user chose; 0 means use the default port.
	Port int `json:"port,omitempty"`
	// Bind is the listen address the user chose; empty means every interface at
	// startup, which is what the window offers first.
	Bind string `json:"bind,omitempty"`
	// Username is the user name used when access auth is enabled; empty means no auth check.
	Username string `json:"username,omitempty"`
	// PasswordHash is the password digest (sha256$salt$digest); the plain text is never stored.
	PasswordHash string `json:"passwordHash,omitempty"`
	// Language is the code of the interface language (for example "zh-CN").
	// An empty value follows the language of the operating system.
	Language string `json:"language,omitempty"`
	// UpdateCheck turns the release-notice channel on or off. Empty means the
	// built-in default ("on"); "off" disables it, which also stops the app from
	// making its daily request to the website.
	UpdateCheck string `json:"updateCheck,omitempty"`
	// Telemetry records whether the user allowed usage reporting.
	Telemetry string `json:"telemetry,omitempty"`
}

// UpdateCheckOff is the value that disables the update channel.
const UpdateCheckOff = "off"

// UpdateCheckEnabled reports whether the app should check for release notices.
func (c Config) UpdateCheckEnabled() bool {
	return c.UpdateCheck != UpdateCheckOff
}

func (c Config) TelemetryConsent() string {
	if c.Telemetry == "granted" {
		return "granted"
	}
	if c.Telemetry == "denied" {
		return "denied"
	}
	return ""
}

// Dir returns the directory that holds the config file.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate user config dir: %w", err)
	}
	return filepath.Join(base, "docwebhub"), nil
}

// legacyDirNames are the folders this program used before its current name,
// newest first. They are read in order, but only when no settings exist under
// the current name, so the site folder, port and password survive a rename;
// the next save writes them under the current name and the old folders are
// ignored from then on.
var legacyDirNames = []string{"webdoccast", "littleweb"}

// legacyPaths returns the pre-rename config files, newest first. It returns an
// empty slice when the user config dir cannot be located.
func legacyPaths() []string {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(legacyDirNames))
	for _, name := range legacyDirNames {
		paths = append(paths, filepath.Join(base, name, "config.json"))
	}
	return paths
}

// Path returns the absolute path of the config file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads the user configuration; a missing file yields a zero-value config.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}

	cfg, err := loadFrom(path)
	if err != nil {
		return Config{}, err
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
		return cfg, nil
	}

	// A previous name's settings only count when the file is really there:
	// loadFrom reports a missing file as an empty config, which would otherwise
	// let the newest legacy name hide a folder that still holds settings.
	for _, old := range legacyPaths() {
		if _, statErr := os.Stat(old); statErr != nil {
			continue
		}
		if legacy, legacyErr := loadFrom(old); legacyErr == nil {
			return legacy, nil
		}
	}
	return cfg, nil
}

// Save writes the configuration into the user config directory.
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return saveTo(path, cfg)
}

func loadFrom(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

func saveTo(path string, cfg Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	// Write a temp file first and rename it afterwards, so an interrupted write
	// never leaves a half-written config file behind.
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("save config %s: %w", path, err)
	}
	return nil
}
