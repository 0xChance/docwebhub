// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package web

import (
	"io"
	"net/http"
	"sync"
	"time"
)

// accessLimit is the number of recent requests kept in memory.
const accessLimit = 200

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
}

func newAccessLog() *accessLog {
	return &accessLog{
		entries: make([]AccessRecord, accessLimit),
		started: time.Now(),
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
