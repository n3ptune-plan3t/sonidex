//go:build windows

package winui

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"sonidex/backend"
	"sonidex/internal/core"
)

const cwUseDefault int32 = -0x80000000

const (
	idMode = 101 + iota
	idWirelessEdit
	idConnect
	idDirectEdit
	idDevice
	idRefresh
	idPort
	idRoute
	idProfile
	idSilence
	idStart

	contentW = 380 // logical px
	marginPx = 16
)

// item is one control in the vertical stack.
type item struct {
	hwnd    uintptr
	h       int // logical height of the (closed) control
	gap     int // logical gap above
	listH   int // logical dropdown height for comboboxes (0 = same as h)
	visible bool
	bold    bool
}

type ui struct {
	hwnd  uintptr
	dpi   int
	font  uintptr
	bold  uintptr
	items []*item

	mode, wEdit, dEdit, dev, port, prof, silence, status, start *item
	wireless, direct, devices                                   []*item
	route                                                       *item

	sess   *core.Session
	prefs  *core.Prefs
	router *backend.Router
	pb     *patchbay

	mu    sync.Mutex
	queue []func()
}

var theUI *ui

func (u *ui) s(v int) int32 { return int32(v * u.dpi / 96) }

// post queues fn for the UI thread (status updates arrive on goroutines).
func (u *ui) post(fn func()) {
	u.mu.Lock()
	u.queue = append(u.queue, fn)
	u.mu.Unlock()
	postMessage(u.hwnd, wmApp, 0, 0)
}

func (u *ui) drain() {
	u.mu.Lock()
	q := u.queue
	u.queue = nil
	u.mu.Unlock()
	for _, fn := range q {
		fn()
	}
}

func (u *ui) add(class, text string, style, ex uint32, id int, h, gap int) *item {
	hwnd := createWindow(ex, class, text, style|wsChild|wsVisible, 0, 0, 10, 10, u.hwnd, uintptr(id))
	it := &item{hwnd: hwnd, h: h, gap: gap, visible: true}
	sendMessage(hwnd, wmSetFont, u.font, 1)
	u.items = append(u.items, it)
	return it
}

func (u *ui) label(text string, gap int) *item { return u.add("STATIC", text, 0, 0, 0, 18, gap) }

func (u *ui) edit(id int, gap int) *item {
	return u.add("EDIT", "", esAutoHScroll|wsTabStop, wsExClientEdge, id, 24, gap)
}

func (u *ui) button(text string, id int, gap int) *item {
	return u.add("BUTTON", text, bsPushButton|wsTabStop, 0, id, 28, gap)
}

func (u *ui) combo(id int, gap int) *item {
	it := u.add("COMBOBOX", "", cbsDropdownLst|wsVScroll|wsTabStop, 0, id, 24, gap)
	it.listH = 180
	return it
}

func cue(it *item, text string) {
	p := utf16(text)
	sendMessage(it.hwnd, emSetCueBanner, 1, ptr(p))
	runtime.KeepAlive(p)
}

func fillCombo(it *item, opts []string, sel int) {
	sendMessage(it.hwnd, cbResetContent, 0, 0)
	for _, o := range opts {
		p := utf16(o)
		sendMessage(it.hwnd, cbAddString, 0, ptr(p))
		runtime.KeepAlive(p)
	}
	sendMessage(it.hwnd, cbSetCurSel, uintptr(sel), 0) // -1 clears
}

func comboText(it *item) string {
	i := int32(sendMessage(it.hwnd, cbGetCurSel, 0, 0))
	if i < 0 {
		return ""
	}
	n := int32(sendMessage(it.hwnd, cbGetLBTextLen, uintptr(i), 0))
	if n <= 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	sendMessage(it.hwnd, cbGetLBText, uintptr(i), uintptr(unsafe.Pointer(&buf[0])))
	return syscall.UTF16ToString(buf)
}

func show(its []*item, v bool) {
	for _, it := range its {
		it.visible = v
		if v {
			showWindow(it.hwnd, swShow)
		} else {
			showWindow(it.hwnd, swHide)
		}
	}
}

