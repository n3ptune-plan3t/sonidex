//go:build windows

// Package winui is the Windows front-end: plain Win32 controls (BUTTON,
// EDIT, COMBOBOX, STATIC) and a GDI-drawn patchbay, called through
// syscall. No toolkit, no cgo of its own, nothing to initialise but
// user32/gdi32.
package winui

import (
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")

	pRegisterClassEx   = user32.NewProc("RegisterClassExW")
	pCreateWindowEx    = user32.NewProc("CreateWindowExW")
	pDefWindowProc     = user32.NewProc("DefWindowProcW")
	pGetMessage        = user32.NewProc("GetMessageW")
	pTranslateMessage  = user32.NewProc("TranslateMessage")
	pDispatchMessage   = user32.NewProc("DispatchMessageW")
	pIsDialogMessage   = user32.NewProc("IsDialogMessageW")
	pPostQuitMessage   = user32.NewProc("PostQuitMessage")
	pPostMessage       = user32.NewProc("PostMessageW")
	pSendMessage       = user32.NewProc("SendMessageW")
	pShowWindow        = user32.NewProc("ShowWindow")
	pMoveWindow        = user32.NewProc("MoveWindow")
	pSetWindowPos      = user32.NewProc("SetWindowPos")
	pSetWindowText     = user32.NewProc("SetWindowTextW")
	pGetWindowText     = user32.NewProc("GetWindowTextW")
	pGetWindowTextLen  = user32.NewProc("GetWindowTextLengthW")
	pLoadCursor        = user32.NewProc("LoadCursorW")
	pLoadIcon          = user32.NewProc("LoadIconW")
	pGetClientRect     = user32.NewProc("GetClientRect")
	pAdjustWindowRect  = user32.NewProc("AdjustWindowRectEx")
	pAdjustWindowRectD = user32.NewProc("AdjustWindowRectExForDpi")
	pGetDpiForWindow   = user32.NewProc("GetDpiForWindow")
	pSetProcessDPIAw   = user32.NewProc("SetProcessDPIAware")
	pGetDC             = user32.NewProc("GetDC")
	pReleaseDC         = user32.NewProc("ReleaseDC")
	pBeginPaint        = user32.NewProc("BeginPaint")
	pEndPaint          = user32.NewProc("EndPaint")
	pInvalidateRect    = user32.NewProc("InvalidateRect")
	pFillRect          = user32.NewProc("FillRect")
	pDrawText          = user32.NewProc("DrawTextW")
	pSetCapture        = user32.NewProc("SetCapture")
	pReleaseCapture    = user32.NewProc("ReleaseCapture")
	pSetForeground     = user32.NewProc("SetForegroundWindow")
	pGetSysColor       = user32.NewProc("GetSysColor")
	pGetSysColorBrush  = user32.NewProc("GetSysColorBrush")
	pDestroyWindow     = user32.NewProc("DestroyWindow")

	pCreateFont       = gdi32.NewProc("CreateFontW")
	pCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	pCreatePen        = gdi32.NewProc("CreatePen")
	pSelectObject     = gdi32.NewProc("SelectObject")
	pDeleteObject     = gdi32.NewProc("DeleteObject")
	pCreateCompatDC   = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatBmp  = gdi32.NewProc("CreateCompatibleBitmap")
	pDeleteDC         = gdi32.NewProc("DeleteDC")
	pBitBlt           = gdi32.NewProc("BitBlt")
	pRoundRect        = gdi32.NewProc("RoundRect")
	pEllipse          = gdi32.NewProc("Ellipse")
	pMoveToEx         = gdi32.NewProc("MoveToEx")
	pLineTo           = gdi32.NewProc("LineTo")
	pSetBkMode        = gdi32.NewProc("SetBkMode")
	pSetTextColor     = gdi32.NewProc("SetTextColor")
	pTextOut          = gdi32.NewProc("TextOutW")
	pGetDeviceCaps    = gdi32.NewProc("GetDeviceCaps")

	pGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
	pInitCommonCtrls = comctl32.NewProc("InitCommonControlsEx")
)

