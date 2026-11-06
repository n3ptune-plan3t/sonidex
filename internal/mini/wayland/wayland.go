//go:build linux

// Package wayland is a mini.Platform for native Wayland sessions, written
// against the core protocol (wl_shm software buffers + xdg-shell) with a
// pure-Go client: no libwayland, GTK, Qt or GPU. Frames are drawn only when
// something changed and the loop sleeps in a channel select otherwise.
package wayland

import (
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rajveermalviya/go-wayland/wayland/client"
	"github.com/rajveermalviya/go-wayland/wayland/cursor"
	xdg "github.com/rajveermalviya/go-wayland/wayland/stable/xdg-shell"
	xdgdeco "github.com/rajveermalviya/go-wayland/wayland/unstable/xdg-decoration-v1"
	"golang.org/x/sys/unix"

	"sonidex/internal/mini"
)

// raw is one wire message handed from the reader goroutine to the loop, so
// that every protocol object is only ever touched from one goroutine.
type raw struct {
	sender, opcode uint32
	fd             int
	data           []byte
}

type shmBuf struct {
	buf  *client.Buffer
	data []byte
	busy bool
}

type Platform struct {
	display *client.Display
	ctx     *client.Context
	reg     *client.Registry
	msgs    chan raw
	wake    chan struct{}
	err     error

	compositor *client.Compositor
	shm        *client.Shm
	seat       *client.Seat
	wm         *xdg.WmBase
	deco       *xdgdeco.DecorationManager
	outputs    map[*client.Output]int32
	bs         int32 // buffer scale (integer output scale)
	uiScale    float64

	sink     mini.Sink
	surface  *client.Surface
	xsurf    *xdg.Surface
	top      *xdg.Toplevel
	minW     int32 // logical
	minH     int32
	lw, lh   int32 // logical window size
	pw, ph   int32 // pending size from the compositor
	cfg      bool  // received first configure
	closed   bool
	need     bool // repaint wanted
	pending  bool // wanted a repaint but every buffer was busy
	img      *image.RGBA
	bufs     [2]*shmBuf
	bufW     int
	bufH     int
	frameBuf []byte

	pointer      *client.Pointer
	keyboard     *client.Keyboard
	cursorTried  bool
	cursorTheme  *cursor.Theme
	cursorSurf   *client.Surface
	cursorHX     int32
	cursorHY     int32
	xkb          *xkb
	repRate      int32
	repDelay     int32
	repCode      uint32
	repEv        mini.KeyEvent
	repTimer     *time.Timer
	repC         <-chan time.Time
	lastX, lastY float64
}

