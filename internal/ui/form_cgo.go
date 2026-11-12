// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

//go:build (darwin || linux) && cgo

package ui

/*
#include <stdlib.h>
*/
import "C"

import (
	"github.com/0xChance/docwebhub/internal/i18n"
)

// This file is the small C ABI between the shared Go form model and the native
// AppKit/GTK renderers. Every getter returns a newly allocated C string; the
// caller owns it and must free() it. The platform files include form_ui.h,
// which declares these functions.

func formCString(value string) *C.char {
	return C.CString(value)
}

func goString(value *C.char) string {
	if value == nil {
		return ""
	}
	return C.GoString(value)
}

func currentForm() *formModel {
	if activeForm == nil {
		return nil
	}
	return &activeForm.form
}

//export docwebhub_form_title
func docwebhub_form_title() *C.char {
	if activeForm == nil {
		return formCString(formProductName)
	}
	return formCString(activeForm.form.title)
}

//export docwebhub_form_row_count
func docwebhub_form_row_count() C.int {
	form := currentForm()
	if form == nil {
		return 0
	}
	return C.int(len(form.rows))
}

//export docwebhub_form_row_id
func docwebhub_form_row_id(row C.int) *C.char {
	form := currentForm()
	index := int(row)
	if form == nil || index < 0 || index >= len(form.rows) {
		return formCString("")
	}
	return formCString(form.rows[index].id)
}

//export docwebhub_form_row_kind
func docwebhub_form_row_kind(row C.int) *C.char {
	form := currentForm()
	index := int(row)
	if form == nil || index < 0 || index >= len(form.rows) {
		return formCString("")
	}
	return formCString(form.rows[index].kind)
}

//export docwebhub_form_row_label
func docwebhub_form_row_label(row C.int) *C.char {
	form := currentForm()
	index := int(row)
	if form == nil || index < 0 || index >= len(form.rows) {
		return formCString("")
	}
	return formCString(form.rows[index].label)
}

//export docwebhub_form_field_count
func docwebhub_form_field_count(row C.int) C.int {
	form := currentForm()
	index := int(row)
	if form == nil || index < 0 || index >= len(form.rows) {
		return 0
	}
	return C.int(len(form.rows[index].fields))
}

func formFieldAt(row, column C.int) (formField, bool) {
	form := currentForm()
	if form == nil {
		return formField{}, false
	}
	rowIndex, columnIndex := int(row), int(column)
	if rowIndex < 0 || rowIndex >= len(form.rows) {
		return formField{}, false
	}
	fields := form.rows[rowIndex].fields
	if columnIndex < 0 || columnIndex >= len(fields) {
		return formField{}, false
	}
	return fields[columnIndex], true
}

//export docwebhub_form_field_id
func docwebhub_form_field_id(row, column C.int) *C.char {
	field, ok := formFieldAt(row, column)
	if !ok {
		return formCString("")
	}
	return formCString(field.id)
}

//export docwebhub_form_field_kind
func docwebhub_form_field_kind(row, column C.int) *C.char {
	field, ok := formFieldAt(row, column)
	if !ok {
		return formCString("")
	}
	return formCString(field.kind)
}

//export docwebhub_form_field_label
func docwebhub_form_field_label(row, column C.int) *C.char {
	field, ok := formFieldAt(row, column)
	if !ok {
		return formCString("")
	}
	return formCString(field.label)
}

//export docwebhub_form_field_value
func docwebhub_form_field_value(row, column C.int) *C.char {
	field, ok := formFieldAt(row, column)
	if !ok {
		return formCString("")
	}
	return formCString(field.value)
}

//export docwebhub_form_field_placeholder
func docwebhub_form_field_placeholder(row, column C.int) *C.char {
	field, ok := formFieldAt(row, column)
	if !ok {
		return formCString("")
	}
	return formCString(field.placeholder)
}

//export docwebhub_form_field_tooltip
func docwebhub_form_field_tooltip(row, column C.int) *C.char {
	field, ok := formFieldAt(row, column)
	if !ok {
		return formCString("")
	}
	return formCString(field.tooltip)
}

//export docwebhub_form_field_accessibility
func docwebhub_form_field_accessibility(row, column C.int) *C.char {
	field, ok := formFieldAt(row, column)
	if !ok {
		return formCString("")
	}
	return formCString(field.accessibility)
}