// layout stacks the visible controls and resizes the window to fit.
func (u *ui) layout() {
	m := u.s(marginPx)
	w := u.s(contentW)
	y := m
	first := true
	for _, it := range u.items {
		if !it.visible {
			continue
		}
		if !first {
			y += u.s(it.gap)
		}
		first = false
		h := u.s(it.h)
		drawH := h
		if it.listH > 0 {
			drawH = h + u.s(it.listH) // closed height + dropdown list height
		}
		pMoveWindow.Call(it.hwnd, uintptr(m), uintptr(y), uintptr(w), uintptr(drawH), 1)
		y += h
	}
	y += m
	style := uint32(wsCaption | wsSysMenu | wsMinimizeBox)
	ow, oh := outerSize(style, wsExControlParent, w+2*m, y, u.dpi)
	pSetWindowPos.Call(u.hwnd, 0, 0, 0, uintptr(ow), uintptr(oh), swpNoMove|swpNoZOrder|swpNoActivate)
}

func (u *ui) setFonts() {
	oldF, oldB := u.font, u.bold
	u.font, u.bold = makeFont(u.dpi, false), makeFont(u.dpi, true)
	for _, it := range u.items {
		f := u.font
		if it.bold {
			f = u.bold
		}
		sendMessage(it.hwnd, wmSetFont, f, 1)
	}
	for _, f := range []uintptr{oldF, oldB} {
		if f != 0 {
			pDeleteObject.Call(f)
		}
	}
}

func (u *ui) setStatus(s string) { setText(u.status.hwnd, s) }

func (u *ui) params() core.Params {
	return core.Params{
		Mode:     comboText(u.mode),
		Serial:   comboText(u.dev),
		Port:     getText(u.port.hwnd),
		DirectIP: getText(u.dEdit.hwnd),
		Profile:  comboText(u.prof),
		Silence:  sendMessage(u.silence.hwnd, bmGetCheck, 0, 0) == 1,
	}
}

func (u *ui) onMode() {
	mode := comboText(u.mode)
	show(u.wireless, mode == core.ModeWirelessADB)
	show(u.direct, mode == core.ModeWiFiDirect)
	show(u.devices, mode != core.ModeWiFiDirect)
	u.layout()
}

func (u *ui) refreshDevices() {
	go func() {
		devs, msg := core.RefreshDevices()
		u.post(func() {
			sel := -1
			if len(devs) > 0 {
				sel = 0
			}
			fillCombo(u.dev, devs, sel)
			u.setStatus(msg)
		})
	}()
}

func (u *ui) connectWireless() {
	addr := getText(u.wEdit.hwnd)
	go func() {
		msg, ok := core.ConnectWireless(addr)
		u.post(func() { u.setStatus(msg) })
		if ok {
			u.refreshDevices()
		}
	}()
}

func (u *ui) onCommand(wp uintptr) {
	id, code := lowWord(wp), highWord(wp)
	switch id {
	case idMode:
		if code == cbnSelChange {
			u.onMode()
		}
	case idProfile:
		if code == cbnSelChange {
			u.prefs.Profile = comboText(u.prof)
			u.prefs.Save()
		}
	case idSilence:
		u.prefs.SilenceSuppress = sendMessage(u.silence.hwnd, bmGetCheck, 0, 0) == 1
		u.prefs.Save()
	case idRefresh:
		u.refreshDevices()
	case idConnect:
		u.connectWireless()
	case idStart:
		p := u.params()
		go u.sess.Toggle(p) // may run adb; keep the UI thread free
	case idRoute:
		u.openPatchbay()
	}
}

func (u *ui) openPatchbay() {
	if u.pb != nil {
		pSetForeground.Call(u.pb.hwnd)
		return
	}
	u.pb = newPatchbay(u.router, func(s string) { u.post(func() { u.setStatus(s) }) }, u.post)
	u.pb.onClosed = func() { u.pb = nil }
}

func (u *ui) wndProc(hwnd, m, wp, lp uintptr) uintptr {
	switch uint32(m) {
	case wmCommand:
		u.onCommand(wp)
		return 0
	case wmApp:
		u.drain()
		return 0
	case wmDpiChanged:
		u.dpi = highWord(wp)
		u.setFonts()
		u.layout()
		return 0
	case wmDestroy:
		u.sess.Stop()
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, m, wp, lp)
	return r
}

