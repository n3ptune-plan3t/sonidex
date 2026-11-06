// Package x11 is a mini.Platform for X11 written against the core protocol
// with a pure-Go client (no Xlib, no GTK/Qt). Frames are pushed with
// PutImage only when something changed, so an idle window costs nothing.
package x11

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	"sonidex/internal/mini"
)

type Platform struct {
	c      *xgb.Conn
	setup  *xproto.SetupInfo
	screen *xproto.ScreenInfo
	scale  float64
	wake   chan struct{}

	keysyms []xproto.Keysym
	per     int
	minKC   xproto.Keycode
}

// New connects to $DISPLAY.
func New() (*Platform, error) {
	c, err := xgb.NewConn()
	if err != nil {
		return nil, err
	}
	p := &Platform{c: c, wake: make(chan struct{}, 1)}
	p.setup = xproto.Setup(c)
	p.screen = p.setup.DefaultScreen(c)

	bpp := 0
	for _, f := range p.setup.PixmapFormats {
		if f.Depth == p.screen.RootDepth {
			bpp = int(f.BitsPerPixel)
		}
	}
	if bpp != 32 {
		c.Close()
		return nil, fmt.Errorf("x11: unsupported pixel format (depth %d, %d bpp)", p.screen.RootDepth, bpp)
	}
	p.scale = p.detectScale()
	p.loadKeymap()
	return p, nil
}

func (p *Platform) Scale() float64 { return p.scale }

func (p *Platform) Wake() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// detectScale honours SONIDEX_SCALE, then Xft.dpi from RESOURCE_MANAGER.
func (p *Platform) detectScale() float64 {
	if v, err := strconv.ParseFloat(os.Getenv("SONIDEX_SCALE"), 64); err == nil && v >= 0.5 && v <= 4 {
		return v
	}
	rep, err := xproto.GetProperty(p.c, false, p.screen.Root, xproto.AtomResourceManager, xproto.AtomString, 0, 16384).Reply()
	if err != nil || rep == nil {
		return 1
	}
	for _, line := range strings.Split(string(rep.Value), "\n") {
		if rest, ok := strings.CutPrefix(line, "Xft.dpi:"); ok {
			if dpi, err := strconv.ParseFloat(strings.TrimSpace(rest), 64); err == nil && dpi > 0 {
				s := float64(int(dpi/96*4+0.5)) / 4 // quarter steps
				if s < 1 {
					s = 1
				}
				if s > 4 {
					s = 4
				}
				return s
			}
		}
	}
	return 1
}

func (p *Platform) loadKeymap() {
	n := int(p.setup.MaxKeycode) - int(p.setup.MinKeycode) + 1
	rep, err := xproto.GetKeyboardMapping(p.c, p.setup.MinKeycode, byte(n)).Reply()
	if err != nil || rep == nil {
		return
	}
	p.keysyms, p.per, p.minKC = rep.Keysyms, int(rep.KeysymsPerKeycode), p.setup.MinKeycode
}

func (p *Platform) keysym(kc xproto.Keycode, col int) xproto.Keysym {
	if p.per == 0 || kc < p.minKC || col >= p.per {
		return 0
	}
	i := (int(kc)-int(p.minKC))*p.per + col
	if i >= len(p.keysyms) {
		return 0
	}
	return p.keysyms[i]
}

func (p *Platform) atom(name string) xproto.Atom {
	r, err := xproto.InternAtom(p.c, false, uint16(len(name)), name).Reply()
	if err != nil || r == nil {
		return 0
	}
	return r.Atom
}

func le32(vals ...uint32) []byte {
	b := make([]byte, 4*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint32(b[4*i:], v)
	}
	return b
}

func (p *Platform) setProp(win xproto.Window, prop, typ xproto.Atom, format byte, data []byte) {
	n := uint32(len(data))
	if format == 32 {
		n /= 4
	}
	_ = xproto.ChangePropertyChecked(p.c, xproto.PropModeReplace, win, prop, typ, format, n, data).Check()
}