// New connects to the compositor and binds the globals it needs.
func New() (*Platform, error) {
	display, err := client.Connect("")
	if err != nil {
		return nil, err
	}
	p := &Platform{
		display: display,
		ctx:     display.Context(),
		msgs:    make(chan raw, 64),
		wake:    make(chan struct{}, 1),
		outputs: map[*client.Output]int32{},
		bs:      1,
	}
	display.SetErrorHandler(func(e client.DisplayErrorEvent) {
		p.err = fmt.Errorf("wayland: protocol error %d: %s", e.Code, e.Message)
	})
	go p.readLoop()

	reg, err := display.GetRegistry()
	if err != nil {
		return nil, err
	}
	p.reg = reg
	var globals []client.RegistryGlobalEvent
	reg.SetGlobalHandler(func(e client.RegistryGlobalEvent) { globals = append(globals, e) })
	if err := p.roundtrip(); err != nil {
		return nil, err
	}

	for _, g := range globals {
		v := func(max uint32) uint32 { return min(g.Version, max) }
		switch g.Interface {
		case "wl_compositor":
			c := client.NewCompositor(p.ctx)
			if err := reg.Bind(g.Name, g.Interface, v(4), c); err != nil {
				return nil, err
			}
			p.compositor = c
		case "wl_shm":
			s := client.NewShm(p.ctx)
			if err := reg.Bind(g.Name, g.Interface, v(1), s); err != nil {
				return nil, err
			}
			p.shm = s
		case "wl_seat":
			s := client.NewSeat(p.ctx)
			if err := reg.Bind(g.Name, g.Interface, v(5), s); err != nil {
				return nil, err
			}
			p.seat = s
		case "xdg_wm_base":
			w := xdg.NewWmBase(p.ctx)
			if err := reg.Bind(g.Name, g.Interface, v(2), w); err != nil {
				return nil, err
			}
			p.wm = w
		case "zxdg_decoration_manager_v1":
			d := xdgdeco.NewDecorationManager(p.ctx)
			if err := reg.Bind(g.Name, g.Interface, v(1), d); err == nil {
				p.deco = d
			}
		case "wl_output":
			o := client.NewOutput(p.ctx)
			if err := reg.Bind(g.Name, g.Interface, v(3), o); err == nil {
				p.outputs[o] = 1
				o.SetScaleHandler(func(e client.OutputScaleEvent) { p.outputs[o] = e.Factor })
			}
		}
	}
	if p.compositor == nil || p.shm == nil || p.wm == nil {
		return nil, errors.New("wayland: compositor lacks wl_compositor, wl_shm or xdg_wm_base")
	}
	p.wm.SetPingHandler(func(e xdg.WmBasePingEvent) { _ = p.wm.Pong(e.Serial) })
	if p.seat != nil {
		p.seat.SetCapabilitiesHandler(p.onCapabilities)
	}
	if err := p.roundtrip(); err != nil { // output scales + seat capabilities
		return nil, err
	}
	for _, f := range p.outputs {
		if f > p.bs {
			p.bs = f
		}
	}
	p.uiScale = float64(p.bs)
	if v, err := strconv.ParseFloat(os.Getenv("SONIDEX_SCALE"), 64); err == nil && v >= 0.5 && v <= 4 {
		p.uiScale = v
	}
	if x, err := newXkb(); err == nil {
		p.xkb = x
	} else {
		fmt.Fprintln(os.Stderr, "sonidex: keyboard text input unavailable:", err)
	}
	return p, nil
}

func (p *Platform) Scale() float64 { return p.uiScale }

func (p *Platform) Wake() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Platform) readLoop() {
	for {
		s, op, fd, data, err := p.ctx.ReadMsg()
		if err != nil {
			close(p.msgs)
			return
		}
		p.msgs <- raw{s, op, fd, data}
	}
}

func (p *Platform) dispatch(m raw) {
	if d, ok := p.ctx.GetProxy(m.sender).(client.Dispatcher); ok {
		d.Dispatch(m.opcode, m.fd, m.data)
	} else if m.fd >= 0 {
		_ = unix.Close(m.fd)
	}
}

func (p *Platform) roundtrip() error {
	cb, err := p.display.Sync()
	if err != nil {
		return err
	}
	done := false
	cb.SetDoneHandler(func(client.CallbackDoneEvent) { done = true })
	for !done {
		m, ok := <-p.msgs
		if !ok {
			return errors.New("wayland: connection closed")
		}
		p.dispatch(m)
		if p.err != nil {
			return p.err
		}
	}
	return nil
}

func (p *Platform) onCapabilities(e client.SeatCapabilitiesEvent) {
	if e.Capabilities&1 != 0 && p.pointer == nil {
		if ptr, err := p.seat.GetPointer(); err == nil {
			p.pointer = ptr
			p.setupPointer(ptr)
		}
	}
	if e.Capabilities&2 != 0 && p.keyboard == nil {
		if kb, err := p.seat.GetKeyboard(); err == nil {
			p.keyboard = kb
			p.setupKeyboard(kb)
		}
	}
}

