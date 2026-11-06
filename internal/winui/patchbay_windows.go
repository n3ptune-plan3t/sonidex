//go:build windows

package winui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"sonidex/backend"
)

// A small Helvum-style patchbay: apps on the left, the Sonidex output on the
// right. Drag from an app's port to the Sonidex port (or click the port) to
// link it; click a cable (or the port again) to unlink. Drawn with plain GDI
// into an off-screen bitmap, repainted only when something changes.

const (
	pbRowH     = 46
	pbRowGap   = 8
	pbPad      = 14
	pbPortR    = 7
	pbSinkH    = 74
	pbMaxNodeW = 320
	pbHintH    = 44
)

type pbNode struct {
	pid    uint32
	name   string
	detail string
	peak   float32
	gone   bool // linked, but no audio session any more
}

type patchbay struct {
	hwnd     uintptr
	router   *backend.Router
	status   func(string)
	post     func(func())
	onClosed func()
	stop     chan struct{}

	dpi  int
	font uintptr
	bold uintptr

	mu    sync.Mutex
	nodes []pbNode
	names map[uint32]string

	down     bool
	downIdx  int
	downPos  fpoint
	dragging bool
	dragPID  uint32
	dragPos  fpoint
}

type fpoint struct{ x, y float64 }

var pbSelf *patchbay // one patchbay window at a time

func newPatchbay(r *backend.Router, status func(string), post func(func())) *patchbay {
	p := &patchbay{router: r, status: status, post: post, names: map[uint32]string{}, stop: make(chan struct{}), downIdx: -1}
	pbSelf = p
	cb := syscall.NewCallback(func(h, m, w, l uintptr) uintptr {
		if pbSelf != nil {
			return pbSelf.wndProc(h, m, w, l)
		}
		r, _, _ := pDefWindowProc.Call(h, m, w, l)
		return r
	})
	registerClass("SonidexPatchbay", cb, 0)
	style := uint32(wsOverlappedWn | wsClipChildren)
	p.hwnd = createWindow(0, "SonidexPatchbay", "Sonidex Patchbay", style, cwUseDefault, cwUseDefault, 760, 520, 0, 0)
	p.setDPI(dpiFor(p.hwnd))
	ow, oh := outerSize(style, 0, int32(720*p.dpi/96), int32(460*p.dpi/96), p.dpi)
	pSetWindowPos.Call(p.hwnd, 0, 0, 0, uintptr(ow), uintptr(oh), swpNoMove|swpNoZOrder)
	showWindow(p.hwnd, swShow)
	p.refreshSessions()
	go func() {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-t.C:
				p.refreshSessions()
			}
		}
	}()
	return p
}

func (p *patchbay) setDPI(dpi int) {
	p.dpi = dpi
	for _, f := range []uintptr{p.font, p.bold} {
		if f != 0 {
			pDeleteObject.Call(f)
		}
	}
	p.font, p.bold = makeFont(dpi, false), makeFont(dpi, true)
}

func (p *patchbay) sc(v float64) float64 { return v * float64(p.dpi) / 96 }

// refreshSessions re-reads the audio sessions and asks for a repaint.
func (p *patchbay) refreshSessions() {
	sessions, err := backend.ListAudioSessions()
	if err != nil && p.status != nil {
		p.status(err.Error())
	}
	nodes := []pbNode{{pid: backend.SystemPID, name: "System (all apps)", detail: "default output mix"}}
	seen := map[uint32]bool{}
	sort.Slice(sessions, func(i, j int) bool {
		return strings.ToLower(sessions[i].Name) < strings.ToLower(sessions[j].Name)
	})
	p.mu.Lock()
	for _, s := range sessions {
		name := strings.TrimSuffix(s.Name, ".exe")
		if name == "" {
			name = fmt.Sprintf("pid %d", s.PID)
		}
		p.names[s.PID] = name
		seen[s.PID] = true
		d := fmt.Sprintf("pid %d", s.PID)
		if !s.Active {
			d += " · idle"
		}
		nodes = append(nodes, pbNode{pid: s.PID, name: name, detail: d, peak: s.Peak})
	}
	for _, pid := range p.router.Linked() {
		if pid != backend.SystemPID && !seen[pid] {
			n := p.names[pid]
			if n == "" {
				n = fmt.Sprintf("pid %d", pid)
			}
			nodes = append(nodes, pbNode{pid: pid, name: n, detail: "no active audio session", gone: true})
		}
	}
	p.nodes = nodes
	p.mu.Unlock()
	p.post(func() {
		if pbSelf == p {
			invalidate(p.hwnd)
		}
	})
}

