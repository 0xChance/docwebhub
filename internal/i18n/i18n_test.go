// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package i18n

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBundledLocales guards the translation files themselves: every language has
// exactly the keys English has, keeps the format placeholders, and can actually
// be selected.
func TestBundledLocales(t *testing.T) {
	bundle, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	english, ok := bundle.data[EnglishCode]
	if !ok {
		t.Fatalf("English is not bundled")
	}

	for _, lang := range bundle.Languages() {
		values := bundle.data[lang.Code]

		for key := range english {
			if _, ok := values[key]; !ok {
				t.Errorf("%s is missing key %q", lang.Code, key)
			}
		}
		for key := range values {
			if _, ok := english[key]; !ok {
				t.Errorf("%s has unknown key %q", lang.Code, key)
			}
		}

		for key, want := range english {
			got, ok := values[key]
			if !ok {
				continue
			}
			for _, verb := range []string{"%s", "%d"} {
				if strings.Count(got, verb) != strings.Count(want, verb) {
					t.Errorf("%s: %q has %d %q, English has %d",
						lang.Code, key, strings.Count(got, verb), verb, strings.Count(want, verb))
				}
			}
		}

		if lang.Name == "" || lang.English == "" {
			t.Errorf("%s is missing language.name or language.english", lang.Code)
		}

		bundle.Set(lang.Code)
		if bundle.Code() != lang.Code {
			t.Errorf("Set(%q) resolved to %q", lang.Code, bundle.Code())
		}
	}
}

func TestFallsBackToEnglish(t *testing.T) {
	bundle, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	bundle.data["test"] = map[string]string{"ui.port.label": "PORT"}
	bundle.sortLanguages()
	bundle.Set("test")

	if got := bundle.T("ui.port.label"); got != "PORT" {
		t.Fatalf("translated key = %q", got)
	}
	if got := bundle.T("ui.bind.label"); got != "Bind" {
		t.Fatalf("missing key should fall back to English, got %q", got)
	}
	if got := bundle.Tf("ui.auth.on", "admin"); got != "On: admin" {
		t.Fatalf("Tf = %q", got)
	}

	delete(bundle.data, "test")
	bundle.sortLanguages()
	bundle.Set(EnglishCode)
}

func TestMatchSystemTag(t *testing.T) {
	bundle, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cases := map[string]string{}
	for _, lang := range bundle.Languages() {
		cases[lang.Code] = lang.Code
	}
	// Realistic spellings seen in LANG / on Windows / in NSLocale output.
	cases["zh_CN.UTF-8"] = "zh-CN"
	cases["zh_TW"] = "zh-TW"
	cases["zh-Hant-TW"] = "zh-TW"
	cases["zh-Hans"] = "zh-CN"
	if bundle.Has("pt-BR") {
		cases["pt"] = "pt-BR"
	} else {
		cases["pt"] = "pt"
	}
	cases["de_DE.UTF-8"] = "de"
	cases[""] = ""

	for tag, want := range cases {
		if got := bundle.match(tag); got != want {
			t.Errorf("match(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"zh_CN.UTF-8": "zh-CN",
		"ZH-HANS":     "zh-hans",
		"pt_BR":       "pt-BR",
		"ja_JP":       "ja-JP",
		"en_US.UTF-8": "en-US",
		"ca@valencia": "ca",
	}
	for in, want := range cases {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// MergeDir replaces a language map instead of mutating it, so a Snapshot that
// another goroutine already holds keeps its old contents.
func TestMergeDirDoesNotMutateSnapshots(t *testing.T) {
	bundle, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	bundle.Set(EnglishCode)
	before := bundle.Snapshot()
	if _, ok := before["test.merged"]; ok {
		t.Fatal("test key already exists")
	}

	dir := t.TempDir()
	override := `{"test.merged": "from the override"}`
	if err := os.WriteFile(filepath.Join(dir, "en.json"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := bundle.MergeDir(dir); err != nil {
		t.Fatalf("MergeDir: %v", err)
	}

	if _, ok := before["test.merged"]; ok {
		t.Fatal("MergeDir modified a snapshot that was already handed out")
	}
	if got := bundle.T("test.merged"); got != "from the override" {
		t.Fatalf("T(test.merged) = %q, want the override", got)
	}
}