func (p *Platform) Run(opt mini.WindowOptions, sink mini.Sink) error {
	p.sink = sink
	bs := p.bs
	logical := func(v int) int32 { return int32((v + int(bs) - 1) / int(bs)) }
	p.lw, p.lh = logical(opt.W), logical(opt.H)
	p.minW, p.minH = logical(opt.MinW), logical(opt.MinH)

	var err error
	if p.surface, err = p.compositor.CreateSurface(); err != nil {
		return err
	}
	if p.xsurf, err = p.wm.GetXdgSurface(p.surface); err != nil {
		return err
	}
	if p.top, err = p.xsurf.GetToplevel(); err != nil {
		return err
	}
	_ = p.top.SetTitle(opt.Title)
	_ = p.top.SetAppId(opt.AppID)
	_ = p.top.SetMinSize(p.minW, p.minH)
	if bs > 1 {
		_ = p.surface.SetBufferScale(bs)
	}
	if p.deco != nil {
		if d, err := p.deco.GetToplevelDecoration(p.top); err == nil {
			_ = d.SetMode(uint32(xdgdeco.ToplevelDecorationModeServerSide))
		}
	}
	p.top.SetConfigureHandler(func(e xdg.ToplevelConfigureEvent) { p.pw, p.ph = e.Width, e.Height })
	p.top.SetCloseHandler(func(xdg.ToplevelCloseEvent) { p.closed = true })
	p.xsurf.SetConfigureHandler(func(e xdg.SurfaceConfigureEvent) {
		_ = p.xsurf.AckConfigure(e.Serial)
		if p.pw > 0 && p.ph > 0 {
			p.lw, p.lh = max(p.pw, p.minW), max(p.ph, p.minH)
		}
		p.cfg, p.need = true, true
	})
	if err := p.surface.Commit(); err != nil { // asks the compositor for the first configure
		return err
	}
	sink.Wake()
	p.need = true

	for {
		select {
		case m, ok := <-p.msgs:
			if !ok {
				return errors.New("wayland: connection closed")
			}
			p.dispatch(m)
		case <-p.wake:
			if sink.Wake() {
				p.need = true
			}
		case <-p.repC:
			p.fireKey(p.repEv)
			if p.repTimer != nil && p.repRate > 0 {
				p.repTimer.Reset(time.Second / time.Duration(p.repRate))
			}
		}
	drain:
		for {
			select {
			case m, ok := <-p.msgs:
				if !ok {
					return errors.New("wayland: connection closed")
				}
				p.dispatch(m)
			default:
				break drain
			}
		}
		if p.err != nil {
			return p.err
		}
		if p.closed {
			return nil
		}
		if p.need && p.cfg {
			p.paint()
		}
	}
}

// ---- drawing ----

func (p *Platform) freeBuffers() {
	for i, b := range p.bufs {
		if b != nil {
			_ = b.buf.Destroy()
			p.bufs[i] = nil
		}
	}
	if len(p.frameBuf) > 0 {
		_ = unix.Munmap(p.frameBuf)
		p.frameBuf = nil
	}
}

// ensureBuffers (re)creates two shm buffers of pw×ph pixels in one pool.
func (p *Platform) ensureBuffers(pw, ph int) error {
	if p.bufs[0] != nil && p.bufW == pw && p.bufH == ph {
		return nil
	}
	p.freeBuffers()
	stride := pw * 4
	size := stride * ph * 2
	fd, err := unix.MemfdCreate("sonidex-shm", unix.MFD_CLOEXEC)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := unix.Ftruncate(fd, int64(size)); err != nil {
		return err
	}
	data, err := unix.Mmap(fd, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return err
	}
	pool, err := p.shm.CreatePool(fd, int32(size))
	if err != nil {
		_ = unix.Munmap(data)
		return err
	}
	p.frameBuf = data
	for i := range p.bufs {
		off := i * stride * ph
		b, err := pool.CreateBuffer(int32(off), int32(pw), int32(ph), int32(stride), 1) // XRGB8888
		if err != nil {
			return err
		}
		sb := &shmBuf{buf: b, data: data[off : off+stride*ph]}
		b.SetReleaseHandler(func(client.BufferReleaseEvent) {
			sb.busy = false
			if p.pending {
				p.need = true
			}
		})
		p.bufs[i] = sb
	}
	_ = pool.Destroy() // buffers keep the memory alive
	p.bufW, p.bufH = pw, ph
	p.img = image.NewRGBA(image.Rect(0, 0, pw, ph))
	return nil
}