// ---- geometry (canvas coordinates, below the hint text) ----

func (p *patchbay) nodeW(width float64) float64 {
	w := width/2 - p.sc(pbPad)*2
	return math.Min(w, p.sc(pbMaxNodeW))
}

func (p *patchbay) nodeRect(width float64, i int) (x, y, w float64) {
	return p.sc(pbPad), p.sc(pbPad) + float64(i)*p.sc(pbRowH+pbRowGap), p.nodeW(width)
}

func (p *patchbay) sinkRect(width float64) (x, y, w float64) {
	w = p.nodeW(width)
	return width - p.sc(pbPad) - w, p.sc(pbPad), w
}

func (p *patchbay) srcPort(width float64, i int) fpoint {
	x, y, w := p.nodeRect(width, i)
	return fpoint{x + w, y + p.sc(pbRowH)/2}
}

func (p *patchbay) sinkPort(width float64) fpoint {
	x, y, _ := p.sinkRect(width)
	return fpoint{x, y + p.sc(pbSinkH)/2}
}

func dist(a, b fpoint) float64 { return math.Hypot(a.x-b.x, a.y-b.y) }

func segDist(p, a, b fpoint) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return dist(p, a)
	}
	t := math.Max(0, math.Min(1, ((p.x-a.x)*dx+(p.y-a.y)*dy)/l2))
	return math.Hypot(p.x-(a.x+t*dx), p.y-(a.y+t*dy))
}

func (p *patchbay) clientWidth() float64 {
	var r rect
	pGetClientRect.Call(p.hwnd, uintptr(unsafe.Pointer(&r)))
	return float64(r.Right)
}

// canvasPos converts a client-area mouse position to canvas coordinates.
func (p *patchbay) canvasPos(lp uintptr) fpoint {
	return fpoint{float64(int16(lp & 0xffff)), float64(int16((lp>>16)&0xffff)) - p.sc(pbHintH)}
}

func (p *patchbay) portAt(pos fpoint, width float64) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.nodes {
		if dist(pos, p.srcPort(width, i)) <= p.sc(pbPortR+6) {
			return i
		}
	}
	return -1
}

func (p *patchbay) toggle(pid uint32, wantLinked bool) {
	go func() {
		if wantLinked {
			if err := p.router.Link(pid); err != nil && p.status != nil {
				p.status("Link failed: " + err.Error())
			}
		} else {
			p.router.Unlink(pid)
		}
		p.refreshSessions()
	}()
}

// ---- input ----

func (p *patchbay) mouse(m uint32, lp uintptr) {
	width := p.clientWidth()
	pos := p.canvasPos(lp)
	switch m {
	case wmLButtonDown:
		pSetCapture.Call(p.hwnd)
		p.down, p.dragging = true, false
		p.downPos = pos
		p.downIdx = p.portAt(pos, width)
	case wmMouseMove:
		if !p.down {
			return
		}
		if p.downIdx >= 0 && !p.dragging && dist(pos, p.downPos) > p.sc(4) {
			p.mu.Lock()
			if p.downIdx < len(p.nodes) {
				p.dragPID = p.nodes[p.downIdx].pid
				p.dragging = true
			}
			p.mu.Unlock()
		}
		if p.dragging {
			p.dragPos = pos
			invalidate(p.hwnd)
		}
	case wmLButtonUp:
		if !p.down {
			return
		}
		pReleaseCapture.Call()
		p.down = false
		if p.dragging {
			p.dragging = false
			if dist(p.dragPos, p.sinkPort(width)) <= p.sc(pbPortR+14) {
				p.toggle(p.dragPID, true)
			}
			invalidate(p.hwnd)
			return
		}
		if p.downIdx >= 0 { // click on a port
			p.mu.Lock()
			var pid uint32
			ok := p.downIdx < len(p.nodes)
			if ok {
				pid = p.nodes[p.downIdx].pid
			}
			p.mu.Unlock()
			if ok {
				p.toggle(pid, !p.router.IsLinked(pid))
			}
			return
		}
		// click on a cable removes it
		sink := p.sinkPort(width)
		p.mu.Lock()
		var hit *uint32
		for i := range p.nodes {
			if p.router.IsLinked(p.nodes[i].pid) && segDist(pos, p.srcPort(width, i), sink) < p.sc(8) {
				pid := p.nodes[i].pid
				hit = &pid
				break
			}
		}
		p.mu.Unlock()
		if hit != nil {
			p.toggle(*hit, false)
		}
	}
}