func newUI() *ui {
	u := &ui{prefs: core.LoadPrefs(), router: backend.NewPlatformRouter()}
	theUI = u
	u.sess = &core.Session{Router: u.router}
	u.sess.OnStatus = func(s string) { u.post(func() { u.setStatus(s) }) }
	u.sess.OnRunning = func(r bool) {
		u.post(func() {
			if r {
				setText(u.start.hwnd, "Stop Streaming")
			} else {
				setText(u.start.hwnd, "Start Streaming")
			}
		})
	}
	if u.router != nil {
		_ = u.router.Link(backend.SystemPID) // default: stream everything
		u.router.SetNotify(u.sess.OnStatus)
	}

	cb := syscall.NewCallback(func(h, m, w, l uintptr) uintptr {
		if theUI != nil {
			return theUI.wndProc(h, m, w, l)
		}
		r, _, _ := pDefWindowProc.Call(h, m, w, l)
		return r
	})
	registerClass("SonidexMain", cb, colorBtnFace+1)

	style := uint32(wsCaption | wsSysMenu | wsMinimizeBox | wsClipChildren)
	u.hwnd = createWindow(wsExControlParent, "SonidexMain", "Sonidex Streamer", style,
		cwUseDefault, cwUseDefault, 500, 600, 0, 0)
	u.dpi = dpiFor(u.hwnd)
	u.font, u.bold = makeFont(u.dpi, false), makeFont(u.dpi, true)

	t := u.add("STATIC", "Sonidex Streamer", ssCenter, 0, 0, 20, 0)
	t.bold = true
	sendMessage(t.hwnd, wmSetFont, u.bold, 1)

	u.label("Connection Mode:", 10)
	u.mode = u.combo(idMode, 2)
	fillCombo(u.mode, core.Modes, 0)

	w1 := u.label("Device ip:port (Wireless debugging):", 8)
	u.wEdit = u.edit(idWirelessEdit, 2)
	cue(u.wEdit, "192.168.1.42:5555")
	w3 := u.button("Connect", idConnect, 4)
	u.wireless = []*item{w1, u.wEdit, w3}

	d1 := u.label("Phone IP address (same WiFi network):", 8)
	u.dEdit = u.edit(idDirectEdit, 2)
	cue(u.dEdit, "192.168.1.42")
	u.direct = []*item{d1, u.dEdit}

	a1 := u.label("Android Target Device:", 8)
	u.dev = u.combo(idDevice, 2)
	a3 := u.button("Refresh Devices", idRefresh, 4)
	u.devices = []*item{a1, u.dev, a3}

	u.label("Port:", 8)
	u.port = u.edit(idPort, 2)
	setText(u.port.hwnd, core.DefaultPort)

	if u.router != nil {
		u.route = u.button("Audio routing (patchbay)…", idRoute, 10)
	}

	u.label("Latency / power profile:", 10)
	u.prof = u.combo(idProfile, 2)
	fillCombo(u.prof, backend.ProfileNames, int(backend.ProfileFromName(u.prefs.Profile)))

	u.silence = u.add("BUTTON", "Pause sending while silent (saves power)", bsAutoCheckbox|wsTabStop, 0, idSilence, 20, 8)
	if u.prefs.SilenceSuppress {
		sendMessage(u.silence.hwnd, bmSetCheck, 1, 0)
	}

	u.status = u.add("STATIC", "Ready", 0, 0, 0, 34, 10)
	u.start = u.button("Start Streaming", idStart, 6)

	show(u.wireless, false)
	show(u.direct, false)
	u.layout()
	showWindow(u.hwnd, swShow)
	u.refreshDevices()
	return u
}

// Run opens the streamer window and blocks until it is closed.
func Run() error {
	runtime.LockOSThread()
	pSetProcessDPIAw.Call() // no-op when the manifest already declares DPI awareness
	pInitCommonCtrls.Call(uintptr(unsafe.Pointer(&initCommonControlsEx{Size: 8, ICC: 0xFF})))
	u := newUI()

	var m msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if h, _, _ := pIsDialogMessage.Call(u.hwnd, uintptr(unsafe.Pointer(&m))); h != 0 {
			continue // Tab / arrow navigation between controls
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}