func (p *Platform) Run(opt mini.WindowOptions, sink mini.Sink) error {
	defer p.c.Close()
	w, h := opt.W, opt.H

	win, err := xproto.NewWindowId(p.c)
	if err != nil {
		return err
	}
	mask := uint32(xproto.CwBackPixel | xproto.CwEventMask)
	events := uint32(xproto.EventMaskExposure | xproto.EventMaskStructureNotify |
		xproto.EventMaskKeyPress | xproto.EventMaskButtonPress | xproto.EventMaskButtonRelease |
		xproto.EventMaskPointerMotion | xproto.EventMaskLeaveWindow | xproto.EventMaskFocusChange |
		xproto.EventMaskKeymapState)
	if err := xproto.CreateWindowChecked(p.c, p.screen.RootDepth, win, p.screen.Root,
		0, 0, uint16(w), uint16(h), 0, xproto.WindowClassInputOutput, p.screen.RootVisual,
		mask, []uint32{p.screen.BlackPixel, events}).Check(); err != nil {
		return err
	}

	p.setProp(win, xproto.AtomWmName, xproto.AtomString, 8, []byte(opt.Title))
	p.setProp(win, p.atom("_NET_WM_NAME"), p.atom("UTF8_STRING"), 8, []byte(opt.Title))
	p.setProp(win, xproto.AtomWmClass, xproto.AtomString, 8, []byte(opt.AppID+"\x00"+capitalize(opt.AppID)+"\x00"))
	hints := make([]byte, 72)                      // WM_SIZE_HINTS: flags, 4 legacy words, min w/h, ...
	binary.LittleEndian.PutUint32(hints[0:], 1<<4) // PMinSize
	binary.LittleEndian.PutUint32(hints[20:], uint32(opt.MinW))
	binary.LittleEndian.PutUint32(hints[24:], uint32(opt.MinH))
	p.setProp(win, xproto.AtomWmNormalHints, xproto.AtomWmSizeHints, 32, hints)
	wmProto, wmDelete := p.atom("WM_PROTOCOLS"), p.atom("WM_DELETE_WINDOW")
	p.setProp(win, wmProto, xproto.AtomAtom, 32, le32(uint32(wmDelete)))

	gc, err := xproto.NewGcontextId(p.c)
	if err != nil {
		return err
	}
	if err := xproto.CreateGCChecked(p.c, gc, xproto.Drawable(win), 0, nil).Check(); err != nil {
		return err
	}
	if err := xproto.MapWindowChecked(p.c, win).Check(); err != nil {
		return err
	}

	// A goroutine does nothing but block on the socket and hand events over,
	// so the loop below can also select on Wake.
	evCh := make(chan xgb.Event)
	go func() {
		defer close(evCh)
		for {
			ev, xerr := p.c.WaitForEvent()
			if ev == nil && xerr == nil {
				return
			}
			if ev != nil {
				evCh <- ev
			}
		}
	}()

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var row []byte
	sink.Wake() // run anything queued before the window existed
	paint := true

	present := func() {
		sink.Draw(img)
		row = p.put(win, gc, img, row)
	}

	handle := func(ev xgb.Event) (redraw, quit bool) {
		switch e := ev.(type) {
		case xproto.ExposeEvent:
			return e.Count == 0, false
		case xproto.ConfigureNotifyEvent:
			nw, nh := int(e.Width), int(e.Height)
			if nw > 0 && nh > 0 && (nw != w || nh != h) {
				w, h = nw, nh
				img = image.NewRGBA(image.Rect(0, 0, w, h))
				return true, false
			}
		case xproto.ClientMessageEvent:
			if e.Type == wmProto && xproto.Atom(e.Data.Data32[0]) == wmDelete {
				return false, true
			}
		case xproto.ButtonPressEvent:
			if e.Detail == 1 {
				return sink.Pointer(mini.PointerEvent{Kind: mini.PointerDown, X: int(e.EventX), Y: int(e.EventY)}), false
			}
		case xproto.ButtonReleaseEvent:
			if e.Detail == 1 {
				return sink.Pointer(mini.PointerEvent{Kind: mini.PointerUp, X: int(e.EventX), Y: int(e.EventY)}), false
			}
		case xproto.MotionNotifyEvent:
			return sink.Pointer(mini.PointerEvent{Kind: mini.PointerMove, X: int(e.EventX), Y: int(e.EventY)}), false
		case xproto.LeaveNotifyEvent:
			return sink.Pointer(mini.PointerEvent{Kind: mini.PointerLeave}), false
		case xproto.FocusInEvent:
			return sink.Focus(true), false
		case xproto.FocusOutEvent:
			return sink.Focus(false), false
		case xproto.MappingNotifyEvent:
			p.loadKeymap()
		case xproto.KeyPressEvent:
			return sink.Key(p.keyEvent(e)), false
		}
		return false, false
	}

	for {
		select {
		case ev, ok := <-evCh:
			if !ok {
				return errors.New("x11: connection closed")
			}
			r, q := handle(ev)
			if q {
				return nil
			}
			paint = paint || r
		case <-p.wake:
			if sink.Wake() {
				paint = true
			}
		}
		// Batch whatever else is already queued into a single repaint.
	drain:
		for {
			select {
			case ev, ok := <-evCh:
				if !ok {
					return errors.New("x11: connection closed")
				}
				r, q := handle(ev)
				if q {
					return nil
				}
				paint = paint || r
			default:
				break drain
			}
		}
		if paint {
			paint = false
			present()
		}
	}
}

