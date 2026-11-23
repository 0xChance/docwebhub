//go:build windows

// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package ui

import (
	"fmt"
	"log"
	"math"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/0xChance/docwebhub/internal/i18n"
)

const emSetCueBanner = 0x1501

var procRtlMoveMemory = kernel32.NewProc("RtlMoveMemory")

const (
	windowWidth      = 600
	contentMinHeight = 500
)

type formControl struct {
	id        string
	kind      string
	action    string
	style     string
	icon      string
	row       int
	column    int
	rowKind   string
	hwnd      syscall.Handle
	controlID uint32
}

type rowLabelControl struct {
	row  int
	hwnd syscall.Handle
}

var (
	appInstance syscall.Handle
	mainWindow  syscall.Handle
	infoWindow  syscall.Handle
	infoEdit    syscall.Handle
	trayAdded   bool

	dpiScale   = 1.0
	brushWhite syscall.Handle
	brushCard  syscall.Handle

	formControls    []*formControl
	formByID        map[string]*formControl
	formByHwnd      map[syscall.Handle]*formControl
	formByControlID map[uint32]*formControl
	formRowLabels   []rowLabelControl
	cardRect        rect
	statusDotX      int
	statusDotY      int
	statusDotRadius int
	cueBanners      [][]uint16
)

// Run shows the native Win32 window until the user closes it.
func Run(opts Options) error {
	runtime.LockOSThread()
	startForm(opts)

	// Declare DPI awareness first, so the screen metrics read below are physical pixels.
	setupDPI()

	appInstance = syscall.Handle(mustCall(procGetModuleHandleW, 0))

	cursor := mustCall(procLoadCursorW, 0, idcArrow)
	icon := mustCall(procLoadIconW, 0, idiApp)

	className := utf16ptr(windowClassName)
	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		style:         csHRedraw | csVRedraw,
		lpfnWndProc:   syscall.NewCallback(windowProc),
		hInstance:     appInstance,
		hIcon:         syscall.Handle(icon),
		hCursor:       syscall.Handle(cursor),
		hbrBackground: syscall.Handle(colorWindow + 1),
		lpszClassName: className,
	}
	if ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		return fmt.Errorf("failed to register window class: %v", err)
	}

	clientWidth := dp(windowWidth)
	clientHeight := dp(windowContentHeight())
	style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox)
	frame := rect{0, 0, int32(clientWidth), int32(clientHeight)}
	procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&frame)), style, 0, 0)
	width, height := int(frame.right-frame.left), int(frame.bottom-frame.top)

	screenWidth := mustCall(procGetSystemMetrics, smCXScreen)
	screenHeight := mustCall(procGetSystemMetrics, smCYScreen)
	x := (int(screenWidth) - width) / 2
	y := (int(screenHeight) - height) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16ptr(activeForm.form.title))),
		style,
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		0, 0, uintptr(appInstance), 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("failed to create window: %v", err)
	}
	mainWindow = syscall.Handle(hwnd)

	whiteBrush, _, _ := procCreateSolidBrush.Call(colorWhite)
	cardBrush, _, _ := procCreateSolidBrush.Call(colorCardFill)
	brushWhite = syscall.Handle(whiteBrush)
	brushCard = syscall.Handle(cardBrush)

	buildForm()
	addTrayIcon(mainWindow)

	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)

	var message windowMsg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}

	return nil
}

// windowContentHeight is the height of the client area at 96 DPI. The form rows
// drive it, so adding a row grows the window instead of clipping the footer.
func windowContentHeight() int {
	settingCount := 0
	for _, row := range activeForm.form.rows {
		if row.group == groupOverview {
			continue // this renderer has no overview tab yet
		}
		if row.kind == "setting" {
			settingCount++
		}
	}
	cardHeight := 32 + settingCount*36
	hintY := 174 + cardHeight + 22
	footerY := hintY + 32
	return max(contentMinHeight, footerY+60)
}