const (
	wsOverlapped   = 0x00000000
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
	wsThickFrame   = 0x00040000
	wsOverlappedWn = 0x00CF0000
	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsTabStop      = 0x00010000
	wsVScroll      = 0x00200000
	wsClipChildren = 0x02000000

	wsExControlParent = 0x00010000
	wsExClientEdge    = 0x00000200

	bsPushButton   = 0
	bsAutoCheckbox = 3
	esAutoHScroll  = 0x80
	cbsDropdownLst = 3
	ssCenter       = 1

	swHide = 0
	swShow = 5

	swpNoZOrder   = 0x0004
	swpNoMove     = 0x0002
	swpNoActivate = 0x0010

	wmDestroy     = 0x0002
	wmSize        = 0x0005
	wmSetFont     = 0x0030
	wmPaint       = 0x000F
	wmEraseBkgnd  = 0x0014
	wmCommand     = 0x0111
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmCaptureChg  = 0x0215
	wmDpiChanged  = 0x02E0
	wmApp         = 0x8000

	cbGetCurSel    = 0x0147
	cbGetLBText    = 0x0148
	cbGetLBTextLen = 0x0149
	cbAddString    = 0x0143
	cbResetContent = 0x014B
	cbSetCurSel    = 0x014E
	cbnSelChange   = 1

	bmGetCheck = 0x00F0
	bmSetCheck = 0x00F1

	emSetCueBanner = 0x1501

	colorWindow     = 5
	colorWindowText = 8
	colorHighlight  = 13
	colorBtnFace    = 15
	colorGrayText   = 17

	idcArrow = 32512
	srccopy  = 0x00CC0020
	psSolid  = 0
	bkTransp = 1

	dtWordBreak = 0x0010
	dtNoPrefix  = 0x0800
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	_       uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type paintStruct struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     rect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type initCommonControlsEx struct{ Size, ICC uint32 }

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func ptr(p *uint16) uintptr { return uintptr(unsafe.Pointer(p)) }

func sendMessage(h uintptr, m uint32, w, l uintptr) uintptr {
	r, _, _ := pSendMessage.Call(h, uintptr(m), w, l)
	return r
}

func postMessage(h uintptr, m uint32, w, l uintptr) {
	pPostMessage.Call(h, uintptr(m), w, l)
}

func showWindow(h uintptr, cmd int) { pShowWindow.Call(h, uintptr(cmd)) }

func invalidate(h uintptr) { pInvalidateRect.Call(h, 0, 0) }

func setText(h uintptr, s string) {
	p := utf16(s)
	pSetWindowText.Call(h, ptr(p))
}

func getText(h uintptr) string {
	n, _, _ := pGetWindowTextLen.Call(h)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	pGetWindowText.Call(h, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

func sysColor(idx int) uint32 {
	c, _, _ := pGetSysColor.Call(uintptr(idx))
	return uint32(c)
}

func rgb(r, g, b uint8) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }

// dpiFor returns the DPI of hwnd (per-monitor on Windows 10+, else system).
func dpiFor(hwnd uintptr) int {
	if pGetDpiForWindow.Find() == nil {
		if d, _, _ := pGetDpiForWindow.Call(hwnd); d != 0 {
			return int(d)
		}
	}
	hdc, _, _ := pGetDC.Call(0)
	d, _, _ := pGetDeviceCaps.Call(hdc, 88) // LOGPIXELSX
	pReleaseDC.Call(0, hdc)
	if d == 0 {
		return 96
	}
	return int(d)
}

// makeFont creates the UI font (Segoe UI 9 pt, like a native dialog).
func makeFont(dpi int, bold bool) uintptr {
	h := -((9*dpi + 36) / 72)
	weight := 400
	if bold {
		weight = 700
	}
	f, _, _ := pCreateFont.Call(
		uintptr(int32(h)), 0, 0, 0, uintptr(weight), 0, 0, 0,
		1,    // DEFAULT_CHARSET
		0, 0, // out/clip precision
		5, // CLEARTYPE_QUALITY
		0, // pitch/family
		ptr(utf16("Segoe UI")))
	return f
}

// outerSize converts a client size to a window size for the given styles.
func outerSize(style, exStyle uint32, w, h int32, dpi int) (int32, int32) {
	r := rect{0, 0, w, h}
	if pAdjustWindowRectD.Find() == nil {
		pAdjustWindowRectD.Call(uintptr(unsafe.Pointer(&r)), uintptr(style), 0, uintptr(exStyle), uintptr(dpi))
	} else {
		pAdjustWindowRect.Call(uintptr(unsafe.Pointer(&r)), uintptr(style), 0, uintptr(exStyle))
	}
	return r.Right - r.Left, r.Bottom - r.Top
}

func registerClass(name string, proc uintptr, bg uintptr) {
	inst, _, _ := pGetModuleHandle.Call(0)
	cur, _, _ := pLoadCursor.Call(0, idcArrow)
	icon, _, _ := pLoadIcon.Call(inst, 1) // resource id 1 from the .syso, if linked
	wc := wndClassEx{
		WndProc:    proc,
		Instance:   inst,
		Icon:       icon,
		IconSm:     icon,
		Cursor:     cur,
		Background: bg,
		ClassName:  utf16(name),
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
}

func createWindow(ex uint32, class, title string, style uint32, x, y, w, h int32, parent uintptr, id uintptr) uintptr {
	inst, _, _ := pGetModuleHandle.Call(0)
	hwnd, _, _ := pCreateWindowEx.Call(
		uintptr(ex), ptr(utf16(class)), ptr(utf16(title)), uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, id, inst, 0)
	return hwnd
}

func lowWord(v uintptr) int  { return int(v & 0xffff) }
func highWord(v uintptr) int { return int((v >> 16) & 0xffff) }
