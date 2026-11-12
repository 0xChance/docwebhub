// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package update

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"0.1.0", "0.2.0", -1, true},
		{"0.2.0", "0.1.0", 1, true},
		{"0.2.0", "0.2.0", 0, true},
		{"v0.2.0", "0.2.0", 0, true},
		{"0.2.0", "0.2.0-rc1", 1, true},
		{"0.2.0-rc1", "0.2.0", -1, true},
		{"0.2.0-rc1", "0.2.0-rc2", -1, true},
		{"1.0.0", "0.99.99", 1, true},
		// Unparsable input must never yield an ordering.
		{"dev", "0.2.0", 0, false},
		{"0.2.0", "dev", 0, false},
		{"0.2", "0.2.0", 0, false},
		{"0.02.0", "0.2.0", 0, false},
		{"", "0.2.0", 0, false},
	}
	for _, c := range cases {
		got, ok := compareVersions(c.a, c.b)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("compareVersions(%q, %q) = %d, %v; want %d, %v", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

func TestAffectedBy(t *testing.T) {
	cases := []struct {
		version, expr string
		want          bool
	}{
		{"0.1.0", "<0.2.0", true},
		{"0.2.0", "<0.2.0", false},
		{"0.2.0", "<=0.2.0", true},
		{"0.3.0", "<0.2.0", false},
		{"0.1.5", ">=0.1.0,<0.2.0", true},
		{"0.2.5", ">=0.1.0,<0.2.0", false},
		{"0.2.0", "0.2.0", true},
		{"0.2.0", "=0.2.0", true},
		// Anything unparsable stays silent.
		{"dev", "<0.2.0", false},
		{"0.1.0", "", false},
		{"0.1.0", "garbage", false},
		{"0.1.0", "<", false},
		{"0.1.0", "<0.2.0,", false},
	}
	for _, c := range cases {
		if got := affectedBy(c.version, c.expr); got != c.want {
			t.Errorf("affectedBy(%q, %q) = %v; want %v", c.version, c.expr, got, c.want)
		}
	}
}

func TestPickLang(t *testing.T) {
	// The values only have to differ from each other; the point is which key
	// pickLang chooses, not what the text says.
	m := map[string]string{"en": "English", "zh-CN": "Simplified"}
	cases := map[string]string{
		"en":    "English",
		"zh-CN": "Simplified",
		"zh-TW": "Simplified", // falls back to the simplified text rather than English
		"ja":    "English",
		"":      "English",
	}
	for lang, want := range cases {
		if got := pickLang(m, lang); got != want {
			t.Errorf("pickLang(%q) = %q; want %q", lang, got, want)
		}
	}
	if got := pickLang(nil, "en"); got != "" {
		t.Errorf("pickLang(nil) = %q; want empty", got)
	}
}

func TestModuleURL(t *testing.T) {
	if got, want := ModuleURL("modules/version-notice.js"), "/__docwebhub/update/modules/version-notice.js"; got != want {
		t.Errorf("ModuleURL = %q; want %q", got, want)
	}
	// Paths outside the expected shape must not become URLs: the proxy
	// whitelist would reject them anyway, but the browser should never be told
	// to try.
	for _, bad := range []string{"", "modules/../x.js", "modules/a/b.js", "modules/UPPER.js", "v1.js"} {
		if got := ModuleURL(bad); got != "" {
			t.Errorf("ModuleURL(%q) = %q; want empty", bad, got)
		}
	}
}