func (p *Platform) paint() {
	pw, ph := int(p.lw*p.bs), int(p.lh*p.bs)
	if err := p.ensureBuffers(pw, ph); err != nil {
		p.err = err
		return
	}
	var b *shmBuf
	for _, c := range p.bufs {
		if !c.busy {
			b = c
			break
		}
	}
	if b == nil {
		p.pending = true // retried when a buffer is released
		return
	}
	p.pending, p.need = false, false
	p.sink.Draw(p.img)
	src, dst := p.img.Pix, b.data
	for i := 0; i+3 < len(src); i += 4 { // RGBA -> XRGB8888 (BGRX in memory)
		dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i+2], src[i+1], src[i], 0xff
	}
	_ = p.surface.Attach(b.buf, 0, 0)
	_ = p.surface.Damage(0, 0, math.MaxInt32, math.MaxInt32)
	_ = p.surface.Commit()
	b.busy = true
}

// ---- pointer ----

func (p *Platform) pointerEvent(kind mini.PointerKind, x, y float64) {
	ev := mini.PointerEvent{Kind: kind, X: int(x * float64(p.bs)), Y: int(y * float64(p.bs))}
	if p.sink.Pointer(ev) {
		p.need = true
	}
}

func (p *Platform) loadCursor() {
	if p.cursorTried {
		return
	}
	p.cursorTried = true
	size := 24
	if v, err := strconv.Atoi(os.Getenv("XCURSOR_SIZE")); err == nil && v > 0 {
		size = v
	}
	th, err := cursor.LoadTheme(os.Getenv("XCURSOR_THEME"), size*int(p.bs), p.shm)
	if err != nil {
		return
	}
	c := th.GetCursor(cursor.LeftPtr)
	if c == nil || len(c.Images) == 0 {
		return
	}
	img := &c.Images[0]
	buf, err := img.GetBuffer()
	if err != nil {
		return
	}
	surf, err := p.compositor.CreateSurface()
	if err != nil {
		return
	}
	if p.bs > 1 {
		_ = surf.SetBufferScale(p.bs)
	}
	_ = surf.Attach(buf, 0, 0)
	_ = surf.Commit()
	p.cursorTheme, p.cursorSurf = th, surf
	p.cursorHX, p.cursorHY = int32(img.HotspotX)/p.bs, int32(img.HotspotY)/p.bs
}

func (p *Platform) setupPointer(ptr *client.Pointer) {
	ptr.SetEnterHandler(func(e client.PointerEnterEvent) {
		p.loadCursor()
		if p.cursorSurf != nil {
			_ = ptr.SetCursor(e.Serial, p.cursorSurf, p.cursorHX, p.cursorHY)
		}
		p.pointerEvent(mini.PointerMove, e.SurfaceX, e.SurfaceY)
	})
	ptr.SetLeaveHandler(func(client.PointerLeaveEvent) { p.pointerEvent(mini.PointerLeave, 0, 0) })
	ptr.SetMotionHandler(func(e client.PointerMotionEvent) {
		p.pointerEvent(mini.PointerMove, e.SurfaceX, e.SurfaceY)
		p.lastX, p.lastY = e.SurfaceX, e.SurfaceY
	})
	ptr.SetButtonHandler(func(e client.PointerButtonEvent) {
		if e.Button != 0x110 { // BTN_LEFT
			return
		}
		kind := mini.PointerUp
		if e.State == 1 {
			kind = mini.PointerDown
		}
		p.pointerEvent(kind, p.lastX, p.lastY)
	})
}

// ---- keyboard ----

