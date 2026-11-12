// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

//go:build windows

package i18n

import "syscall"

// SystemLanguages returns the user's preferred languages, most preferred first.
//
// Windows keeps the UI language as a LANGID, and neither it nor the locale name
// shows up in the process environment, so ask the kernel directly.
func SystemLanguages() []string {
	id := uiLanguage()
	if id == 0 {
		return nil
	}
	return []string{langIDToTag(id)}
}

func uiLanguage() uint16 {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	if id := callLangID(kernel32.NewProc("GetUserDefaultUILanguage")); id != 0 {
		return id
	}
	return callLangID(kernel32.NewProc("GetSystemDefaultUILanguage"))
}

func callLangID(proc *syscall.LazyProc) uint16 {
	id, _, _ := proc.Call()
	if id == 0 || id > 0xFFFF {
		return 0
	}
	return uint16(id)
}

// langIDToTag maps a Windows LANGID onto a language tag. Only the languages
// docWebHub ships are distinguished; anything else falls back to English.
func langIDToTag(id uint16) string {
	primary := id & 0x3FF
	sub := id >> 10

	switch primary {
	case 0x09: // English
		return "en"
	case 0x04: // Chinese
		// SUBLANG_CHINESE_TRADITIONAL and the Hong Kong / Macao variants use
		// traditional script; everything else is simplified.
		if sub == 0x01 || sub == 0x04 || sub == 0x05 {
			return "zh-TW"
		}
		return "zh-CN"
	case 0x11:
		return "ja"
	case 0x12:
		return "ko"
	case 0x0A:
		return "es"
	case 0x0C:
		return "fr"
	case 0x07:
		return "de"
	case 0x16:
		// SUBLANG_PORTUGUESE_BRAZILIAN; plain Portuguese has no region.
		if sub == 0x01 {
			return "pt-BR"
		}
		return "pt"
	case 0x19:
		return "ru"
	case 0x10:
		return "it"
	}
	return "en"
}
