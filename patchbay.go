package main

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"sonidex/backend"
)

// A small Helvum-style patchbay for Windows: apps on the left, the Sonidex
// output on the right. Drag from an app's port to the Sonidex port (or click
// the port) to link it; click a cable (or the port again) to unlink.

const (
	pbRowH     = 46
	pbRowGap   = 8
	pbPad      = 14
	pbPortR    = 7
	pbSinkH    = 74
	pbMaxNodeW = 320
)

type pbNode struct {
	pid    uint32
	name   string
	detail string
	peak   float32
	gone   bool // linked, but no audio session any more
}

type patchbay struct {
	widget.BaseWidget
	router *backend.Router
	status func(string)

	mu    sync.Mutex
	nodes []pbNode
	names map[uint32]string // remembered names for linked pids

	dragging bool
	dragPID  uint32
	dragPos  fyne.Position
}

func newPatchbay(r *backend.Router, status func(string)) *patchbay {
	p := &patchbay{router: r, status: status, names: map[uint32]string{}}
	p.ExtendBaseWidget(p)
	return p
}

func (p *patchbay) MinSize() fyne.Size { return fyne.NewSize(420, 260) }

// refreshSessions re-reads the audio sessions and redraws.
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
	p.Refresh()
}

func (p *patchbay) nodeRect(size fyne.Size, i int) (pos fyne.Position, w float32) {
	w = size.Width/2 - pbPad*2
	if w > pbMaxNodeW {
		w = pbMaxNodeW
	}
	return fyne.NewPos(pbPad, pbPad+float32(i)*(pbRowH+pbRowGap)), w
}

func (p *patchbay) sinkRect(size fyne.Size) (pos fyne.Position, w float32) {
	w = size.Width/2 - pbPad*2
	if w > pbMaxNodeW {
		w = pbMaxNodeW
	}
	return fyne.NewPos(size.Width-pbPad-w, pbPad), w
}

func (p *patchbay) srcPort(size fyne.Size, i int) fyne.Position {
	pos, w := p.nodeRect(size, i)
	return fyne.NewPos(pos.X+w, pos.Y+pbRowH/2)
}

func (p *patchbay) sinkPort(size fyne.Size) fyne.Position {
	pos, _ := p.sinkRect(size)
	return fyne.NewPos(pos.X, pos.Y+pbSinkH/2)
}

func dist(a, b fyne.Position) float64 {
	return math.Hypot(float64(a.X-b.X), float64(a.Y-b.Y))
}

func segDist(p, a, b fyne.Position) float64 {
	dx, dy := float64(b.X-a.X), float64(b.Y-a.Y)
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return dist(p, a)
	}
	t := (float64(p.X-a.X)*dx + float64(p.Y-a.Y)*dy) / l2
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(float64(p.X)-(float64(a.X)+t*dx), float64(p.Y)-(float64(a.Y)+t*dy))
}