func (p *Platform) setupKeyboard(kb *client.Keyboard) {
	kb.SetKeymapHandler(func(e client.KeyboardKeymapEvent) {
		defer unix.Close(e.Fd)
		if p.xkb == nil || e.Format != 1 { // 1 = xkb text v1
			return
		}
		data, err := unix.Mmap(e.Fd, 0, int(e.Size), unix.PROT_READ, unix.MAP_PRIVATE)
		if err != nil {
			return
		}
		defer unix.Munmap(data)
		_ = p.xkb.setKeymap(strings.TrimRight(string(data), "\x00"))
	})
	kb.SetEnterHandler(func(client.KeyboardEnterEvent) {
		if p.sink.Focus(true) {
			p.need = true
		}
	})
	kb.SetLeaveHandler(func(client.KeyboardLeaveEvent) {
		p.stopRepeat()
		if p.sink.Focus(false) {
			p.need = true
		}
	})
	kb.SetModifiersHandler(func(e client.KeyboardModifiersEvent) {
		if p.xkb != nil {
			p.xkb.updateMask(e.ModsDepressed, e.ModsLatched, e.ModsLocked, e.Group)
		}
	})
	kb.SetRepeatInfoHandler(func(e client.KeyboardRepeatInfoEvent) { p.repRate, p.repDelay = e.Rate, e.Delay })
	kb.SetKeyHandler(func(e client.KeyboardKeyEvent) {
		if e.State == 0 { // released
			if e.Key == p.repCode {
				p.stopRepeat()
			}
			return
		}
		ev := p.keyEvent(e.Key)
		if ev.Key == mini.KeyNone && ev.Text == "" {
			return
		}
		p.fireKey(ev)
		if p.repRate > 0 && (p.xkb == nil || !p.xkb.ready() || p.xkb.repeats(e.Key+8)) {
			p.stopRepeat()
			p.repCode, p.repEv = e.Key, ev
			p.repTimer = time.NewTimer(time.Duration(p.repDelay) * time.Millisecond)
			p.repC = p.repTimer.C
		}
	})
}

func (p *Platform) fireKey(ev mini.KeyEvent) {
	if p.sink.Key(ev) {
		p.need = true
	}
}

func (p *Platform) stopRepeat() {
	if p.repTimer != nil {
		p.repTimer.Stop()
	}
	p.repTimer, p.repC, p.repCode = nil, nil, 0
}

// keyEvent translates an evdev key code through xkb; without libxkbcommon
// only the navigation keys work.
func (p *Platform) keyEvent(code uint32) mini.KeyEvent {
	if !p.xkb.ready() {
		return evdevNav(code)
	}
	kc := code + 8
	ev := mini.FromKeysym(p.xkb.sym(kc))
	if ev.Key == mini.KeyNone && ev.Text == "" {
		if r := p.xkb.utf32(kc); r >= 0x20 && r != 0x7f {
			ev.Text = string(rune(r))
		}
	}
	ev.Shift = ev.Shift || p.xkb.mod("Shift")
	ev.Ctrl = p.xkb.mod("Control")
	ev.Alt = p.xkb.mod("Mod1")
	return ev
}

func evdevNav(code uint32) mini.KeyEvent {
	switch code {
	case 1:
		return mini.KeyEvent{Key: mini.KeyEscape}
	case 14:
		return mini.KeyEvent{Key: mini.KeyBackspace}
	case 15:
		return mini.KeyEvent{Key: mini.KeyTab}
	case 28, 96:
		return mini.KeyEvent{Key: mini.KeyEnter}
	case 102:
		return mini.KeyEvent{Key: mini.KeyHome}
	case 103:
		return mini.KeyEvent{Key: mini.KeyUp}
	case 105:
		return mini.KeyEvent{Key: mini.KeyLeft}
	case 106:
		return mini.KeyEvent{Key: mini.KeyRight}
	case 107:
		return mini.KeyEvent{Key: mini.KeyEnd}
	case 108:
		return mini.KeyEvent{Key: mini.KeyDown}
	case 111:
		return mini.KeyEvent{Key: mini.KeyDelete}
	}
	return mini.KeyEvent{}
}