// buildForm creates every native widget from the shared form description.
func buildForm() {
	formControls = nil
	formByID = make(map[string]*formControl)
	formByHwnd = make(map[syscall.Handle]*formControl)
	formByControlID = make(map[uint32]*formControl)
	formRowLabels = nil
	cueBanners = nil
	statusDotRadius = dp(5)

	form := &activeForm.form
	settingCount := 0
	for _, row := range form.rows {
		if row.group == groupOverview {
			continue
		}
		if row.kind == "setting" {
			settingCount++
		}
	}
	cardHeight := 32 + settingCount*36
	hintY := 174 + cardHeight + 22
	footerY := hintY + 32
	cardRect = rect{
		left:   int32(dp(windowMargin)),
		top:    int32(dp(174)),
		right:  int32(dp(windowWidth - windowMargin)),
		bottom: int32(dp(174 + cardHeight)),
	}

	fontHeading := createFont(int32(-dp(22)), fwSemibold, false)
	fontBody := createFont(int32(-dp(15)), fwNormal, false)
	fontSmall := createFont(int32(-dp(13)), fwNormal, false)
	fontLink := createFont(int32(-dp(14)), fwNormal, true)
	fontButton := createFont(int32(-dp(13)), fwSemibold, false)

	settingIndex := 0
	for rowIndex, row := range form.rows {
		// The overview tab is GTK-only for now. Rendering its rows here would
		// stack unknown kinds into the fixed layout below.
		if row.group == groupOverview {
			continue
		}
		switch row.kind {
		case "header":
			for column, field := range row.fields {
				y := 24
				height := 18
				switch field.kind {
				case kindHeading:
					y = 56
					height = 32
				case kindDescription:
					y = 94
					height = 18
				}
				x := windowMargin
				width := windowWidth - windowMargin*2
				if field.kind == kindStatus {
					statusDotX = dp(x + 5)
					statusDotY = dp(y + height/2)
					x += 18
					width -= 18
				}
				textFont := fontSmall
				if field.kind == kindHeading {
					textFont = fontHeading
				}
				createFormField(rowIndex, column, row.kind,
					dp(x), dp(y), dp(width), dp(height), textFont)
			}

		case "url":
			widths := formWidths(row.fields, dp(windowWidth-windowMargin*2), dp(10))
			x := dp(windowMargin)
			for column, field := range row.fields {
				height := formFieldHeight(field.kind)
				y := dp(130) + (dp(30)-height)/2
				font := fontBody
				if field.kind == kindLink {
					font = fontLink
				} else if field.kind == kindButton {
					font = fontButton
				}
				createFormField(rowIndex, column, row.kind, x, y, widths[column], height, font)
				x += widths[column] + dp(10)
			}

		case "setting":
			rowY := dp(174 + 16 + settingIndex*36)
			if row.label != "" {
				label := createControl(uintptr(mainWindow), "STATIC", row.label,
					wsChild|wsVisible|ssLeft,
					dp(40), rowY+dp(9), dp(76), dp(18), 0, fontSmall)
				formByHwnd[label] = &formControl{row: rowIndex, rowKind: row.kind, kind: "row-label", style: styleDefault}
				formRowLabels = append(formRowLabels, rowLabelControl{row: rowIndex, hwnd: label})
			}

			widths := formWidths(row.fields, dp(428), dp(12))
			x := dp(132)
			for column, field := range row.fields {
				if field.kind == kindSpacer {
					x += widths[column] + dp(12)
					continue
				}
				height := formFieldHeight(field.kind)
				y := rowY + (dp(36)-height)/2
				font := fontSmall
				switch field.kind {
				case kindButton:
					font = fontButton
				case kindLink:
					font = fontLink
				case kindInput, kindNumber, kindPassword, kindSelect:
					font = fontBody
				}
				createFormField(rowIndex, column, row.kind, x, y, widths[column], height, font)
				x += widths[column] + dp(12)
			}
			settingIndex++

		case "hint":
			for column := range row.fields {
				createFormField(rowIndex, column, row.kind,
					dp(windowMargin), dp(hintY), dp(windowWidth-windowMargin*2), dp(16), fontSmall)
			}

		case "footer":
			if row.label != "" {
				label := createControl(uintptr(mainWindow), "STATIC", row.label,
					wsChild|wsVisible|ssLeft,
					dp(windowMargin), dp(footerY+6), dp(76), dp(18), 0, fontSmall)
				formByHwnd[label] = &formControl{row: rowIndex, rowKind: row.kind, kind: "row-label", style: styleDefault}
				formRowLabels = append(formRowLabels, rowLabelControl{row: rowIndex, hwnd: label})
			}

			widths := formWidths(row.fields, dp(472), dp(12))
			x := dp(104)
			for column, field := range row.fields {
				if field.kind == kindSpacer {
					x += widths[column] + dp(12)
					continue
				}
				height := formFieldHeight(field.kind)
				y := dp(footerY) + (dp(30)-height)/2
				font := fontBody
				if field.kind == kindButton {
					font = fontButton
				}
				createFormField(rowIndex, column, row.kind, x, y, widths[column], height, font)
				x += widths[column] + dp(12)
			}
		}
	}

	applyFormLabels()
}

func formFieldHeight(kind string) int {
	switch kind {
	case kindButton:
		return dp(30)
	case kindSelect:
		return dp(28)
	case kindInput, kindNumber, kindPassword:
		return dp(26)
	case kindLink:
		return dp(26)
	case kindHint:
		return dp(16)
	default:
		return dp(18)
	}
}

