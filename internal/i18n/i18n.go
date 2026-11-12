// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

// Package i18n serves the user-visible text of docWebHub.
//
// Translations live in the top-level locales/ directory as one flat JSON file
// per language (locales/en.json is the source of truth). They are embedded into
// the binary, so a single executable still ships every language; a directory of
// JSON files next to the binary (or pointed at by WEBDOCAST_LOCALES) overrides
// the embedded ones, which is handy while translating.
//
// Lookups go through T, which falls back to English for any key a translation
// is missing, so a partially translated file never breaks the interface.
package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/0xChance/docwebhub/locales"
)

const (
	// EnglishCode is bundled with the binary and used when nothing else matches.
	EnglishCode = "en"
	// DirEnv names the environment variable that points at a directory of JSON
	// files overriding the embedded translations.
	DirEnv = "WEBDOCAST_LOCALES"
	// DirName is the folder looked for next to the executable.
	DirName = "locales"

	nameKey = "language.name"
	engKey  = "language.english"
)

// Language is one bundled translation.
type Language struct {
	// Code is the language tag used as the file name and the config value.
	Code string
	// Name is the language in its own language, shown in the picker.
	Name string
	// English is the English name, kept readable whatever language is active.
	English string
}

// Bundle holds every loaded translation plus the active one.
type Bundle struct {
	langs    []Language
	data     map[string]map[string]string
	fallback map[string]string
	current  atomic.Pointer[state]
}

type state struct {
	code string
	data map[string]string
}

// Load reads the embedded translations, then any overriding directory.
func Load() (*Bundle, error) {
	entries, err := locales.FS.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("read embedded locales: %w", err)
	}

	b := &Bundle{data: make(map[string]map[string]string)}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := locales.FS.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read embedded locale %s: %w", entry.Name(), err)
		}
		values, err := decode(raw)
		if err != nil {
			return nil, fmt.Errorf("parse embedded locale %s: %w", entry.Name(), err)
		}
		b.data[codeFromName(entry.Name())] = values
	}

	if _, ok := b.data[EnglishCode]; !ok {
		return nil, fmt.Errorf("embedded locales are missing %s.json", EnglishCode)
	}
	b.fallback = b.data[EnglishCode]
	b.sortLanguages()

	if dir := overrideDir(); dir != "" {
		if err := b.MergeDir(dir); err != nil {
			// A broken override must not stop the app; the embedded text is fine.
			_ = err
		}
	}

	b.Set(EnglishCode)
	return b, nil
}

// LoadDefault loads the bundle used by the package-level helpers. It never
// fails: without translations the app runs in English.
func LoadDefault() *Bundle {
	b, err := Load()
	if err != nil {
		b = &Bundle{
			data:     map[string]map[string]string{},
			fallback: map[string]string{},
		}
	}
	defaultBundle.Store(b)
	return b
}

var defaultBundle atomic.Pointer[Bundle]

// Default returns the process-wide bundle, loading it on first use.
func Default() *Bundle {
	if b := defaultBundle.Load(); b != nil {
		return b
	}
	return LoadDefault()
}

// Languages lists every available translation, English first.
func (b *Bundle) Languages() []Language {
	out := make([]Language, len(b.langs))
	copy(out, b.langs)
	return out
}

// Has reports whether code is bundled.
func (b *Bundle) Has(code string) bool {
	_, ok := b.data[normalize(code)]
	return ok
}

// Set switches the active language. An empty code (or "auto") follows the
// system language.
func (b *Bundle) Set(code string) {
	code = normalize(code)
	if code == "" || code == "auto" {
		code = b.System()
	}
	values, ok := b.data[code]
	if !ok {
		values = b.fallback
		code = EnglishCode
	}
	b.current.Store(&state{code: code, data: values})
}

// Code returns the active language tag.
func (b *Bundle) Code() string {
	if s := b.current.Load(); s != nil {
		return s.code
	}
	return EnglishCode
}

// System returns the bundled language that best matches the system language,
// falling back to English.
func (b *Bundle) System() string {
	for _, candidate := range SystemLanguages() {
		if code := b.match(candidate); code != "" {
			return code
		}
	}
	return EnglishCode
}

// T returns the translation of key, falling back to English and finally to the
// key itself.
func (b *Bundle) T(key string) string {
	if s := b.current.Load(); s != nil {
		if value, ok := s.data[key]; ok && value != "" {
			return value
		}
	}
	if value, ok := b.fallback[key]; ok && value != "" {
		return value
	}
	return key
}

// Tf formats the translation of key with args.
func (b *Bundle) Tf(key string, args ...any) string {
	return fmt.Sprintf(b.T(key), args...)
}

// Snapshot returns the active map. Treat it as read-only; it is replaced, never
// modified, when the language changes.
func (b *Bundle) Snapshot() map[string]string {
	if s := b.current.Load(); s != nil {
		return s.data
	}
	return b.fallback
}

