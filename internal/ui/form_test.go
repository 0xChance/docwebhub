// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/0xChance/docwebhub/internal/i18n"
)

func newTestForm(t *testing.T, mutate func(*Options)) *formController {
	t.Helper()
	i18n.LoadDefault()
	i18n.SetLanguage("en")

	opts := Options{
		URL:          "http://127.0.0.1:9101",
		Port:         9101,
		Bind:         "127.0.0.1",
		BindOptions:  []BindOption{{Label: "Localhost only (127.0.0.1)", Value: "127.0.0.1"}},
		Root:         "/tmp/site",
		AuthUsername: "",
		Languages: []LangOption{
			{Code: "en", Name: "English"},
			{Code: "zh-CN", Name: "Chinese (Simplified)"},
		},
	}
	if mutate != nil {
		mutate(&opts)
	}
	return newFormController(opts)
}

func formFieldByID(t *testing.T, c *formController, id string) formField {
	t.Helper()
	field, ok := c.field(id)
	if !ok {
		t.Fatalf("field %q not found", id)
	}
	return field
}

func TestFormShape(t *testing.T) {
	c := newTestForm(t, nil)

	// header, stats, badges, devices, hourly, recent, url, root, port, bind,
	// auth, auth-status, hint, footer(language)
	if got := len(c.form.rows); got != 14 {
		t.Fatalf("rows = %d, want 14", got)
	}
	wantFieldCounts := []int{3, 2, 3, 1, 1, 1, 2, 2, 2, 2, 3, 1, 1, 1}
	for i, want := range wantFieldCounts {
		if got := len(c.form.rows[i].fields); got != want {
			t.Errorf("row %d (%s) has %d fields, want %d", i, c.form.rows[i].id, got, want)
		}
	}
	// Every row belongs to a tab, and the four tabs are all present.
	seen := map[string]bool{}
	for _, row := range c.form.rows {
		if row.group == "" {
			t.Errorf("row %q has no group", row.id)
		}
		seen[row.group] = true
	}
	for _, want := range []string{groupOverview, groupSettings, groupSecurity, groupLanguage, groupChrome} {
		if !seen[want] {
			t.Errorf("no row in group %q", want)
		}
	}
	if c.form.rows[7].kind != "setting" || c.form.rows[7].label != "Folder" {
		t.Fatalf("root row = %+v", c.form.rows[7])
	}
	if field := formFieldByID(t, c, "url"); field.kind != kindLink || field.value != "http://127.0.0.1:9101" {
		t.Fatalf("url field = %+v", field)
	}
	if field := formFieldByID(t, c, "port"); field.value != "9101" || field.width != 90 {
		t.Fatalf("port field = %+v", field)
	}
	if field := formFieldByID(t, c, "language"); len(field.options) != 3 || field.options[0].label != "System default" {
		t.Fatalf("language field = %+v", field)
	}
	if field := formFieldByID(t, c, "bind"); len(field.options) != 1 || field.options[0].value != "127.0.0.1" {
		t.Fatalf("bind field = %+v", field)
	}
}

func TestPortAction(t *testing.T) {
	called := 0
	c := newTestForm(t, func(opts *Options) {
		opts.SetPort = func(port int) (string, error) {
			called++
			if port != 9200 {
				t.Fatalf("SetPort got %d, want 9200", port)
			}
			return "http://127.0.0.1:9200", nil
		}
	})

	if msg := c.action(actionApplyPort, "not-a-port", ""); msg == "" {
		t.Fatal("invalid port should return an error")
	}
	if c.errorTitle != i18n.T("alert.port.invalid.title") {
		t.Fatalf("error title = %q", c.errorTitle)
	}
	if called != 0 {
		t.Fatalf("SetPort called for invalid input")
	}

	if msg := c.action(actionApplyPort, "9200", ""); msg != "" {
		t.Fatalf("valid port returned error %q", msg)
	}
	if called != 1 {
		t.Fatalf("SetPort calls = %d, want 1", called)
	}
	if c.port != 9200 || c.url != "http://127.0.0.1:9200" {
		t.Fatalf("state after port change: port=%d url=%q", c.port, c.url)
	}
	if got := formFieldByID(t, c, "port").value; got != "9200" {
		t.Fatalf("port field = %q", got)
	}
	if !containsAll(c.changed, "url", "port") {
		t.Fatalf("changed = %v", c.changed)
	}
}

func TestBindFailureRestoresSelection(t *testing.T) {
	c := newTestForm(t, func(opts *Options) {
		opts.SetBind = func(bind string) (string, error) {
			return "", errors.New("address gone")
		}
	})

	if msg := c.action(actionSetBind, "0.0.0.0", ""); msg != "address gone" {
		t.Fatalf("message = %q", msg)
	}
	if c.bind != "127.0.0.1" {
		t.Fatalf("bind changed to %q", c.bind)
	}
	if !containsAll(c.changed, "bind") {
		t.Fatalf("changed = %v", c.changed)
	}
	if got := formFieldByID(t, c, "bind").value; got != "127.0.0.1" {
		t.Fatalf("bind field = %q", got)
	}
}