func formWidths(fields []formField, total, gap int) []int {
	widths := make([]int, len(fields))
	if len(fields) == 0 {
		return widths
	}

	fixed := 0
	stretch := 0
	for i, field := range fields {
		if field.width < 0 {
			stretch++
			continue
		}
		widths[i] = dp(field.width)
		fixed += widths[i]
	}

	available := total - gap*(len(fields)-1)
	stretchWidth := 0
	if stretch > 0 {
		stretchWidth = max(0, (available-fixed)/stretch)
	}
	for i, field := range fields {
		if field.width < 0 {
			widths[i] = stretchWidth
		}
	}
	return widths
}

func createFormField(row, column int, rowKind string, x, y, width, height int, font syscall.Handle) *formControl {
	field := activeForm.form.rows[row].fields[column]
	control := &formControl{
		id:      field.id,
		kind:    field.kind,
		action:  field.action,
		style:   field.style,
		icon:    field.icon,
		row:     row,
		column:  column,
		rowKind: rowKind,
	}

	var class, text string
	var style uintptr
	var controlID uint32

	switch field.kind {
	case kindButton:
		class, text = "BUTTON", field.label
		style = wsChild | wsVisible | wsTabStop | bsOwnerDraw
	case kindLink:
		class, text = "STATIC", field.value
		style = wsChild | wsVisible | ssLeft | ssNotify
	case kindInput, kindNumber, kindPassword:
		class, text = "EDIT", field.value
		style = wsChild | wsVisible | wsTabStop | wsBorder
		if field.kind == kindNumber {
			style |= esNumber
		}
		if field.kind == kindPassword {
			style |= esPassword
		}
	case kindSelect:
		class = "COMBOBOX"
		style = wsChild | wsVisible | wsTabStop | wsVScroll | cbsDropDownList
	case kindStatus, kindHeading, kindDescription, kindValue, kindHint:
		class, text = "STATIC", field.value
		if field.kind == kindHint {
			text = field.label
		}
		style = wsChild | wsVisible | ssLeft
	default:
		return control
	}

	if field.action != "" && (field.kind == kindButton || field.kind == kindLink || field.kind == kindSelect) {
		controlID = uint32(3000 + len(formControls))
	}

	createHeight := height
	if field.kind == kindSelect {
		// For a combo box the height includes the drop-down list. The closed
		// control still draws at its normal height, but the popup needs room.
		createHeight = dp(220)
	}

	hwnd := createControl(uintptr(mainWindow), class, text, style,
		x, y, width, createHeight, uintptr(controlID), font)
	control.hwnd = hwnd
	control.controlID = controlID

	formControls = append(formControls, control)
	formByID[field.id] = control
	formByHwnd[hwnd] = control
	if controlID != 0 {
		formByControlID[controlID] = control
	}

	if field.kind == kindSelect {
		applyFormField(control, true)
	} else {
		if field.kind == kindInput || field.kind == kindNumber || field.kind == kindPassword {
			setCueBanner(hwnd, field.placeholder)
		}
	}
	return control
}

func applyFormField(control *formControl, updateValue bool) {
	if control == nil {
		return
	}
	field, ok := activeForm.fieldAt(control.row, control.column)
	if !ok {
		return
	}
	control.style = field.style
	control.action = field.action

	switch field.kind {
	case kindButton:
		setWindowText(control.hwnd, field.label)
	case kindLink:
		setWindowText(control.hwnd, field.value)
	case kindInput, kindNumber, kindPassword:
		if updateValue {
			setWindowText(control.hwnd, field.value)
		}
		setCueBanner(control.hwnd, field.placeholder)
	case kindSelect:
		resetCombo(control.hwnd)
		selected := 0
		for i, option := range field.options {
			addComboString(control.hwnd, option.label)
			if option.value == field.value {
				selected = i
			}
		}
		procSendMessageW.Call(uintptr(control.hwnd), cbSetCurSel, uintptr(selected), 0)
	case kindHint:
		setWindowText(control.hwnd, field.label)
	default:
		setWindowText(control.hwnd, field.value)
	}

	procInvalidateRect.Call(uintptr(control.hwnd), 0, 1)
	if field.kind == kindStatus {
		procInvalidateRect.Call(uintptr(mainWindow), 0, 1)
	}
}

func applyChangedFields() {
	for _, id := range activeForm.changedIDs() {
		applyFormField(formByID[id], true)
	}
}

func applyFormLabels() {
	if mainWindow != 0 {
		setWindowText(mainWindow, activeForm.form.title)
	}
	for _, control := range formControls {
		applyFormField(control, false)
	}
	for _, label := range formRowLabels {
		if label.row >= 0 && label.row < len(activeForm.form.rows) {
			setWindowText(label.hwnd, activeForm.form.rows[label.row].label)
		}
	}
	if infoWindow != 0 {
		setWindowText(infoWindow, i18n.T("ui.access.title"))
	}
	procInvalidateRect.Call(uintptr(mainWindow), 0, 1)
}

