// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

// Command update-sign manages the Ed25519 key pair used to sign the release
// manifest, and signs it.
//
//	go run ./tools/update-sign gen     <dir>        write <dir>/update.key and update.pub
//	go run ./tools/update-sign sign    <key> <file> write <file>.sig
//	go run ./tools/update-sign refresh <key> <file> recompute every script hash, then sign
//
// refresh is what to run after editing a module: a script whose hash no longer
// matches the manifest is silently dropped by every client, so the two steps
// are one command to make forgetting impossible.
//
// The private key must never be committed. Keep it offline (a password manager
// or a hardware token) and sign the manifest by hand: a key sitting in CI is a
// key any compromised CI run can use to push a fake security advisory.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/0xChance/docwebhub/internal/update"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	switch os.Args[1] {
	case "gen":
		if len(os.Args) != 3 {
			usage()
		}
		gen(os.Args[2])
	case "sign":
		if len(os.Args) != 4 {
			usage()
		}
		sign(os.Args[2], os.Args[3])
	case "refresh":
		if len(os.Args) != 4 {
			usage()
		}
		refresh(os.Args[2], os.Args[3])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage:\n  update-sign gen <dir>\n  update-sign sign <private-key> <file>\n  update-sign refresh <private-key> <file>\n")
	os.Exit(2)
}

func gen(dir string) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fatal("generate key: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fatal("create %s: %v", dir, err)
	}

	keyPath := filepath.Join(dir, "update.key")
	pubPath := filepath.Join(dir, "update.pub")

	// The private key is base64 so it survives copy-paste between machines; it
	// is the raw 64-byte seed+public form ed25519 expects.
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(priv)+"\n"), 0o600); err != nil {
		fatal("write %s: %v", keyPath, err)
	}
	if err := os.WriteFile(pubPath, []byte(base64.StdEncoding.EncodeToString(pub)+"\n"), 0o644); err != nil {
		fatal("write %s: %v", pubPath, err)
	}

	fmt.Printf("wrote %s (keep it offline, never commit)\n", keyPath)
	fmt.Printf("wrote %s (embed this in internal/update/keys/)\n", pubPath)
}

func sign(keyPath, filePath string) {
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		fatal("read key: %v", err)
	}
	priv, err := base64.StdEncoding.DecodeString(trimSpace(string(raw)))
	if err != nil || len(priv) != ed25519.PrivateKeySize {
		fatal("key is not a base64 Ed25519 private key")
	}

	payload, err := os.ReadFile(filePath)
	if err != nil {
		fatal("read %s: %v", filePath, err)
	}

	sig := ed25519.Sign(ed25519.PrivateKey(priv), payload)
	out := filePath + ".sig"
	if err := os.WriteFile(out, []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		fatal("write %s: %v", out, err)
	}
	fmt.Printf("signed %s -> %s\n", filePath, out)
}

// refresh rewrites the sha256 of every script the manifest names, then signs
// it. The hashes are what bind a script to the signed manifest; recomputing
// them by hand is the step people forget, and a stale hash makes a module
// disappear from every client without any error on the page.
func refresh(keyPath, configPath string) {
	payload, err := os.ReadFile(configPath)
	if err != nil {
		fatal("read %s: %v", configPath, err)
	}

	// Decoding into the struct rather than a map keeps the field order the
	// manifest is written in. A map would re-sort the keys alphabetically, and
	// every refresh would then produce a diff of the whole file.
	var manifest update.Release
	if err := json.Unmarshal(payload, &manifest); err != nil {
		fatal("%s is not valid JSON: %v", configPath, err)
	}

	dir := filepath.Dir(configPath)

	if manifest.Loader.Src != "" {
		sum, err := hashFile(filepath.Join(dir, filepath.FromSlash(manifest.Loader.Src)))
		if err != nil {
			fatal("loader: %v", err)
		}
		manifest.Loader.SHA256 = sum
		fmt.Printf("loader  %-32s %s\n", manifest.Loader.Src, sum[:12]+"…")
	}

	for i := range manifest.Modules {
		m := &manifest.Modules[i]
		sum, err := hashFile(filepath.Join(dir, filepath.FromSlash(m.Src)))
		if err != nil {
			fatal("module %s: %v", m.ID, err)
		}
		m.SHA256 = sum
		fmt.Printf("module  %-32s %s\n", m.Src, sum[:12]+"…")
	}

	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fatal("encode: %v", err)
	}
	if err := os.WriteFile(configPath, append(encoded, '\n'), 0o644); err != nil {
		fatal("write %s: %v", configPath, err)
	}
	fmt.Printf("rewrote %s\n", configPath)

	sign(keyPath, configPath)
}

// hashFile returns the hex SHA-256 of a file, which is the form the manifest
// stores and the app compares against.
func hashFile(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "update-sign: "+format+"\n", args...)
	os.Exit(1)
}