func (p *patchbay) portAt(pos fyne.Position) (idx int, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.nodes {
		if dist(pos, p.srcPort(p.Size(), i)) <= pbPortR+6 {
			return i, true
		}
	}
	return 0, false
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

// --- interaction ---

func (p *patchbay) Tapped(e *fyne.PointEvent) {
	if i, ok := p.portAt(e.Position); ok {
		p.mu.Lock()
		pid := p.nodes[i].pid
		p.mu.Unlock()
		p.toggle(pid, !p.router.IsLinked(pid))
		return
	}
	// Click on a cable removes it.
	sink := p.sinkPort(p.Size())
	p.mu.Lock()
	var hit *uint32
	for i := range p.nodes {
		if p.router.IsLinked(p.nodes[i].pid) && segDist(e.Position, p.srcPort(p.Size(), i), sink) < 8 {
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

func (p *patchbay) Dragged(e *fyne.DragEvent) {
	if !p.dragging {
		start := fyne.NewPos(e.Position.X-e.Dragged.DX, e.Position.Y-e.Dragged.DY)
		i, ok := p.portAt(start)
		if !ok {
			return
		}
		p.mu.Lock()
		p.dragPID = p.nodes[i].pid
		p.mu.Unlock()
		p.dragging = true
	}
	p.dragPos = e.Position
	p.Refresh()
}

func (p *patchbay) DragEnd() {
	if !p.dragging {
		return
	}
	p.dragging = false
	if dist(p.dragPos, p.sinkPort(p.Size())) <= pbPortR+14 {
		p.toggle(p.dragPID, true)
	}
	p.Refresh()
}

// --- rendering ---

func (p *patchbay) CreateRenderer() fyne.WidgetRenderer {
	r := &pbRenderer{p: p}
	r.rebuild(p.Size())
	return r
}

type pbRenderer struct {
	p    *patchbay
	objs []fyne.CanvasObject
}

func (r *pbRenderer) Layout(s fyne.Size)           { r.rebuild(s) }
func (r *pbRenderer) MinSize() fyne.Size           { return r.p.MinSize() }
func (r *pbRenderer) Refresh()                     { r.rebuild(r.p.Size()); canvas.Refresh(r.p) }
func (r *pbRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *pbRenderer) Destroy()                     {}

func (r *pbRenderer) rebuild(size fyne.Size) {
	p := r.p
	var objs []fyne.CanvasObject
	bg := canvas.NewRectangle(theme.BackgroundColor())
	bg.Resize(size)
	objs = append(objs, bg)

	nodeFill := theme.InputBackgroundColor()
	accent := theme.PrimaryColor()
	idle := theme.DisabledColor()
	text := theme.ForegroundColor()

	p.mu.Lock()
	nodes := append([]pbNode(nil), p.nodes...)
	p.mu.Unlock()

	// Sink node.
	sp, sw := p.sinkRect(size)
	sink := canvas.NewRectangle(nodeFill)
	sink.StrokeColor = accent
	sink.StrokeWidth = 2
	sink.CornerRadius = 6
	sink.Move(sp)
	sink.Resize(fyne.NewSize(sw, pbSinkH))
	objs = append(objs, sink)
	t := canvas.NewText("Sonidex → Phone", text)
	t.TextStyle = fyne.TextStyle{Bold: true}
	t.TextSize = 14
	t.Move(fyne.NewPos(sp.X+16, sp.Y+14))
	objs = append(objs, t)
	n := len(p.router.Linked())
	sub := canvas.NewText(fmt.Sprintf("%d source(s) linked", n), idle)
	sub.TextSize = 11
	sub.Move(fyne.NewPos(sp.X+16, sp.Y+38))
	objs = append(objs, sub)
	sinkPort := p.sinkPort(size)

	var cables, rows []fyne.CanvasObject
	for i, nd := range nodes {
		pos, w := p.nodeRect(size, i)
		linked := p.router.IsLinked(nd.pid)
		box := canvas.NewRectangle(nodeFill)
		box.CornerRadius = 6
		if linked {
			box.StrokeColor, box.StrokeWidth = accent, 2
		} else {
			box.StrokeColor, box.StrokeWidth = idle, 1
		}
		box.Move(pos)
		box.Resize(fyne.NewSize(w, pbRowH))
		rows = append(rows, box)

		// Level meter along the bottom edge.
		if nd.peak > 0 {
			m := canvas.NewRectangle(color.NRGBA{R: 0x3d, G: 0xd6, B: 0x8c, A: 0xff})
			m.Move(fyne.NewPos(pos.X+8, pos.Y+pbRowH-6))
			m.Resize(fyne.NewSize((w-16)*float32(math.Min(1, float64(nd.peak))), 3))
			rows = append(rows, m)
		}
		title := canvas.NewText(nd.name, text)
		title.TextStyle = fyne.TextStyle{Bold: true}
		title.TextSize = 13
		title.Move(fyne.NewPos(pos.X+10, pos.Y+4))
		rows = append(rows, title)
		d := canvas.NewText(nd.detail, idle)
		d.TextSize = 10
		d.Move(fyne.NewPos(pos.X+10, pos.Y+24))
		rows = append(rows, d)

		pp := p.srcPort(size, i)
		pc := canvas.NewCircle(idle)
		if linked {
			pc.FillColor = accent
		}
		pc.StrokeColor, pc.StrokeWidth = text, 1
		pc.Move(fyne.NewPos(pp.X-pbPortR, pp.Y-pbPortR))
		pc.Resize(fyne.NewSize(pbPortR*2, pbPortR*2))
		rows = append(rows, pc)

		if linked {
			c := accent
			if nd.gone {
				c = idle
			}
			l := canvas.NewLine(c)
			l.StrokeWidth = 3
			l.Position1, l.Position2 = pp, sinkPort
			cables = append(cables, l)
		}
	}
	sc := canvas.NewCircle(accent)
	sc.StrokeColor, sc.StrokeWidth = text, 1
	sc.Move(fyne.NewPos(sinkPort.X-pbPortR, sinkPort.Y-pbPortR))
	sc.Resize(fyne.NewSize(pbPortR*2, pbPortR*2))

	objs = append(objs, cables...)
	objs = append(objs, rows...)
	objs = append(objs, sc)

	if p.dragging {
		for i, nd := range nodes {
			if nd.pid == p.dragPID {
				l := canvas.NewLine(accent)
				l.StrokeWidth = 2
				l.Position1, l.Position2 = p.srcPort(size, i), p.dragPos
				objs = append(objs, l)
			}
		}
	}
	r.objs = objs
}

// startAutoRefresh polls sessions (and meters) until stop is closed.
func (p *patchbay) startAutoRefresh(stop <-chan struct{}) {
	p.refreshSessions()
	go func() {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				p.refreshSessions()
			}
		}
	}()
}