func windowProc(hwnd syscall.Handle, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmClose:
		// Closing the window only hides it; the server keeps running in the tray.
		procShowWindow.Call(uintptr(hwnd), swHide)
		return 0
	case wmDestroy:
		removeTrayIcon()
		procPostQuitMessage.Call(0)
		return 0
	case wmTrayCallback:
		switch uint32(lParam) {
		case wmLButtonUp:
			showMainWindow(hwnd)
		case wmRButtonUp:
			showTrayMenu(hwnd)
		}
		return 0
	case wmCommand:
		id := uint32(wParam & 0xffff)
		code := uint16(wParam >> 16)
		if control := formByControlID[id]; control != nil {
			if control.kind == kindSelect && code == cbnSelChange {
				performFormAction(control)
				return 0
			}
			if code == 0 {
				performFormAction(control)
				return 0
			}
		}
	case wmCtlColorStatic:
		control := formByHwnd[syscall.Handle(lParam)]
		brush := brushWhite
		if control != nil && control.rowKind == "setting" {
			brush = brushCard
		}
		procSetBkMode.Call(wParam, 1)
		procSetTextColor.Call(wParam, staticTextColor(control))
		return uintptr(brush)
	case wmPaint:
		paintWindow(hwnd)
		return 0
	case wmDrawItem:
		var item drawItemStruct
		copyMemory(uintptr(unsafe.Pointer(&item)), lParam, unsafe.Sizeof(item))
		drawButton(&item)
		return 1
	case wmRestoreCopyTitle:
		if control := formByID["copy"]; control != nil {
			if field, ok := activeForm.field("copy"); ok {
				setWindowText(control.hwnd, field.label)
			}
		}
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return ret
}

func staticTextColor(control *formControl) uintptr {
	if control == nil {
		return colorText
	}
	switch control.kind {
	case kindLink:
		return colorLink
	case kindStatus:
		if control.style == styleWarning {
			return colorOrange
		}
		return colorText
	case kindDescription, kindHint:
		return colorMuted
	case kindValue:
		switch control.style {
		case styleSuccess:
			return colorGreen
		case styleWarning:
			return colorOrange
		case styleMuted, styleFaint:
			return colorMuted
		default:
			return colorText
		}
	case "row-label":
		return colorText
	default:
		return colorText
	}
}

func performFormAction(control *formControl) {
	if control == nil || control.action == "" {
		return
	}

	if control.action == actionCopyURL {
		copyCurrentURL(control)
		return
	}

	value1, value2 := "", ""
	switch {
	case control.action == actionChooseRoot:
		dir, ok := chooseFolder(mainWindow)
		if !ok {
			return
		}
		value1 = dir
	case control.kind == kindSelect:
		value1 = selectedComboValue(control)
	default:
		value1, value2 = rowInputValues(control.row)
	}

	message := activeForm.action(control.action, value1, value2)
	if message != "" {
		showWarning(mainWindow, activeForm.errorTitle, message)
		applyChangedFields()
		return
	}

	if activeForm.labelsChanged {
		applyFormLabels()
	}
	applyChangedFields()
	if activeForm.actionText != "" {
		showAccessInfoWindow(activeForm.actionText)
	}
}

func rowInputValues(row int) (string, string) {
	value1, value2 := "", ""
	first := true
	for _, control := range formControls {
		if control.row != row {
			continue
		}
		if control.kind != kindInput && control.kind != kindNumber && control.kind != kindPassword {
			continue
		}
		text := getWindowText(control.hwnd)
		if first {
			value1 = text
			first = false
		} else {
			value2 = text
		}
	}
	return value1, value2
}

func selectedComboValue(control *formControl) string {
	field, ok := activeForm.fieldAt(control.row, control.column)
	if !ok {
		return ""
	}
	index, _, _ := procSendMessageW.Call(uintptr(control.hwnd), cbGetCurSel, 0, 0)
	i := int(index)
	if i < 0 || i >= len(field.options) {
		return ""
	}
	return field.options[i].value
}

func resetCombo(hwnd syscall.Handle) {
	procSendMessageW.Call(uintptr(hwnd), cbResetContent, 0, 0)
}

func addComboString(hwnd syscall.Handle, text string) {
	procSendMessageW.Call(uintptr(hwnd), cbAddString, 0,
		uintptr(unsafe.Pointer(utf16ptr(text))))
}

func setCueBanner(hwnd syscall.Handle, text string) {
	if text == "" {
		return
	}
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return
	}
	cueBanners = append(cueBanners, utf16)
	procSendMessageW.Call(uintptr(hwnd), emSetCueBanner, 1,
		uintptr(unsafe.Pointer(&utf16[0])))
}

func copyCurrentURL(control *formControl) {
	field, ok := activeForm.field("url")
	if !ok || field.value == "" {
		return
	}
	if !copyText(mainWindow, field.value) {
		return
	}

	setWindowText(control.hwnd, i18n.T("ui.url.copied"))
	time.AfterFunc(1200*time.Millisecond, func() {
		procPostMessageW.Call(uintptr(mainWindow), wmRestoreCopyTitle, 0, 0)
	})
}