//export docwebhub_form_field_action
func docwebhub_form_field_action(row, column C.int) *C.char {
	field, ok := formFieldAt(row, column)
	if !ok {
		return formCString("")
	}
	return formCString(field.action)
}

//export docwebhub_form_field_style
func docwebhub_form_field_style(row, column C.int) *C.char {
	field, ok := formFieldAt(row, column)
	if !ok {
		return formCString("")
	}
	return formCString(field.style)
}

//export docwebhub_form_field_icon
func docwebhub_form_field_icon(row, column C.int) *C.char {
	field, ok := formFieldAt(row, column)
	if !ok {
		return formCString("")
	}
	return formCString(field.icon)
}

//export docwebhub_form_field_width
func docwebhub_form_field_width(row, column C.int) C.int {
	field, ok := formFieldAt(row, column)
	if !ok {
		return 0
	}
	return C.int(field.width)
}

//export docwebhub_form_field_option_count
func docwebhub_form_field_option_count(row, column C.int) C.int {
	field, ok := formFieldAt(row, column)
	if !ok {
		return 0
	}
	return C.int(len(field.options))
}

//export docwebhub_form_field_option_label
func docwebhub_form_field_option_label(row, column, option C.int) *C.char {
	field, ok := formFieldAt(row, column)
	if !ok {
		return formCString("")
	}
	index := int(option)
	if index < 0 || index >= len(field.options) {
		return formCString("")
	}
	return formCString(field.options[index].label)
}

//export docwebhub_form_field_option_value
func docwebhub_form_field_option_value(row, column, option C.int) *C.char {
	field, ok := formFieldAt(row, column)
	if !ok {
		return formCString("")
	}
	index := int(option)
	if index < 0 || index >= len(field.options) {
		return formCString("")
	}
	return formCString(field.options[index].value)
}

//export docwebhub_form_menu_count
func docwebhub_form_menu_count() C.int {
	form := currentForm()
	if form == nil {
		return 0
	}
	return C.int(len(form.menu))
}

//export docwebhub_form_menu_label
func docwebhub_form_menu_label(index C.int) *C.char {
	form := currentForm()
	i := int(index)
	if form == nil || i < 0 || i >= len(form.menu) {
		return formCString("")
	}
	return formCString(form.menu[i].label)
}

//export docwebhub_form_menu_action
func docwebhub_form_menu_action(index C.int) *C.char {
	form := currentForm()
	i := int(index)
	if form == nil || i < 0 || i >= len(form.menu) {
		return formCString("")
	}
	return formCString(form.menu[i].action)
}

//export docwebhub_form_menu_separator
func docwebhub_form_menu_separator(index C.int) C.int {
	form := currentForm()
	i := int(index)
	if form == nil || i < 0 || i >= len(form.menu) {
		return 0
	}
	if form.menu[i].separator {
		return 1
	}
	return 0
}

//export docwebhub_text
func docwebhub_text(key *C.char) *C.char {
	return formCString(i18n.T(goString(key)))
}

//export docwebhub_action
func docwebhub_action(action, value1, value2 *C.char) *C.char {
	if activeForm == nil {
		return nil
	}
	message := activeForm.action(goString(action), goString(value1), goString(value2))
	if message == "" {
		return nil
	}
	return formCString(message)
}

//export docwebhub_action_error_title
func docwebhub_action_error_title() *C.char {
	if activeForm == nil {
		return formCString("")
	}
	return formCString(activeForm.errorTitle)
}

//export docwebhub_action_text
func docwebhub_action_text() *C.char {
	if activeForm == nil {
		return formCString("")
	}
	return formCString(activeForm.actionText)
}

//export docwebhub_labels_changed
func docwebhub_labels_changed() C.int {
	if activeForm != nil && activeForm.labelsChanged {
		return 1
	}
	return 0
}

//export docwebhub_changed_count
func docwebhub_changed_count() C.int {
	if activeForm == nil {
		return 0
	}
	return C.int(len(activeForm.changed))
}

//export docwebhub_changed_id
func docwebhub_changed_id(index C.int) *C.char {
	if activeForm == nil {
		return formCString("")
	}
	i := int(index)
	if i < 0 || i >= len(activeForm.changed) {
		return formCString("")
	}
	return formCString(activeForm.changed[i])
}