func TestChooseRootAction(t *testing.T) {
	c := newTestForm(t, func(opts *Options) {
		opts.Root = "/missing"
		opts.RootMissing = true
		opts.ChooseRoot = func(dir string) error {
			if dir != "/tmp/chosen" {
				t.Fatalf("ChooseRoot got %q", dir)
			}
			return nil
		}
	})

	if msg := c.action(actionChooseRoot, "/tmp/chosen", ""); msg != "" {
		t.Fatalf("unexpected error: %q", msg)
	}
	if c.root != "/tmp/chosen" || c.rootMissing {
		t.Fatalf("root state = %q missing=%v", c.root, c.rootMissing)
	}
	if got := formFieldByID(t, c, "root-value").value; got != "/tmp/chosen" {
		t.Fatalf("root value = %q", got)
	}
	if got := formFieldByID(t, c, "status").style; got != styleSuccess {
		t.Fatalf("status style = %q", got)
	}
	if !containsAll(c.changed, "root-value", "status", "intro") {
		t.Fatalf("changed = %v", c.changed)
	}
}

func TestBindAction(t *testing.T) {
	c := newTestForm(t, func(opts *Options) {
		opts.SetBind = func(bind string) (string, error) {
			if bind != "0.0.0.0" {
				t.Fatalf("SetBind got %q", bind)
			}
			return "http://192.168.1.9:9101", nil
		}
	})

	if msg := c.action(actionSetBind, "0.0.0.0", ""); msg != "" {
		t.Fatalf("unexpected error: %q", msg)
	}
	if c.bind != "0.0.0.0" || c.url != "http://192.168.1.9:9101" {
		t.Fatalf("state = bind:%q url:%q", c.bind, c.url)
	}
	if got := formFieldByID(t, c, "bind").value; got != "0.0.0.0" {
		t.Fatalf("bind field = %q", got)
	}
	if !containsAll(c.changed, "url", "bind") {
		t.Fatalf("changed = %v", c.changed)
	}
}

func TestAuthActionClearsPassword(t *testing.T) {
	var gotUser, gotPass string
	c := newTestForm(t, func(opts *Options) {
		opts.SetAuth = func(username, password string) error {
			gotUser, gotPass = username, password
			return nil
		}
	})

	if msg := c.action(actionApplyAuth, "  alice  ", "secret"); msg != "" {
		t.Fatalf("unexpected error: %q", msg)
	}
	if gotUser != "alice" || gotPass != "secret" {
		t.Fatalf("SetAuth got user=%q pass=%q", gotUser, gotPass)
	}
	if !c.authEnabled || c.authUser != "alice" {
		t.Fatalf("auth state = enabled:%v user:%q", c.authEnabled, c.authUser)
	}
	if got := formFieldByID(t, c, "auth-pass").value; got != "" {
		t.Fatalf("password field retained %q", got)
	}
	if got := formFieldByID(t, c, "auth-status").value; !strings.Contains(got, "alice") {
		t.Fatalf("auth status = %q", got)
	}
}

func TestLanguageActionReloadsLabels(t *testing.T) {
	defer i18n.SetLanguage("en")

	c := newTestForm(t, func(opts *Options) {
		opts.SetLanguage = func(code string) []BindOption {
			i18n.SetLanguage(code)
			// The label comes from the table so the assertion below tests the
			// language switch rather than a copy of one translation.
			return []BindOption{{Label: i18n.T("ui.bind.local"), Value: "127.0.0.1"}}
		}
	})

	if msg := c.action(actionSetLanguage, "zh-CN", ""); msg != "" {
		t.Fatalf("unexpected error: %q", msg)
	}
	if !c.labelsChanged {
		t.Fatal("labelsChanged = false")
	}
	if c.language != "zh-CN" {
		t.Fatalf("language = %q", c.language)
	}
	var portLabel string
	for _, row := range c.form.rows {
		if row.id == "port" {
			portLabel = row.label
		}
	}
	if portLabel != i18n.T("ui.port.label") {
		t.Fatalf("port row label = %q", portLabel)
	}
	if got := formFieldByID(t, c, "bind").options[0].label; got != i18n.T("ui.bind.local") {
		t.Fatalf("bind option label = %q", got)
	}
	if !containsAll(c.changed, "language", "bind") {
		t.Fatalf("changed = %v", c.changed)
	}
}

func TestAccessInfoAction(t *testing.T) {
	c := newTestForm(t, func(opts *Options) {
		opts.AccessInfo = func() string { return "GET / 200" }
	})
	if msg := c.action(actionAccessInfo, "", ""); msg != "" {
		t.Fatalf("unexpected error: %q", msg)
	}
	if c.actionText != "GET / 200" {
		t.Fatalf("actionText = %q", c.actionText)
	}
}

func TestRootMissingState(t *testing.T) {
	c := newTestForm(t, func(opts *Options) {
		opts.RootMissing = true
		opts.Root = "/missing"
	})
	if got := formFieldByID(t, c, "status").style; got != styleWarning {
		t.Fatalf("status style = %q", got)
	}
	if got := formFieldByID(t, c, "root-value").value; got != "Not selected" {
		t.Fatalf("root value = %q", got)
	}
	if got := formFieldByID(t, c, "intro").value; !strings.Contains(got, "No site folder") {
		t.Fatalf("intro = %q", got)
	}
}

func containsAll(values []string, want ...string) bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	for _, value := range want {
		if !set[value] {
			return false
		}
	}
	return true
}