func copyText(owner syscall.Handle, text string) bool {
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return false
	}
	if ok, _, _ := procOpenClipboard.Call(uintptr(owner)); ok == 0 {
		return false
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()

	handle, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(len(utf16)*2))
	if handle == 0 {
		return false
	}
	ptr, _, _ := procGlobalLock.Call(handle)
	if ptr == 0 {
		procGlobalFree.Call(handle)
		return false
	}
	copyMemory(ptr, uintptr(unsafe.Pointer(&utf16[0])), uintptr(len(utf16)*2))
	procGlobalUnlock.Call(handle)

	if ok, _, _ := procSetClipboardData.Call(cfUnicodeText, handle); ok == 0 {
		procGlobalFree.Call(handle)
		return false
	}
	return true
}

func paintWindow(hwnd syscall.Handle) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))

	screen := syscall.Handle(hdc)
	if cardRect.right > cardRect.left && cardRect.bottom > cardRect.top {
		drawRoundedRect(screen, int(cardRect.left), int(cardRect.top),
			int(cardRect.right), int(cardRect.bottom), dp(16),
			uintptr(colorCardFill), uintptr(colorCardEdge), 1)
	}

	style := styleSuccess
	if field, ok := activeForm.field("status"); ok {
		style = field.style
	}
	glow, soft, solid := uintptr(colorGreenGlow), uintptr(colorGreenSoft), uintptr(colorGreen)
	if style == styleWarning {
		glow, soft, solid = uintptr(colorOrangeGlow), uintptr(colorOrangeSoft), uintptr(colorOrange)
	}
	drawCircle(screen, statusDotX, statusDotY, dp(9), glow)
	drawCircle(screen, statusDotX, statusDotY, dp(7), soft)
	drawCircle(screen, statusDotX, statusDotY, statusDotRadius, solid)
}

func drawRoundedRect(hdc syscall.Handle, x1, y1, x2, y2, radius int, fill, edge uintptr, width int) {
	brush, _, _ := procCreateSolidBrush.Call(fill)
	pen, _, _ := procCreatePen.Call(psSolid, uintptr(width), edge)
	oldBrush, _, _ := procSelectObject.Call(uintptr(hdc), brush)
	oldPen, _, _ := procSelectObject.Call(uintptr(hdc), pen)
	procRoundRect.Call(uintptr(hdc), uintptr(x1), uintptr(y1), uintptr(x2), uintptr(y2),
		uintptr(radius), uintptr(radius))
	procSelectObject.Call(uintptr(hdc), oldBrush)
	procSelectObject.Call(uintptr(hdc), oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)
}

func drawCircle(hdc syscall.Handle, cx, cy, radius int, color uintptr) {
	brush, _, _ := procCreateSolidBrush.Call(color)
	pen, _, _ := procCreatePen.Call(psSolid, 1, color)
	oldBrush, _, _ := procSelectObject.Call(uintptr(hdc), brush)
	oldPen, _, _ := procSelectObject.Call(uintptr(hdc), pen)
	procEllipse.Call(uintptr(hdc), uintptr(cx-radius), uintptr(cy-radius),
		uintptr(cx+radius), uintptr(cy+radius))
	procSelectObject.Call(uintptr(hdc), oldBrush)
	procSelectObject.Call(uintptr(hdc), oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)
}

func drawButton(item *drawItemStruct) {
	if item == nil {
		return
	}
	hdc := item.hdc
	r := item.rcItem
	control := formByControlID[item.ctlID]
	primary := control != nil && control.style == stylePrimary

	fill, edge, textColor := uintptr(colorWhite), uintptr(colorButtonEdge), uintptr(colorText)
	if primary {
		fill, edge, textColor = uintptr(colorPrimary), uintptr(colorPrimary), uintptr(colorWhite)
	}
	drawRoundedRect(hdc, int(r.left), int(r.top), int(r.right), int(r.bottom), dp(10), fill, edge, 1)

	label := getWindowText(item.hwndItem)
	if label == "" {
		return
	}

	font, _, _ := procSendMessageW.Call(uintptr(item.hwndItem), wmGetFont, 0, 0)
	oldFont, _, _ := procSelectObject.Call(uintptr(hdc), font)
	defer procSelectObject.Call(uintptr(hdc), oldFont)

	text := utf16ptr(label)
	units := len(syscall.StringToUTF16(label)) - 1
	var size textSize
	procGetTextExtentPointW.Call(uintptr(hdc), uintptr(unsafe.Pointer(text)), uintptr(units),
		uintptr(unsafe.Pointer(&size)))

	iconWidth := 0
	if control != nil && (control.icon == iconCopy || control.icon == iconFolder) {
		iconWidth = dp(16) + dp(6)
	}

	contentWidth := int(size.cx) + iconWidth
	left := int(r.left) + (int(r.right-r.left)-contentWidth)/2
	top := int(r.top) + (int(r.bottom-r.top)-int(size.cy))/2

	if control != nil {
		switch control.icon {
		case iconFolder:
			drawFolderIcon(hdc, left, top+dp(1), int(size.cy), textColor)
		case iconCopy:
			drawCopyIcon(hdc, left, top, int(size.cy), textColor)
		}
	}

	textRect := rect{
		left:   int32(left + iconWidth),
		top:    r.top,
		right:  int32(left + iconWidth + int(size.cx)),
		bottom: r.bottom,
	}
	procSetTextColor.Call(uintptr(hdc), textColor)
	procSetBkMode.Call(uintptr(hdc), 1)
	procDrawTextW.Call(uintptr(hdc), uintptr(unsafe.Pointer(text)), uintptr(units),
		uintptr(unsafe.Pointer(&textRect)), dtVCenter|dtSingleLine|dtNoPrefix)
}

