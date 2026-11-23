// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package web

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDevicesAggregateByHost(t *testing.T) {
	log := newAccessLog()
	base := time.Now()
	for i, rec := range []struct{ addr string }{
		{"192.168.1.23:5011"},
		{"192.168.1.23:5012"},
		{"192.168.1.8:4000"},
	} {
		log.add(AccessRecord{Time: base.Add(time.Duration(i) * time.Second), Path: "/a.txt", RemoteAddr: rec.addr})
	}

	devices := log.devicesSnapshot()
	if len(devices) != 2 {
		t.Fatalf("devices = %d, want 2 (ports must not split an address)", len(devices))
	}
	if devices[0].Addr != "192.168.1.8" {
		t.Fatalf("first device = %q, want the most recent one", devices[0].Addr)
	}
	if got := devices[1].Hits; got != 2 {
		t.Fatalf("hits for 192.168.1.23 = %d, want 2", got)
	}
}

func TestHourlyRequestsRoll(t *testing.T) {
	log := newAccessLog()
	base := time.Unix(1_700_000_000, 0).Truncate(time.Hour)

	log.add(AccessRecord{Time: base, Path: "/a", RemoteAddr: "10.0.0.1:1"})
	log.add(AccessRecord{Time: base, Path: "/b", RemoteAddr: "10.0.0.1:1"})
	log.add(AccessRecord{Time: base.Add(2 * time.Hour), Path: "/c", RemoteAddr: "10.0.0.1:1"})

	hourly := log.hourlySnapshot()
	if len(hourly) != 24 {
		t.Fatalf("len = %d, want 24", len(hourly))
	}
	if hourly[23] != 1 {
		t.Fatalf("this hour = %d, want 1", hourly[23])
	}
	if hourly[21] != 2 {
		t.Fatalf("two hours back = %d, want 2", hourly[21])
	}
	if hourly[22] != 0 {
		t.Fatalf("the hour in between = %d, want 0", hourly[22])
	}
}

func TestHourlyRequestsResetAfterGap(t *testing.T) {
	log := newAccessLog()
	base := time.Unix(1_700_000_000, 0).Truncate(time.Hour)
	log.add(AccessRecord{Time: base, Path: "/a", RemoteAddr: "10.0.0.1:1"})
	log.add(AccessRecord{Time: base.Add(30 * time.Hour), Path: "/b", RemoteAddr: "10.0.0.1:1"})

	hourly := log.hourlySnapshot()
	var sum int64
	for _, v := range hourly {
		sum += v
	}
	if sum != 1 {
		t.Fatalf("total = %d, want 1: a gap longer than a day must clear the window", sum)
	}
}

func TestRecentFilesIgnoreListingsAndAssets(t *testing.T) {
	log := newAccessLog()
	now := time.Now()
	for _, p := range []string{
		"/docs/",
		"/__docwebhub/preview/preview.js",
		"/",
		"/week.docx",
		"/quote.xlsx",
		"/week.docx", // a repeat must not take two slots
	} {
		log.add(AccessRecord{Time: now, Path: p, RemoteAddr: "10.0.0.1:1"})
	}

	recent := log.recentSnapshot()
	if len(recent) != 2 {
		t.Fatalf("recent = %v, want the two documents", recent)
	}
	if recent[0] != "/quote.xlsx" {
		t.Fatalf("first = %q, want the newest", recent[0])
	}
}

func TestCountTree(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt")
	write("sub/b.txt")
	write("sub/deeper/c.txt")

	srv, err := New(root, freePort(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })

	files, dirs, truncated := srv.CountTree()
	if files != 3 || dirs != 2 || truncated {
		t.Fatalf("CountTree = %d files, %d dirs, truncated=%v; want 3, 2, false", files, dirs, truncated)
	}
}
