//go:build windows

// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package ui

import "syscall"

// Win32 constants, procedures and structures shared by the Windows form
// renderer. They are kept separate from the rendering code so the latter reads
// as layout and actions rather than a wall of API declarations.
const (
	windowClassName = "docWebHubWindow"
	windowTitle     = "docWebHub"

	linkID       = 1001
	openBtnID    = 1002
	chooseBtnID  = 1003
	portApplyID  = 1005
	accessBtnID  = 1006
	bindComboID  = 1007
	userEditID   = 1008
	passEditID   = 1009
	authApplyID  = 1010
	langComboID  = 1011
	copyBtnID    = 1012
	windowMargin = 24
)

const (
	wsOverlapped  = 0x00000000
	wsCaption     = 0x00C00000
	wsSysMenu     = 0x00080000
	wsMinimizeBox = 0x00020000
	wsMaximizeBox = 0x00010000
	wsThickFrame  = 0x00040000
	wsChild       = 0x40000000
	wsVisible     = 0x10000000
	wsTabStop     = 0x00010000

	csHRedraw = 0x0002
	csVRedraw = 0x0001

	ssLeft   = 0x0000
	ssNotify = 0x0100

	bsPushButton = 0x00000000
	bsOwnerDraw  = 0x0000000B

	wsBorder  = 0x00800000
	wsVScroll = 0x00200000
	wsHScroll = 0x00100000

	esNumber      = 0x2000
	esMultiline   = 0x0004
	esAutoVScroll = 0x0040
	esAutoHScroll = 0x0080
	esReadOnly    = 0x0800
	esPassword    = 0x0020

	cbsDropDownList = 0x0003
	cbAddString     = 0x0143
	cbResetContent  = 0x014B
	cbSetCurSel     = 0x014E
	cbGetCurSel     = 0x0147
	cbnSelChange    = 1

	infoWindowClassName = "docWebHubInfoWindow"

	// Tooltip control: the language picker explains itself on hover.
	tooltipClassName  = "tooltips_class32"
	wsPopup           = 0x80000000
	ttsNoPrefix       = 0x0002
	ttsAlwaysTip      = 0x0001
	ttfIDIsHwnd       = 0x0001
	ttfSubclass       = 0x0010
	ttmAddTool        = 0x0400 + 50
	ttmUpdateTipText  = 0x0400 + 57
	ttmSetMaxTipWidth = 0x0400 + 24
	// ICC_WIN95_CLASSES, which registers the tooltip class among others.
	iccWin95Classes = 0x000000FF

	wmDestroy        = 0x0002
	wmClose          = 0x0010
	wmCommand        = 0x0111
	wmSetFont        = 0x0030
	wmCtlColorStatic = 0x0138
	wmSize           = 0x0005
	wmNull           = 0x0000
	wmPaint          = 0x000F
	wmDrawItem       = 0x002B
	wmGetFont        = 0x0031
	wmEraseBkgnd     = 0x0014

	// GDI stock objects and drawing flags.
	nullBrush    = 5
	dtCenter     = 0x0001
	dtVCenter    = 0x0004
	dtSingleLine = 0x0020
	dtNoPrefix   = 0x0800
	psSolid      = 0

	// Clipboard.
	cfUnicodeText = 13
	gmemMoveable  = 0x0002

	// Window colours, COLORREF is 0x00BBGGRR.
	colorCardFill   = 0x00F5F5F5
	colorCardEdge   = 0x00EBEBEB
	colorButtonEdge = 0x00D9D9D9
	colorPrimary    = 0x00FF7A00 // #007AFF
	colorText       = 0x001E1C1E
	colorMuted      = 0x00736E6E
	colorLink       = 0x00CC6600
	colorGreen      = 0x0041CD28 // #28CD41
	colorGreenSoft  = 0x00A8EBB4
	colorGreenGlow  = 0x00DCF6E1
	colorOrange     = 0x00008CFF
	colorOrangeSoft = 0x0066B3FF
	colorOrangeGlow = 0x00CCE0FF
	colorWhite      = 0x00FFFFFF

	wmRestoreCopyTitle = wmApp + 3

	// Tray icon: custom callback message and click events.
	wmApp          = 0x8000
	wmTrayCallback = wmApp + 1
	wmLButtonUp    = 0x0202
	wmRButtonUp    = 0x0205

	// Shell_NotifyIconW
	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifInfo    = 0x00000010

	// Balloon icon shown next to the text. NIIF_NONE keeps it neutral so an
	// advisory looks like information, not like the shell's error icon.
	niifNone = 0x00000000
	niifInfo = 0x00000001

	// Tray menu.
	menuShowWindow  = 2001
	menuOpenBrowser = 2002
	menuQuit        = 2003

	mfString    = 0x00000000
	mfSeparator = 0x00000800

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	// Option bits for SHBrowseForFolder.
	bifReturnOnlyFSDirs = 0x00000001
	bifEditBox          = 0x00000010
	bifNewDialogStyle   = 0x00000040

	mbIconWarning = 0x00000030

	swHide = 0
	swShow = 5

	colorWindow = 5
	idcArrow    = 32512
	idiApp      = 32512

	defaultCharset    = 1
	outDefaultPrecis  = 0
	clipDefaultPrecis = 0
	cleartypeQuality  = 5
	fwNormal          = 400
	fwSemibold        = 600

	smCXScreen = 0
	smCYScreen = 1

	// DPI_AWARENESS_CONTEXT_SYSTEM_AWARE, which is -2 when passed as a uintptr.
	dpiAwarenessSystemAware = ^uintptr(1)
	dpiAwarenessSystem      = 1
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shcore   = syscall.NewLazyDLL("shcore.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procSendMessageW     = user32.NewProc("SendMessageW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procLoadIconW        = user32.NewProc("LoadIconW")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procGetSysColorBrush = user32.NewProc("GetSysColorBrush")
	procCreateFontW      = gdi32.NewProc("CreateFontW")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")

	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
	procGetDpiForSystem               = user32.NewProc("GetDpiForSystem")
	procSetProcessDpiAwareness        = shcore.NewProc("SetProcessDpiAwareness")

	procSetWindowTextW       = user32.NewProc("SetWindowTextW")
	procGetClientRect        = user32.NewProc("GetClientRect")
	procMoveWindow           = user32.NewProc("MoveWindow")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procMessageBoxW          = user32.NewProc("MessageBoxW")
	procInvalidateRect       = user32.NewProc("InvalidateRect")
	procSHBrowseForFolderW   = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDListW = shell32.NewProc("SHGetPathFromIDListW")
	procCoTaskMemFree        = ole32.NewProc("CoTaskMemFree")

	procShellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
	procExtractIconExW      = shell32.NewProc("ExtractIconExW")
	procGetModuleFileNameW  = kernel32.NewProc("GetModuleFileNameW")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procPostMessageW        = user32.NewProc("PostMessageW")

	procBeginPaint          = user32.NewProc("BeginPaint")
	procEndPaint            = user32.NewProc("EndPaint")
	procDrawTextW           = user32.NewProc("DrawTextW")
	procAdjustWindowRectEx  = user32.NewProc("AdjustWindowRectEx")
	procGetStockObject      = gdi32.NewProc("GetStockObject")
	procRoundRect           = gdi32.NewProc("RoundRect")
	procEllipse             = gdi32.NewProc("Ellipse")
	procCreateSolidBrush    = gdi32.NewProc("CreateSolidBrush")
	procCreatePen           = gdi32.NewProc("CreatePen")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procGetTextExtentPointW = gdi32.NewProc("GetTextExtentPoint32W")

	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procGlobalFree       = kernel32.NewProc("GlobalFree")

	// Tooltips live in comctl32; the call below registers their window class.
	procInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type point struct {
	x int32
	y int32
}

type rect struct {
	left   int32
	top    int32
	right  int32
	bottom int32
}

type paintStruct struct {
	hdc         syscall.Handle
	fErase      int32
	rcPaint     rect
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

// drawItemStruct mirrors DRAWITEMSTRUCT, sent for owner-drawn buttons.
type drawItemStruct struct {
	ctlType    uint32
	ctlID      uint32
	itemID     uint32
	itemAction uint32
	itemState  uint32
	hwndItem   syscall.Handle
	hdc        syscall.Handle
	rcItem     rect
	itemData   uintptr
}

type textSize struct {
	cx int32
	cy int32
}

type windowMsg struct {
	hwnd     syscall.Handle
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

type browseInfoW struct {
	hwndOwner      syscall.Handle
	pidlRoot       uintptr
	pszDisplayName *uint16
	lpszTitle      *uint16
	ulFlags        uint32
	lpfn           uintptr
	lParam         uintptr
	iImage         int32
}

// toolInfoW mirrors TOOLINFOW, the description of one tooltip.
type toolInfoW struct {
	cbSize     uint32
	uFlags     uint32
	hwnd       syscall.Handle
	uId        uintptr
	rect       rect
	hinst      syscall.Handle
	lpszText   uintptr
	lParam     uintptr
	lpReserved uintptr
}

// initCommonControlsEx mirrors INITCOMMONCONTROLSEX.
type initCommonControlsEx struct {
	dwSize uint32
	dwICC  uint32
}

// notifyIconData mirrors NOTIFYICONDATAW (V3, 976 bytes on 64-bit).
type notifyIconData struct {
	cbSize            uint32
	hWnd              syscall.Handle
	uID               uint32
	uFlags            uint32
	uCallbackMessage  uint32
	hIcon             syscall.Handle
	szTip             [128]uint16
	dwState           uint32
	dwStateMask       uint32
	szInfo            [256]uint16
	uTimeoutOrVersion uint32
	szInfoTitle       [64]uint16
	dwInfoFlags       uint32
	guidItem          [16]byte
	hBalloonIcon      syscall.Handle
}