func drawFolderIcon(hdc syscall.Handle, x, y, height int, color uintptr) {
	width := dp(15)
	pen, _, _ := procCreatePen.Call(psSolid, 1, color)
	oldPen, _, _ := procSelectObject.Call(uintptr(hdc), pen)
	brush, _, _ := procGetStockObject.Call(nullBrush)
	oldBrush, _, _ := procSelectObject.Call(uintptr(hdc), brush)

	procRoundRect.Call(uintptr(hdc), uintptr(x), uintptr(y+1), uintptr(x+dp(7)), uintptr(y+dp(5)), 2, 2)
	procRoundRect.Call(uintptr(hdc), uintptr(x), uintptr(y+dp(4)), uintptr(x+width), uintptr(y+height-1), 3, 3)

	procSelectObject.Call(uintptr(hdc), oldBrush)
	procSelectObject.Call(uintptr(hdc), oldPen)
	procDeleteObject.Call(pen)
}

func drawCopyIcon(hdc syscall.Handle, x, y, height int, color uintptr) {
	back := dp(9)
	front := dp(9)
	pen, _, _ := procCreatePen.Call(psSolid, 1, color)
	oldPen, _, _ := procSelectObject.Call(uintptr(hdc), pen)
	brush, _, _ := procGetStockObject.Call(nullBrush)
	oldBrush, _, _ := procSelectObject.Call(uintptr(hdc), brush)

	procRoundRect.Call(uintptr(hdc), uintptr(x), uintptr(y), uintptr(x+back), uintptr(y+height-dp(3)), 2, 2)
	procRoundRect.Call(uintptr(hdc), uintptr(x+dp(4)), uintptr(y+dp(3)), uintptr(x+dp(4)+front), uintptr(y+height), 2, 2)

	procSelectObject.Call(uintptr(hdc), oldBrush)
	procSelectObject.Call(uintptr(hdc), oldPen)
	procDeleteObject.Call(pen)
}

func createControl(parent uintptr, class, text string, style uintptr, x, y, width, height int, id uintptr, font syscall.Handle) syscall.Handle {
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(utf16ptr(class))),
		uintptr(unsafe.Pointer(utf16ptr(text))),
		style,
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		parent, id, uintptr(appInstance), 0,
	)
	procSendMessageW.Call(hwnd, wmSetFont, uintptr(font), 1)
	return syscall.Handle(hwnd)
}

func createFont(height, weight int32, underline bool) syscall.Handle {
	var underlined uintptr
	if underline {
		underlined = 1
	}

	font, _, _ := procCreateFontW.Call(
		uintptr(height), 0, 0, 0, uintptr(weight), 0, underlined, 0,
		defaultCharset, outDefaultPrecis, clipDefaultPrecis, cleartypeQuality,
		0, uintptr(unsafe.Pointer(utf16ptr("Microsoft YaHei UI"))),
	)
	return syscall.Handle(font)
}