// put uploads img (RGBA) as BGRX rows, in requests small enough for the
// server's maximum request length.
func (p *Platform) put(win xproto.Window, gc xproto.Gcontext, img *image.RGBA, buf []byte) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	maxReq := int(p.setup.MaximumRequestLength)*4 - 64
	rows := maxReq / (w * 4)
	if rows < 1 {
		rows = 1
	}
	if rows > h {
		rows = h
	}
	if need := rows * w * 4; len(buf) < need {
		buf = make([]byte, need)
	}
	msb := p.setup.ImageByteOrder == xproto.ImageOrderMSBFirst
	for y0 := 0; y0 < h; y0 += rows {
		n := rows
		if y0+n > h {
			n = h - y0
		}
		dst := buf[:n*w*4]
		for y := 0; y < n; y++ {
			src := img.Pix[(y0+y)*img.Stride : (y0+y)*img.Stride+w*4]
			d := dst[y*w*4 : (y+1)*w*4]
			for i := 0; i < len(src); i += 4 {
				if msb {
					d[i], d[i+1], d[i+2], d[i+3] = 0, src[i], src[i+1], src[i+2]
				} else {
					d[i], d[i+1], d[i+2], d[i+3] = src[i+2], src[i+1], src[i], 0
				}
			}
		}
		xproto.PutImage(p.c, xproto.ImageFormatZPixmap, xproto.Drawable(win), gc,
			uint16(w), uint16(n), 0, int16(y0), 0, p.screen.RootDepth, dst)
	}
	return buf
}

// keyEvent turns a core-protocol key press into a mini.KeyEvent using the
// keyboard mapping (shift, caps lock and AltGr/level-3 columns).
func (p *Platform) keyEvent(e xproto.KeyPressEvent) mini.KeyEvent {
	shift := e.State&xproto.ModMaskShift != 0
	lock := e.State&xproto.ModMaskLock != 0
	altgr := e.State&xproto.ModMask5 != 0
	ctrl := e.State&xproto.ModMaskControl != 0
	alt := e.State&xproto.ModMask1 != 0

	base := 0
	if altgr && p.per >= 4 {
		base = 2
	}
	ks := p.keysym(e.Detail, base)
	if lock && unicode.IsLetter(rune(ks)) && ks < 0x100 {
		shift = !shift
	}
	if shift {
		if s := p.keysym(e.Detail, base+1); s != 0 {
			ks = s
		}
	}

	ev := mini.FromKeysym(uint32(ks))
	ev.Shift = ev.Shift || shift
	ev.Ctrl, ev.Alt = ctrl, alt
	return ev
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