// ---- drawing ----

type gdi struct{ hdc uintptr }

func (g gdi) brush(c uint32) uintptr { b, _, _ := pCreateSolidBrush.Call(uintptr(c)); return b }
func (g gdi) pen(w float64, c uint32) uintptr {
	pn, _, _ := pCreatePen.Call(psSolid, uintptr(int32(math.Max(1, math.Round(w)))), uintptr(c))
	return pn
}

// with selects pen and brush, runs fn, then restores and frees them.
func (g gdi) with(pen, brush uintptr, fn func()) {
	op, _, _ := pSelectObject.Call(g.hdc, pen)
	ob, _, _ := pSelectObject.Call(g.hdc, brush)
	fn()
	pSelectObject.Call(g.hdc, op)
	pSelectObject.Call(g.hdc, ob)
	pDeleteObject.Call(pen)
	pDeleteObject.Call(brush)
}

func (g gdi) text(x, y float64, s string, color uint32) {
	pSetTextColor.Call(g.hdc, uintptr(color))
	u := syscall.StringToUTF16(s)
	pTextOut.Call(g.hdc, uintptr(int32(x)), uintptr(int32(y)), uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1))
}

func (p *patchbay) render(hdc uintptr, w, h int32) {
	g := gdi{hdc}
	whole := rect{0, 0, w, h}
	bgBrush, _, _ := pGetSysColorBrush.Call(colorWindow)
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&whole)), bgBrush)
	pSetBkMode.Call(hdc, bkTransp)

	textC := sysColor(colorWindowText)
	idleC := sysColor(colorGrayText)
	accent := sysColor(colorHighlight)
	nodeFill := sysColor(colorBtnFace)
	width := float64(w)

	// hint text
	pSelectObject.Call(hdc, p.font)
	hint := syscall.StringToUTF16("Drag from an app's port to Sonidex (or click the port). Click a cable to remove it.")
	hr := rect{int32(p.sc(pbPad)), int32(p.sc(8)), w - int32(p.sc(pbPad)), int32(p.sc(pbHintH))}
	pSetTextColor.Call(hdc, uintptr(idleC))
	pDrawText.Call(hdc, uintptr(unsafe.Pointer(&hint[0])), ^uintptr(0), uintptr(unsafe.Pointer(&hr)), dtWordBreak|dtNoPrefix)

	off := p.sc(pbHintH)
	oy := func(y float64) float64 { return y + off }

	p.mu.Lock()
	nodes := append([]pbNode(nil), p.nodes...)
	dragging, dragPID, dragPos := p.dragging, p.dragPID, p.dragPos
	p.mu.Unlock()
	linked := p.router.Linked()
	isLinked := map[uint32]bool{}
	for _, pid := range linked {
		isLinked[pid] = true
	}

	// sink node
	sx, sy, sw := p.sinkRect(width)
	g.with(g.pen(2*p.dpiF(), accent), g.brush(nodeFill), func() {
		pRoundRect.Call(hdc, uintptr(int32(sx)), uintptr(int32(oy(sy))), uintptr(int32(sx+sw)), uintptr(int32(oy(sy+p.sc(pbSinkH)))), uintptr(int32(p.sc(12))), uintptr(int32(p.sc(12))))
	})
	pSelectObject.Call(hdc, p.bold)
	g.text(sx+p.sc(16), oy(sy+p.sc(14)), "Sonidex → Phone", textC)
	pSelectObject.Call(hdc, p.font)
	g.text(sx+p.sc(16), oy(sy+p.sc(40)), fmt.Sprintf("%d source(s) linked", len(linked)), idleC)
	sink := p.sinkPort(width)
	sink.y = oy(sink.y)

	// cables first, so nodes draw over their ends
	for i, nd := range nodes {
		if !isLinked[nd.pid] {
			continue
		}
		pp := p.srcPort(width, i)
		pp.y = oy(pp.y)
		c := accent
		if nd.gone {
			c = idleC
		}
		g.with(g.pen(3*p.dpiF(), c), g.brush(c), func() {
			pMoveToEx.Call(hdc, uintptr(int32(pp.x)), uintptr(int32(pp.y)), 0)
			pLineTo.Call(hdc, uintptr(int32(sink.x)), uintptr(int32(sink.y)))
		})
	}

	for i, nd := range nodes {
		x, y, nw := p.nodeRect(width, i)
		y = oy(y)
		lk := isLinked[nd.pid]
		penW, penC := p.dpiF(), idleC
		if lk {
			penW, penC = 2*p.dpiF(), accent
		}
		g.with(g.pen(penW, penC), g.brush(nodeFill), func() {
			pRoundRect.Call(hdc, uintptr(int32(x)), uintptr(int32(y)), uintptr(int32(x+nw)), uintptr(int32(y+p.sc(pbRowH))), uintptr(int32(p.sc(12))), uintptr(int32(p.sc(12))))
		})
		if nd.peak > 0 { // level meter along the bottom edge
			mw := (nw - p.sc(16)) * math.Min(1, float64(nd.peak))
			mr := rect{int32(x + p.sc(8)), int32(y + p.sc(pbRowH) - p.sc(6)), int32(x + p.sc(8) + mw), int32(y + p.sc(pbRowH) - p.sc(3))}
			mb := g.brush(rgb(0x3d, 0xd6, 0x8c))
			pFillRect.Call(hdc, uintptr(unsafe.Pointer(&mr)), mb)
			pDeleteObject.Call(mb)
		}
		pSelectObject.Call(hdc, p.bold)
		g.text(x+p.sc(10), y+p.sc(4), nd.name, textC)
		pSelectObject.Call(hdc, p.font)
		g.text(x+p.sc(10), y+p.sc(23), nd.detail, idleC)

		pp := p.srcPort(width, i)
		pp.y = oy(pp.y)
		fill := idleC
		if lk {
			fill = accent
		}
		r := p.sc(pbPortR)
		g.with(g.pen(p.dpiF(), textC), g.brush(fill), func() {
			pEllipse.Call(hdc, uintptr(int32(pp.x-r)), uintptr(int32(pp.y-r)), uintptr(int32(pp.x+r)), uintptr(int32(pp.y+r)))
		})
	}
	r := p.sc(pbPortR)
	g.with(g.pen(p.dpiF(), textC), g.brush(accent), func() {
		pEllipse.Call(hdc, uintptr(int32(sink.x-r)), uintptr(int32(sink.y-r)), uintptr(int32(sink.x+r)), uintptr(int32(sink.y+r)))
	})

	if dragging { // rubber-band cable while dragging
		for i, nd := range nodes {
			if nd.pid != dragPID {
				continue
			}
			pp := p.srcPort(width, i)
			pp.y = oy(pp.y)
			g.with(g.pen(2*p.dpiF(), accent), g.brush(accent), func() {
				pMoveToEx.Call(hdc, uintptr(int32(pp.x)), uintptr(int32(pp.y)), 0)
				pLineTo.Call(hdc, uintptr(int32(dragPos.x)), uintptr(int32(oy(dragPos.y))))
			})
		}
	}
}