func showAccessInfoWindow(text string) {
	if infoWindow == 0 {
		if err := registerInfoWindowClass(); err != nil {
			log.Printf("%v", err)
			return
		}

		width, height := dp(680), dp(480)
		screenWidth := mustCall(procGetSystemMetrics, smCXScreen)
		screenHeight := mustCall(procGetSystemMetrics, smCYScreen)
		x := (int(screenWidth) - width) / 2
		y := (int(screenHeight) - height) / 2
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}

		handle, _, err := procCreateWindowExW.Call(
			0,
			uintptr(unsafe.Pointer(utf16ptr(infoWindowClassName))),
			uintptr(unsafe.Pointer(utf16ptr(i18n.T("ui.access.title")))),
			wsOverlapped|wsCaption|wsSysMenu|wsMinimizeBox|wsMaximizeBox|wsThickFrame,
			uintptr(x), uintptr(y), uintptr(width), uintptr(height),
			0, 0, uintptr(appInstance), 0,
		)
		if handle == 0 {
			log.Printf("failed to create access info window: %v", err)
			return
		}
		infoWindow = syscall.Handle(handle)

		infoFont := createFont(int32(-dp(13)), fwNormal, false)
		infoEdit = createControl(uintptr(infoWindow), "EDIT", "",
			wsChild|wsVisible|wsTabStop|wsBorder|wsVScroll|wsHScroll|
				esMultiline|esReadOnly|esAutoVScroll|esAutoHScroll,
			dp(12), dp(12), dp(600), dp(400), 0, infoFont)
		resizeInfoEdit(infoWindow)
	}

	setWindowText(infoWindow, i18n.T("ui.access.title"))
	setWindowText(infoEdit, text)
	procShowWindow.Call(uintptr(infoWindow), swShow)
	procSetForegroundWindow.Call(uintptr(infoWindow))
}

func registerInfoWindowClass() error {
	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		style:         csHRedraw | csVRedraw,
		lpfnWndProc:   syscall.NewCallback(infoWindowProc),
		hInstance:     appInstance,
		hCursor:       syscall.Handle(mustCall(procLoadCursorW, 0, idcArrow)),
		hbrBackground: syscall.Handle(colorWindow + 1),
		lpszClassName: utf16ptr(infoWindowClassName),
	}
	if ret, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		return fmt.Errorf("failed to register access info window class: %v", err)
	}
	return nil
}

func infoWindowProc(hwnd syscall.Handle, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmClose:
		procDestroyWindow.Call(uintptr(hwnd))
		return 0
	case wmDestroy:
		infoWindow = 0
		infoEdit = 0
		return 0
	case wmSize:
		if infoEdit != 0 {
			resizeInfoEdit(hwnd)
		}
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return ret
}

func resizeInfoEdit(hwnd syscall.Handle) {
	var client rect
	if ret, _, _ := procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&client))); ret == 0 {
		return
	}

	width := int(client.right-client.left) - dp(24)
	height := int(client.bottom-client.top) - dp(24)
	if width < 20 || height < 20 {
		return
	}

	procMoveWindow.Call(
		uintptr(infoEdit),
		uintptr(dp(12)), uintptr(dp(12)),
		uintptr(width), uintptr(height),
		1,
	)
}

func addTrayIcon(hwnd syscall.Handle) {
	var data notifyIconData
	data.cbSize = uint32(unsafe.Sizeof(data))
	data.hWnd = hwnd
	data.uID = 1
	data.uFlags = nifMessage | nifIcon | nifTip
	data.uCallbackMessage = wmTrayCallback
	data.hIcon = loadAppIcon()
	if tip, err := syscall.UTF16FromString(activeForm.form.title); err == nil {
		copy(data.szTip[:], tip)
	}

	if ret, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&data))); ret == 0 {
		log.Printf("failed to add tray icon")
		return
	}
	trayAdded = true
}

// NotifyUpdate shows a tray balloon for a security advisory.
//
// It returns false when there is no tray icon to attach the balloon to, which
// happens in -no-gui builds and before the window exists. The caller must not
// treat that as an error: the page banner still carries the advisory.
//
// Windows has no way to post a balloon without a NOTIFYICONDATA entry, so this
// deliberately reuses the tray icon the app already installs rather than
// creating a second one.
func NotifyUpdate(title, body string) bool {
	if !trayAdded || mainWindow == 0 {
		return false
	}

	var data notifyIconData
	data.cbSize = uint32(unsafe.Sizeof(data))
	data.hWnd = mainWindow
	data.uID = 1
	data.uFlags = nifInfo
	data.dwInfoFlags = niifInfo

	// Long text is clipped rather than truncated mid-rune: the fields are
	// fixed-size UTF-16 arrays, and Windows renders whatever fits.
	copyUTF16(data.szInfoTitle[:], title)
	copyUTF16(data.szInfo[:], body)

	ret, _, _ := procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&data)))
	if ret == 0 {
		log.Printf("failed to show a tray notification")
		return false
	}
	return true
}

// copyUTF16 fills a fixed-size UTF-16 buffer, leaving a terminating NUL.
func copyUTF16(dst []uint16, text string) {
	if len(dst) == 0 {
		return
	}
	src, err := syscall.UTF16FromString(text)
	if err != nil {
		return
	}
	if len(src) > len(dst) {
		src = src[:len(dst)-1]
	}
	copy(dst, src)
	dst[len(src)] = 0
}

func removeTrayIcon() {
	if !trayAdded {
		return
	}

	var data notifyIconData
	data.cbSize = uint32(unsafe.Sizeof(data))
	data.hWnd = mainWindow
	data.uID = 1
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
	trayAdded = false
}

