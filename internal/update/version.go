// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package update

import (
	"strconv"
	"strings"
)

// version is a parsed semantic version. A "dev" build never parses, which is
// what keeps a development checkout from showing update banners.
type version struct {
	nums [3]int
	pre  string
}

func parseVersion(s string) (version, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return version{}, false
	}

	// Split off the pre-release suffix before parsing numbers.
	pre := ""
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre = s[i+1:]
		s = s[:i]
	}
	// Build metadata never participates in comparison.
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}

	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	var v version
	for i, p := range parts {
		// Reject "01" and other forms that strconv would accept but semver forbids.
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return version{}, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return version{}, false
		}
		v.nums[i] = n
	}
	v.pre = pre
	return v, true
}

// compareVersions returns -1, 0 or 1, and false when either side cannot be
// parsed. Callers treat an unparsable pair as "say nothing": a banner about a
// version nobody can compare is worse than no banner.
func compareVersions(a, b string) (int, bool) {
	av, ok := parseVersion(a)
	if !ok {
		return 0, false
	}
	bv, ok := parseVersion(b)
	if !ok {
		return 0, false
	}
	for i := range 3 {
		switch {
		case av.nums[i] < bv.nums[i]:
			return -1, true
		case av.nums[i] > bv.nums[i]:
			return 1, true
		}
	}
	switch {
	case av.pre == bv.pre:
		return 0, true
	case av.pre == "":
		return 1, true // a release outranks a pre-release of the same number
	case bv.pre == "":
		return -1, true
	case av.pre < bv.pre:
		return -1, true
	default:
		return 1, true
	}
}

// affectedBy reports whether v falls inside an advisory's range expression.
//
// Supported syntax is deliberately tiny: a comma-separated list of comparisons
// against a version, all of which must hold, for example "<0.2.1" or
// ">=0.1.0,<0.2.1".
//
// It returns false for anything it does not understand. Showing an urgent
// security banner to the wrong users trains them to ignore the next one, so a
// parse failure must stay silent rather than guess.
func affectedBy(v, expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}

	for _, term := range strings.Split(expr, ",") {
		term = strings.TrimSpace(term)
		if term == "" {
			return false
		}

		op := ""
		for _, candidate := range []string{"<=", ">=", "<", ">", "="} {
			if strings.HasPrefix(term, candidate) {
				op = candidate
				break
			}
		}

		// A bare version means "exactly this one". The comparison operator and
		// the prefix actually consumed are tracked separately: "0.2.0" has no
		// operator to strip, so slicing by len(op) would eat its first digit.
		consumed := op
		if op == "" {
			op = "="
		}

		want := strings.TrimSpace(term[len(consumed):])
		cmp, ok := compareVersions(v, want)
		if !ok {
			return false
		}
		switch op {
		case "<":
			if cmp >= 0 {
				return false
			}
		case "<=":
			if cmp > 0 {
				return false
			}
		case ">":
			if cmp <= 0 {
				return false
			}
		case ">=":
			if cmp < 0 {
				return false
			}
		case "=":
			if cmp != 0 {
				return false
			}
		}
	}
	return true
}