func (p *patchbay) dpiF() float64 { return float64(p.dpi) / 96 }

func (p *patchbay) paint() {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(p.hwnd, uintptr(unsafe.Pointer(&ps)))
	var rc rect
	pGetClientRect.Call(p.hwnd, uintptr(unsafe.Pointer(&rc)))
	w, h := rc.Right, rc.Bottom
	mem, _, _ := pCreateCompatDC.Call(hdc)
	bmp, _, _ := pCreateCompatBmp.Call(hdc, uintptr(w), uintptr(h))
	old, _, _ := pSelectObject.Call(mem, bmp)
	p.render(mem, w, h)
	pBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), mem, 0, 0, srccopy)
	pSelectObject.Call(mem, old)
	pDeleteObject.Call(bmp)
	pDeleteDC.Call(mem)
	pEndPaint.Call(p.hwnd, uintptr(unsafe.Pointer(&ps)))
}

func (p *patchbay) wndProc(hwnd, m, wp, lp uintptr) uintptr {
	switch uint32(m) {
	case wmPaint:
		p.paint()
		return 0
	case wmEraseBkgnd:
		return 1 // everything is painted off-screen
	case wmSize:
		invalidate(hwnd)
		return 0
	case wmLButtonDown, wmLButtonUp, wmMouseMove:
		p.mouse(uint32(m), lp)
		return 0
	case wmDpiChanged:
		p.setDPI(highWord(wp))
		invalidate(hwnd)
		return 0
	case wmDestroy:
		close(p.stop)
		pbSelf = nil
		for _, f := range []uintptr{p.font, p.bold} {
			pDeleteObject.Call(f)
		}
		if p.onClosed != nil {
			p.post(p.onClosed)
		}
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, m, wp, lp)
	return r
}
