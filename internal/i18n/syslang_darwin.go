// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

//go:build darwin && cgo

package i18n

/*
#cgo LDFLAGS: -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
*/
import "C"

// SystemLanguages returns the user's preferred languages, most preferred first.
//
// A double-clicked .app has no useful LANG in its environment, so the list has
// to come from the system preference rather than from environment variables.
func SystemLanguages() []string {
	array := C.CFLocaleCopyPreferredLanguages()
	if array == 0 {
		return nil
	}
	defer C.CFRelease(C.CFTypeRef(array))

	count := C.CFArrayGetCount(array)
	out := make([]string, 0, count)
	for i := C.CFIndex(0); i < count; i++ {
		item := C.CFArrayGetValueAtIndex(array, i)
		if item == nil {
			continue
		}
		if text := cfString(C.CFStringRef(item)); text != "" {
			out = append(out, text)
		}
	}
	return out
}

// cfString copies a CFString into a Go string.
func cfString(value C.CFStringRef) string {
	if value == 0 {
		return ""
	}
	// Fast path: ASCII and most UTF-8 strings are stored in a form CoreFoundation
	// can hand back directly.
	if ptr := C.CFStringGetCStringPtr(value, C.kCFStringEncodingUTF8); ptr != nil {
		return C.GoString(ptr)
	}

	buffer := make([]C.char, 256)
	if C.CFStringGetCString(value, &buffer[0], C.CFIndex(len(buffer)), C.kCFStringEncodingUTF8) != 0 {
		return C.GoString(&buffer[0])
	}
	return ""
}
