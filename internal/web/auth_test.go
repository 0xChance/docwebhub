// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package web

import (
	"strings"
	"testing"
)

func TestHashPasswordIsSalted(t *testing.T) {
	first, err := HashPassword("secret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	second, err := HashPassword("secret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if first == second {
		t.Fatal("salting the same password twice should produce different digests")
	}
	if strings.Contains(first, "secret") {
		t.Fatalf("the digest must not contain the plaintext: %s", first)
	}
	parts := strings.Split(first, "$")
	if len(parts) != 3 || parts[0] != "sha256" {
		t.Fatalf("unexpected digest format: %s", first)
	}
}

func TestHashPasswordRejectsEmpty(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Fatal("an empty password should return an error")
	}
}

func TestAuthVerify(t *testing.T) {
	hash, err := HashPassword("p@ss word")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	auth := &Auth{Username: "admin", Hash: hash}

	cases := []struct {
		name     string
		username string
		password string
		want     bool
	}{
		{"valid credentials", "admin", "p@ss word", true},
		{"wrong password", "admin", "p@ss", false},
		{"wrong username", "root", "p@ss word", false},
		{"username is case-sensitive", "Admin", "p@ss word", false},
		{"empty password", "admin", "", false},
	}

	for _, tc := range cases {
		if got := auth.Verify(tc.username, tc.password); got != tc.want {
			t.Errorf("%s: Verify(%q, %q) = %v, want %v", tc.name, tc.username, tc.password, got, tc.want)
		}
	}
}

func TestAuthVerifyWithMalformedHash(t *testing.T) {
	cases := []*Auth{
		{Username: "admin", Hash: "plain-text"},
		{Username: "admin", Hash: "md5$abcd$ef01"},
		{Username: "admin", Hash: "sha256$zz$zz"},
	}

	for _, auth := range cases {
		if auth.Verify("admin", "anything") {
			t.Errorf("a malformed digest %q should be rejected", auth.Hash)
		}
	}
}

func TestAuthDisabledAlwaysPasses(t *testing.T) {
	// nil counts as disabled, so Verify passes everything through.
	var nilAuth *Auth
	if nilAuth.Enabled() {
		t.Fatal("nil must not count as enabled")
	}
	if !nilAuth.Verify("any", "thing") {
		t.Fatal("verification should pass through when no password is set")
	}

	empty := &Auth{}
	if empty.Enabled() {
		t.Fatal("an empty username or digest must not count as enabled")
	}
	if !empty.Verify("", "") {
		t.Fatal("verification should pass through when no password is set")
	}
}