func loadAppIcon() syscall.Handle {
	path := make([]uint16, 260)
	if ret, _, _ := procGetModuleFileNameW.Call(0, uintptr(unsafe.Pointer(&path[0])), uintptr(len(path))); ret == 0 {
		return 0
	}

	var large, small syscall.Handle
	count, _, _ := procExtractIconExW.Call(
		uintptr(unsafe.Pointer(&path[0])), 0,
		uintptr(unsafe.Pointer(&large)), uintptr(unsafe.Pointer(&small)), 1,
	)
	if count == 0 {
		return 0
	}
	if small != 0 {
		return small
	}
	return large
}

func showTrayMenu(hwnd syscall.Handle) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	type entry struct {
		id     int
		action string
	}
	var entries []entry
	nextID := 100
	for _, item := range activeForm.form.menu {
		if item.separator {
			appendMenu(menu, mfSeparator, 0, "")
			continue
		}
		appendMenu(menu, mfString, nextID, item.label)
		entries = append(entries, entry{id: nextID, action: item.action})
		nextID++
	}

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	procSetForegroundWindow.Call(uintptr(hwnd))
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd,
		uintptr(pt.x), uintptr(pt.y), 0, uintptr(hwnd), 0)
	procPostMessageW.Call(uintptr(hwnd), wmNull, 0, 0)

	for _, item := range entries {
		if item.id != int(cmd) {
			continue
		}
		switch item.action {
		case menuActionShow:
			showMainWindow(hwnd)
		case menuActionQuit:
			procDestroyWindow.Call(uintptr(hwnd))
		default:
			message := activeForm.action(item.action, "", "")
			if message != "" {
				showWarning(hwnd, activeForm.errorTitle, message)
			}
			applyChangedFields()
		}
		return
	}
}

func appendMenu(menu uintptr, flags uintptr, id int, text string) {
	var textPtr uintptr
	if text != "" {
		textPtr = uintptr(unsafe.Pointer(utf16ptr(text)))
	}
	procAppendMenuW.Call(menu, flags, uintptr(id), textPtr)
}

func showMainWindow(hwnd syscall.Handle) {
	procShowWindow.Call(uintptr(hwnd), swShow)
	procSetForegroundWindow.Call(uintptr(hwnd))
}

func getWindowText(hwnd syscall.Handle) string {
	buffer := make([]uint16, 256)
	procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	return syscall.UTF16ToString(buffer)
}

func setWindowText(hwnd syscall.Handle, text string) {
	procSetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(utf16ptr(text))))
}

func chooseFolder(owner syscall.Handle) (string, bool) {
	display := make([]uint16, 260)
	info := browseInfoW{
		hwndOwner:      owner,
		pszDisplayName: &display[0],
		lpszTitle:      utf16ptr(i18n.T("dialog.choose.message")),
		ulFlags:        bifReturnOnlyFSDirs | bifNewDialogStyle | bifEditBox,
	}

	pidl, _, _ := procSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&info)))
	if pidl == 0 {
		return "", false
	}
	defer procCoTaskMemFree.Call(pidl)

	path := make([]uint16, 260)
	if ret, _, _ := procSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&path[0]))); ret == 0 {
		return "", false
	}
	return syscall.UTF16ToString(path), true
}

func showWarning(owner syscall.Handle, title, text string) {
	procMessageBoxW.Call(
		uintptr(owner),
		uintptr(unsafe.Pointer(utf16ptr(text))),
		uintptr(unsafe.Pointer(utf16ptr(title))),
		mbIconWarning,
	)
}

func utf16ptr(s string) *uint16 {
	ptr, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return ptr
}

func mustCall(proc *syscall.LazyProc, args ...uintptr) uintptr {
	ret, _, _ := proc.Call(args...)
	return ret
}

// copyMemory copies count bytes between raw Win32 pointers. It is used for the
// pointers handed to window procedures and GlobalLock, which cannot be expressed
// as Go pointers without triggering the unsafe.Pointer vet check.
func copyMemory(dst, src, count uintptr) {
	procRtlMoveMemory.Call(dst, src, count)
}

// setupDPI declares the process system-DPI aware and derives the scale factor
// from the real DPI.
func setupDPI() {
	if ret, _, _ := procSetProcessDpiAwarenessContext.Call(dpiAwarenessSystemAware); ret == 0 {
		if ret, _, _ := procSetProcessDpiAwareness.Call(dpiAwarenessSystem); ret != 0 {
			procSetProcessDPIAware.Call()
		}
	}

	if dpi, _, _ := procGetDpiForSystem.Call(); dpi >= 96 {
		dpiScale = float64(dpi) / 96.0
	}
}

// dp converts a design size at 96 DPI into pixels at the current DPI.
func dp(value int) int {
	if dpiScale == 1 {
		return value
	}
	return int(math.Round(float64(value) * dpiScale))
}