// MergeDir loads every *.json in dir on top of what is already loaded.
func (b *Bundle) MergeDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		values, err := decode(raw)
		if err != nil {
			return err
		}
		code := codeFromName(entry.Name())
		// Copy instead of merging in place: a Snapshot handed out earlier may
		// still be read from another goroutine through the atomic pointer. The
		// old map stays immutable and the new one replaces it only here.
		merged := make(map[string]string, len(b.data[code])+len(values))
		for k, v := range b.data[code] {
			merged[k] = v
		}
		for k, v := range values {
			merged[k] = v
		}
		b.data[code] = merged
	}
	b.fallback = b.data[EnglishCode]
	b.sortLanguages()
	b.Set(b.Code())
	return nil
}

// match maps a system language tag onto a bundled code, or "" if none fits.
func (b *Bundle) match(tag string) string {
	tag = normalize(tag)
	if tag == "" {
		return ""
	}
	if _, ok := b.data[tag]; ok {
		return tag
	}
	// zh-Hant / zh-Hans style script subtags.
	switch {
	case strings.HasPrefix(tag, "zh"):
		if strings.Contains(tag, "hant") || strings.Contains(tag, "tw") ||
			strings.Contains(tag, "hk") || strings.Contains(tag, "mo") {
			if _, ok := b.data["zh-TW"]; ok {
				return "zh-TW"
			}
		}
		if _, ok := b.data["zh-CN"]; ok {
			return "zh-CN"
		}
	}
	// Drop the region: pt-PT or pt should still find pt-BR.
	base := tag
	if i := strings.Index(base, "-"); i > 0 {
		base = base[:i]
	}
	for _, lang := range b.langs {
		if lang.Code == base {
			return lang.Code
		}
		if strings.EqualFold(strings.Split(lang.Code, "-")[0], base) {
			return lang.Code
		}
	}
	return ""
}

func (b *Bundle) sortLanguages() {
	codes := make([]string, 0, len(b.data))
	for code := range b.data {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	langs := make([]Language, 0, len(codes))
	for _, code := range codes {
		values := b.data[code]
		langs = append(langs, Language{
			Code:    code,
			Name:    values[nameKey],
			English: values[engKey],
		})
	}
	// English first, then the rest alphabetically by code.
	sort.SliceStable(langs, func(i, j int) bool {
		if langs[i].Code == EnglishCode {
			return true
		}
		if langs[j].Code == EnglishCode {
			return false
		}
		return langs[i].Code < langs[j].Code
	})
	for i := range langs {
		if langs[i].Name == "" {
			langs[i].Name = langs[i].Code
		}
		if langs[i].English == "" {
			langs[i].English = langs[i].Name
		}
	}
	b.langs = langs
}

// decode parses one translation file.
func decode(raw []byte) (map[string]string, error) {
	values := map[string]string{}
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	return values, nil
}

// codeFromName turns "zh-CN.json" into "zh-CN".
func codeFromName(name string) string {
	return strings.TrimSuffix(name, ".json")
}

// normalize turns "zh_CN.UTF-8" or "ZH-HANS" into "zh-CN"-shaped input.
func normalize(tag string) string {
	tag = strings.TrimSpace(tag)
	if i := strings.IndexByte(tag, '.'); i >= 0 {
		tag = tag[:i] // strip the UTF-8 encoding suffix
	}
	tag = strings.ReplaceAll(tag, "_", "-")
	if i := strings.IndexByte(tag, '@'); i >= 0 {
		tag = tag[:i]
	}
	// Keep the casing of the region subtag: zh-cn -> zh-CN.
	parts := strings.Split(tag, "-")
	for i := range parts {
		if i == 0 {
			parts[i] = strings.ToLower(parts[i])
			continue
		}
		if len(parts[i]) == 2 {
			parts[i] = strings.ToUpper(parts[i])
			continue
		}
		parts[i] = strings.ToLower(parts[i])
	}
	return strings.Join(parts, "-")
}

// overrideDir returns the directory to load overrides from, or "".
func overrideDir() string {
	if dir := os.Getenv(DirEnv); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), DirName)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return ""
}

// Package-level helpers operating on the default bundle.

// Languages lists every available translation.
func Languages() []Language { return Default().Languages() }

// SetLanguage switches the active language; "" follows the system.
func SetLanguage(code string) { Default().Set(code) }

// SetSystemLanguage switches to the language reported by the operating system.
func SetSystemLanguage() { Default().Set("") }

// ActiveCode returns the active language tag.
func ActiveCode() string { return Default().Code() }

// T translates key with the default bundle.
func T(key string) string { return Default().T(key) }

// Tf formats the translation of key with args.
func Tf(key string, args ...any) string { return Default().Tf(key, args...) }

// Snapshot returns the active translation map.
func Snapshot() map[string]string { return Default().Snapshot() }
