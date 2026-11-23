// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package web

import (
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// accessLimit is the number of recent requests kept in memory.
const accessLimit = 200

// deviceLimit caps the per-address table. A LAN with a handful of devices never
// comes close; the cap exists so a scan from a /24 cannot grow the map without
// bound.
const deviceLimit = 200

// recentLimit is how many distinct files the overview can name as "recently
// opened". Three are shown, the rest is slack for repeats.
const recentLimit = 12

// Device is one address that has read something, newest activity first when
// returned by Devices.
type Device struct {
	Addr  string
	First time.Time
	Last  time.Time
	Hits  int64
}

// AccessRecord is one request for a static asset.
type AccessRecord struct {
	Time       time.Time
	Method     string
	Path       string
	RemoteAddr string
	Status     int
	Duration   time.Duration
	UserAgent  string
}

// accessLog is a ring buffer that keeps only the most recent entries.
type accessLog struct {
	mu      sync.Mutex
	entries []AccessRecord
	next    int
	count   int
	total   int64
	started time.Time

	// Aggregates kept beside the ring buffer. The ring only holds the last 200
	// requests, so anything the overview shows has to be accumulated as
	// requests arrive — the buffer cannot be asked for a day of history.
	devices map[string]*Device
	recent  [recentLimit]string
	recentN int
	recentI int

	// Requests per clock hour, as a rolling 24-slot window. hourStart is the
	// Unix hour the window currently ends on.
	hourly    [24]int64
	hourStart int64
	hourPos   int
}

func newAccessLog() *accessLog {
	return &accessLog{
		entries: make([]AccessRecord, accessLimit),
		started: time.Now(),
		devices: make(map[string]*Device, 8),
	}
}

func (a *accessLog) add(record AccessRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.total++
	a.entries[a.next] = record
	a.next = (a.next + 1) % accessLimit
	if a.count < accessLimit {
		a.count++
	}

	a.trackDevice(record)
	a.rollHour(record.Time.Unix() / 3600)
	a.hourly[a.hourPos]++
	a.trackFile(record)
}

// trackDevice folds one request into the per-address table. Addresses carry a
// port, so the host is what gets counted.
func (a *accessLog) trackDevice(record AccessRecord) {
	host := record.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		return
	}

	if d, ok := a.devices[host]; ok {
		d.Last = record.Time
		d.Hits++
		return
	}
	if len(a.devices) >= deviceLimit {
		// Full: drop the least recently seen device rather than refusing the
		// new one, so an active reader is never invisible.
		var oldest, oldestKey string
		var oldestAt time.Time
		for key, d := range a.devices {
			if oldestKey == "" || d.Last.Before(oldestAt) {
				oldest, oldestKey, oldestAt = key, key, d.Last
			}
		}
		_ = oldest
		delete(a.devices, oldestKey)
	}
	a.devices[host] = &Device{Addr: host, First: record.Time, Last: record.Time, Hits: 1}
}

// rollHour advances the 24-slot window to the hour the new request belongs to.
// A gap larger than a day clears the whole window, which is what a machine that
// was asleep overnight should look like.
func (a *accessLog) rollHour(hour int64) {
	if a.hourStart == 0 {
		a.hourStart = hour
		return
	}
	diff := hour - a.hourStart
	if diff <= 0 {
		return
	}
	if diff >= 24 {
		a.hourly = [24]int64{}
		a.hourPos = 0
		a.hourStart = hour
		return
	}
	for i := int64(0); i < diff; i++ {
		a.hourPos = (a.hourPos + 1) % 24
		a.hourly[a.hourPos] = 0
	}
	a.hourStart = hour
}

// trackFile remembers which documents were opened. Directory listings and the
// application's own assets are not "opened files"; the point is to name the
// documents a colleague read.
func (a *accessLog) trackFile(record AccessRecord) {
	p := record.Path
	if strings.HasPrefix(p, "/__docwebhub/") || strings.HasSuffix(p, "/") {
		return
	}
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if p == "" || p == "/" {
		return
	}
	for i := 0; i < a.recentN; i++ {
		if a.recent[i] == p {
			return // already named; the list is about files, not visits
		}
	}
	a.recent[a.recentI] = p
	a.recentI = (a.recentI + 1) % recentLimit
	if a.recentN < recentLimit {
		a.recentN++
	}
}

// snapshot returns the entries oldest-first, the total request count and the
// time the server started.
func (a *accessLog) snapshot() ([]AccessRecord, int64, time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()

	entries := make([]AccessRecord, a.count)
	start := a.next - a.count
	if start < 0 {
		start += accessLimit
	}
	for i := range entries {
		entries[i] = a.entries[(start+i)%accessLimit]
	}
	return entries, a.total, a.started
}

// Devices returns the addresses seen, most recent activity first.
func (a *accessLog) devicesSnapshot() []Device {
	a.mu.Lock()
	defer a.mu.Unlock()

	out := make([]Device, 0, len(a.devices))
	for _, d := range a.devices {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	return out
}

// hourlySnapshot returns the 24 slots oldest-first, so index 23 is this hour.
func (a *accessLog) hourlySnapshot() []int64 {
	a.mu.Lock()
	defer a.mu.Unlock()

	out := make([]int64, 24)
	for i := 0; i < 24; i++ {
		out[i] = a.hourly[(a.hourPos+1+i)%24]
	}
	return out
}

// recentSnapshot returns the file paths named recently, newest first.
func (a *accessLog) recentSnapshot() []string {
	a.mu.Lock()
	defer a.mu.Unlock()

	out := make([]string, 0, a.recentN)
	for i := 1; i <= a.recentN; i++ {
		p := a.recent[(a.recentI-i+recentLimit)%recentLimit]
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// statusRecorder captures the response status while preserving the original
// ResponseWriter behaviour.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// ReadFrom lets http.ServeContent and io.Copy hand the body to the underlying
// writer directly, so a sendfile-capable ResponseWriter stays on the fast path.
func (r *statusRecorder) ReadFrom(src io.Reader) (int64, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if readerFrom, ok := r.ResponseWriter.(io.ReaderFrom); ok {
		return readerFrom.ReadFrom(src)
	}
	return io.Copy(r.ResponseWriter, src)
}

// Unwrap exposes the original writer to http.ResponseController, so flushing
// and deadline control keep working through the access-log wrapper.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// AccessSnapshot returns the recent access records, the total request count and
// the time the server started.
func (s *Server) AccessSnapshot() ([]AccessRecord, int64, time.Time) {
	return s.access.snapshot()
}

// Devices returns the addresses that have read something, newest first.
func (s *Server) Devices() []Device { return s.access.devicesSnapshot() }

// HourlyRequests returns 24 request counts, oldest first, this hour last.
func (s *Server) HourlyRequests() []int64 { return s.access.hourlySnapshot() }

// RecentFiles returns the documents opened recently, newest first.
func (s *Server) RecentFiles() []string { return s.access.recentSnapshot() }

// Started returns when the server process began serving.
func (s *Server) Started() time.Time {
	_, _, started := s.access.snapshot()
	return started
}

// TotalRequests returns how many requests have been served since start.
func (s *Server) TotalRequests() int64 {
	_, total, _ := s.access.snapshot()
	return total
}
