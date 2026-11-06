package mini

import (
	"image"
	"image/color"
	"os"
	"strings"

	"golang.org/x/image/font"
)

// Theme holds the palette. SONIDEX_THEME=light selects the light one.
type Theme struct {
	BG, FG, Dim, Field, Border, Accent, AccentFG, Btn color.RGBA
}

func DefaultTheme() Theme {
	if strings.EqualFold(os.Getenv("SONIDEX_THEME"), "light") {
		return Theme{
			BG: rgb(0xf3, 0xf3, 0xf5), FG: rgb(0x1c, 0x1d, 0x21), Dim: rgb(0x6b, 0x6f, 0x78),
			Field: rgb(0xff, 0xff, 0xff), Border: rgb(0xc9, 0xcc, 0xd3), Accent: rgb(0x2b, 0x6c, 0xe6),
			AccentFG: rgb(0xff, 0xff, 0xff), Btn: rgb(0xe4, 0xe6, 0xea),
		}
	}
	return Theme{
		BG: rgb(0x1f, 0x20, 0x24), FG: rgb(0xe6, 0xe6, 0xe9), Dim: rgb(0x8b, 0x8f, 0x98),
		Field: rgb(0x2a, 0x2c, 0x32), Border: rgb(0x3d, 0x40, 0x48), Accent: rgb(0x4c, 0x8d, 0xf6),
		AccentFG: rgb(0xff, 0xff, 0xff), Btn: rgb(0x34, 0x37, 0x40),
	}
}

func rgb(r, g, b uint8) color.RGBA { return color.RGBA{r, g, b, 255} }

func mix(a, b color.RGBA, t float64) color.RGBA {
	m := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return color.RGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 255}
}

// Widget is one row of a Form.
type Widget interface {
	base() *Base
	height(f *Form, w int) int
	draw(f *Form, c *Canvas)
	pointer(f *Form, ev PointerEvent) bool
	key(f *Form, ev KeyEvent) bool
	focusable() bool
}

// Base is embedded by every widget.
type Base struct {
	rect   image.Rectangle
	hidden bool
	gap    float64
}

func (b *Base) base() *Base { return b }

// Show shows or hides the widget (hidden widgets take no space).
func (b *Base) Show(v bool) { b.hidden = !v }

// Form is a vertical stack of widgets with focus, hover and a drop-down
// overlay. All methods must run on the UI goroutine (use App.Do).
type Form struct {
	S     float64 // UI scale
	Theme Theme

	items   []Widget
	focus   Widget
	hover   Widget
	pressed Widget
	popup   *Select
	active  bool // window has keyboard focus
	size    image.Point
}

func NewForm(scale float64) *Form {
	if scale <= 0 {
		scale = 1
	}
	return &Form{S: scale, Theme: DefaultTheme(), active: true}
}

func (f *Form) px(v float64) int { return int(v*f.S + 0.5) }

// Add appends w, separated from the previous row by gap logical pixels.
func (f *Form) Add(w Widget, gap float64) {
	w.base().gap = gap
	f.items = append(f.items, w)
}

func (f *Form) layout(w int) int {
	m := f.px(16)
	cw := w - 2*m
	y := m
	first := true
	for _, it := range f.items {
		b := it.base()
		if b.hidden {
			b.rect = image.Rectangle{}
			continue
		}
		if !first {
			y += f.px(b.gap)
		}
		first = false
		h := it.height(f, cw)
		b.rect = image.Rect(m, y, m+cw, y+h)
		y += h
	}
	return y + m
}

// Measure returns the content height in pixels for a width given in
// logical pixels, with the widgets' current visibility.
func (f *Form) Measure(wLogical float64) int { return f.layout(f.px(wLogical)) }

// Draw paints the whole form.
func (f *Form) Draw(img *image.RGBA) {
	f.size = img.Rect.Size()
	f.layout(f.size.X)
	c := &Canvas{Img: img}
	c.Clear(f.Theme.BG)
	for _, it := range f.items {
		if !it.base().hidden {
			it.draw(f, c)
		}
	}
	if f.popup != nil {
		f.popup.drawList(f, c)
	}
}

func (f *Form) hit(x, y int) Widget {
	p := image.Pt(x, y)
	for i := len(f.items) - 1; i >= 0; i-- {
		b := f.items[i].base()
		if !b.hidden && p.In(b.rect) {
			return f.items[i]
		}
	}
	return nil
}

func (f *Form) focused(w Widget) bool { return f.active && f.focus == w && !w.base().hidden }

func (f *Form) setFocus(w Widget) { f.focus = w }

func (f *Form) focusables() []Widget {
	var out []Widget
	for _, it := range f.items {
		if it.focusable() && !it.base().hidden {
			out = append(out, it)
		}
	}
	return out
}

func (f *Form) cycle(back bool) {
	list := f.focusables()
	if len(list) == 0 {
		return
	}
	cur := -1
	for i, w := range list {
		if w == f.focus {
			cur = i
		}
	}
	switch {
	case cur < 0 && back:
		cur = len(list) - 1
	case cur < 0:
		cur = 0
	case back:
		cur = (cur + len(list) - 1) % len(list)
	default:
		cur = (cur + 1) % len(list)
	}
	f.focus = list[cur]
}

// Pointer routes a mouse event; it reports whether a repaint is needed.
func (f *Form) Pointer(ev PointerEvent) bool {
	if ev.Kind == PointerLeave {
		had := f.hover != nil
		f.hover = nil
		return had
	}
	if f.popup != nil {
		return f.popup.popupPointer(f, ev)
	}
	if f.pressed != nil {
		p := f.pressed
		p.pointer(f, ev)
		if ev.Kind == PointerUp {
			f.pressed = nil
		}
		return true
	}
	t := f.hit(ev.X, ev.Y)
	redraw := t != f.hover
	f.hover = t
	if ev.Kind == PointerDown {
		if t != nil {
			if t.focusable() {
				f.setFocus(t)
			}
			f.pressed = t
			t.pointer(f, ev)
		} else {
			f.setFocus(nil)
		}
		redraw = true
	}
	return redraw
}

// Key routes a key press; it reports whether a repaint is needed.
func (f *Form) Key(ev KeyEvent) bool {
	if f.popup != nil {
		return f.popup.popupKey(f, ev)
	}
	if ev.Key == KeyTab {
		f.cycle(ev.Shift)
		return true
	}
	if f.focus != nil && !f.focus.base().hidden {
		return f.focus.key(f, ev)
	}
	return false
}

func within(r image.Rectangle, x, y int) bool { return image.Pt(x, y).In(r) }

func isActivate(ev KeyEvent) bool { return ev.Key == KeyEnter || ev.Text == " " }

// ---- Label ----

type Label struct {
	Base
	Text     string
	Bold     bool
	Dim      bool
	Center   bool
	Wrap     bool
	Size     float64 // logical pixels; 0 = 14
	MinLines int
}

func (l *Label) focusable() bool { return false }

func (l *Label) face(f *Form) font.Face {
	sz := l.Size
	if sz == 0 {
		sz = 14
	}
	return face(l.Bold, sz*f.S)
}

func wrapText(fc font.Face, s string, w int) []string {
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		cur := words[0]
		for _, wd := range words[1:] {
			if textWidth(fc, cur+" "+wd) <= w {
				cur += " " + wd
			} else {
				lines = append(lines, cur)
				cur = wd
			}
		}
		lines = append(lines, cur)
	}
	return lines
}

func (l *Label) lines(f *Form, w int) []string {
	if !l.Wrap {
		return []string{l.Text}
	}
	return wrapText(l.face(f), l.Text, w)
}

func (l *Label) height(f *Form, w int) int {
	n := len(l.lines(f, w))
	if n < l.MinLines {
		n = l.MinLines
	}
	return n*lineHeight(l.face(f)) + f.px(2)
}

func (l *Label) draw(f *Form, c *Canvas) {
	fc := l.face(f)
	col := f.Theme.FG
	if l.Dim {
		col = f.Theme.Dim
	}
	clip := c.Sub(l.rect)
	y := l.rect.Min.Y + fc.Metrics().Ascent.Ceil() + f.px(1)
	for _, ln := range l.lines(f, l.rect.Dx()) {
		x := l.rect.Min.X
		if l.Center {
			x += (l.rect.Dx() - textWidth(fc, ln)) / 2
		}
		c.TextIn(clip, x, y, ln, fc, col)
		y += lineHeight(fc)
	}
}

func (l *Label) pointer(*Form, PointerEvent) bool { return false }
func (l *Label) key(*Form, KeyEvent) bool         { return false }

// ---- Button ----

type Button struct {
	Base
	Text    string
	Primary bool
	OnClick func() // runs on the UI goroutine: start slow work in a goroutine

	down bool
}

func (b *Button) focusable() bool           { return true }
func (b *Button) height(f *Form, _ int) int { return f.px(36) }

func (b *Button) draw(f *Form, c *Canvas) {
	t := f.Theme
	fill, fg := t.Btn, t.FG
	if b.Primary {
		fill, fg = t.Accent, t.AccentFG
	}
	switch {
	case b.down:
		fill = mix(fill, color.RGBA{0, 0, 0, 255}, 0.18)
	case f.hover == Widget(b):
		fill = mix(fill, color.RGBA{255, 255, 255, 255}, 0.10)
	}
	border, bw := t.Border, 1.0
	if b.Primary {
		border, bw = fill, 0
	}
	if f.focused(b) {
		border, bw = t.Accent, 2*f.S
		if b.Primary {
			border = mix(t.Accent, color.RGBA{255, 255, 255, 255}, 0.55)
		}
	}
	c.RRect(b.rect, float64(f.px(6)), fill, border, bw)
	fc := face(false, 14*f.S)
	x := b.rect.Min.X + (b.rect.Dx()-textWidth(fc, b.Text))/2
	y := b.rect.Min.Y + (b.rect.Dy()-lineHeight(fc))/2 + fc.Metrics().Ascent.Ceil()
	c.TextIn(c.Sub(b.rect), x, y, b.Text, fc, fg)
}

func (b *Button) click() {
	if b.OnClick != nil {
		b.OnClick()
	}
}

func (b *Button) pointer(f *Form, ev PointerEvent) bool {
	in := within(b.rect, ev.X, ev.Y)
	switch ev.Kind {
	case PointerDown:
		b.down = true
	case PointerMove:
		b.down = in
	case PointerUp:
		was := b.down
		b.down = false
		if was && in {
			b.click()
		}
	}
	return true
}

func (b *Button) key(f *Form, ev KeyEvent) bool {
	if isActivate(ev) {
		b.click()
	}
	return true
}

// ---- Check ----

type Check struct {
	Base
	Text     string
	Checked  bool
	OnChange func(bool)
}

func (k *Check) focusable() bool           { return true }
func (k *Check) height(f *Form, _ int) int { return f.px(28) }

func (k *Check) toggle() {
	k.Checked = !k.Checked
	if k.OnChange != nil {
		k.OnChange(k.Checked)
	}
}

func (k *Check) draw(f *Form, c *Canvas) {
	t := f.Theme
	box := f.px(18)
	top := k.rect.Min.Y + (k.rect.Dy()-box)/2
	r := image.Rect(k.rect.Min.X, top, k.rect.Min.X+box, top+box)
	if f.focused(k) {
		c.RRect(r.Inset(-f.px(3)), float64(f.px(7)), t.BG, t.Accent, 1.5*f.S)
	}
	if k.Checked {
		c.RRect(r, float64(f.px(4)), t.Accent, t.Accent, 0)
		fx, fy, s := float64(r.Min.X), float64(r.Min.Y), float64(box)
		w := 2 * f.S
		c.Line(fx+s*0.24, fy+s*0.52, fx+s*0.43, fy+s*0.72, w, t.AccentFG)
		c.Line(fx+s*0.43, fy+s*0.72, fx+s*0.78, fy+s*0.30, w, t.AccentFG)
	} else {
		c.RRect(r, float64(f.px(4)), t.Field, t.Border, 1)
	}
	fc := face(false, 14*f.S)
	y := k.rect.Min.Y + (k.rect.Dy()-lineHeight(fc))/2 + fc.Metrics().Ascent.Ceil()
	c.TextIn(c.Sub(k.rect), r.Max.X+f.px(10), y, k.Text, fc, t.FG)
}

func (k *Check) pointer(f *Form, ev PointerEvent) bool {
	if ev.Kind == PointerUp && within(k.rect, ev.X, ev.Y) {
		k.toggle()
	}
	return true
}

func (k *Check) key(f *Form, ev KeyEvent) bool {
	if isActivate(ev) {
		k.toggle()
	}
	return true
}

// ---- Entry ----

type Entry struct {
	Base
	Text        string
	Placeholder string
	MaxLen      int
	OnChange    func(string)

	cursor int // rune index
	scroll int // px
}

func (e *Entry) focusable() bool           { return true }
func (e *Entry) height(f *Form, _ int) int { return f.px(36) }

// SetText replaces the text and moves the caret to the end.
func (e *Entry) SetText(s string) {
	e.Text = s
	e.cursor = len([]rune(s))
	e.scroll = 0
}

func (e *Entry) pad(f *Form) int { return f.px(10) }

func (e *Entry) draw(f *Form, c *Canvas) {
	t := f.Theme
	focused := f.focused(e)
	border, bw := t.Border, 1.0
	if focused {
		border, bw = t.Accent, 2*f.S
	}
	c.RRect(e.rect, float64(f.px(6)), t.Field, border, bw)

	fc := face(false, 14*f.S)
	inner := image.Rect(e.rect.Min.X+e.pad(f), e.rect.Min.Y+2, e.rect.Max.X-e.pad(f), e.rect.Max.Y-2)
	rs := []rune(e.Text)
	if e.cursor > len(rs) {
		e.cursor = len(rs)
	}
	caret := textWidth(fc, string(rs[:e.cursor]))
	if caret-e.scroll > inner.Dx()-2 {
		e.scroll = caret - inner.Dx() + 2
	}
	if caret-e.scroll < 0 {
		e.scroll = caret
	}
	asc := fc.Metrics().Ascent.Ceil()
	base := e.rect.Min.Y + (e.rect.Dy()-lineHeight(fc))/2 + asc
	clip := c.Sub(inner)
	if e.Text == "" && !focused {
		c.TextIn(clip, inner.Min.X, base, e.Placeholder, fc, t.Dim)
	} else {
		c.TextIn(clip, inner.Min.X-e.scroll, base, e.Text, fc, t.FG)
	}
	if focused {
		cw := f.px(1)
		x := inner.Min.X + caret - e.scroll
		c.FillRect(image.Rect(x, base-asc, x+cw, base-asc+lineHeight(fc)), t.FG)
	}
}

func (e *Entry) pointer(f *Form, ev PointerEvent) bool {
	if ev.Kind != PointerDown {
		return true
	}
	fc := face(false, 14*f.S)
	rs := []rune(e.Text)
	xr := ev.X - (e.rect.Min.X + e.pad(f)) + e.scroll
	idx := 0
	for i := 1; i <= len(rs); i++ {
		w := textWidth(fc, string(rs[:i]))
		wp := textWidth(fc, string(rs[:i-1]))
		if xr < (w+wp)/2 {
			break
		}
		idx = i
	}
	e.cursor = idx
	return true
}

func (e *Entry) key(f *Form, ev KeyEvent) bool {
	rs := []rune(e.Text)
	if e.cursor > len(rs) {
		e.cursor = len(rs)
	}
	changed := false
	switch ev.Key {
	case KeyBackspace:
		if e.cursor > 0 {
			rs = append(rs[:e.cursor-1], rs[e.cursor:]...)
			e.cursor--
			changed = true
		}
	case KeyDelete:
		if e.cursor < len(rs) {
			rs = append(rs[:e.cursor], rs[e.cursor+1:]...)
			changed = true
		}
	case KeyLeft:
		if e.cursor > 0 {
			e.cursor--
		}
	case KeyRight:
		if e.cursor < len(rs) {
			e.cursor++
		}
	case KeyHome:
		e.cursor = 0
	case KeyEnd:
		e.cursor = len(rs)
	default:
		if ev.Text != "" && !ev.Ctrl && !ev.Alt {
			for _, r := range ev.Text {
				if r < 0x20 || r == 0x7f || (e.MaxLen > 0 && len(rs) >= e.MaxLen) {
					continue
				}
				rs = append(rs[:e.cursor], append([]rune{r}, rs[e.cursor:]...)...)
				e.cursor++
				changed = true
			}
		}
	}
	if changed {
		e.Text = string(rs)
		if e.OnChange != nil {
			e.OnChange(e.Text)
		}
	}
	return true
}

// ---- Select ----

type Select struct {
	Base
	Options  []string
	Index    int // -1 = nothing selected
	OnChange func(int)

	hoverIdx int
}

func (s *Select) focusable() bool           { return true }
func (s *Select) height(f *Form, _ int) int { return f.px(36) }

// Selected returns the chosen option, or "".
func (s *Select) Selected() string {
	if s.Index >= 0 && s.Index < len(s.Options) {
		return s.Options[s.Index]
	}
	return ""
}

// SetOptions replaces the options and selects idx (no OnChange call).
func (s *Select) SetOptions(opts []string, idx int) {
	s.Options = opts
	if idx < 0 || idx >= len(opts) {
		idx = -1
	}
	s.Index = idx
}

// SetSelected selects the option named name (no OnChange call).
func (s *Select) SetSelected(name string) {
	for i, o := range s.Options {
		if o == name {
			s.Index = i
			return
		}
	}
}

func (s *Select) choose(i int) {
	if i < 0 || i >= len(s.Options) || i == s.Index {
		return
	}
	s.Index = i
	if s.OnChange != nil {
		s.OnChange(i)
	}
}

func (s *Select) draw(f *Form, c *Canvas) {
	t := f.Theme
	border, bw := t.Border, 1.0
	if f.focused(s) || f.popup == s {
		border, bw = t.Accent, 2*f.S
	}
	fill := t.Field
	if f.hover == Widget(s) && f.popup == nil {
		fill = mix(fill, color.RGBA{255, 255, 255, 255}, 0.06)
	}
	c.RRect(s.rect, float64(f.px(6)), fill, border, bw)
	fc := face(false, 14*f.S)
	pad := f.px(10)
	y := s.rect.Min.Y + (s.rect.Dy()-lineHeight(fc))/2 + fc.Metrics().Ascent.Ceil()
	inner := image.Rect(s.rect.Min.X+pad, s.rect.Min.Y, s.rect.Max.X-f.px(30), s.rect.Max.Y)
	if sel := s.Selected(); sel != "" {
		c.TextIn(c.Sub(inner), inner.Min.X, y, sel, fc, t.FG)
	} else {
		c.TextIn(c.Sub(inner), inner.Min.X, y, "None", fc, t.Dim)
	}
	cx, cy := float64(s.rect.Max.X-f.px(17)), float64(s.rect.Min.Y+s.rect.Dy()/2)
	d, w := 4*f.S, 1.8*f.S
	c.Line(cx-d, cy-d/2, cx, cy+d/2, w, t.Dim)
	c.Line(cx, cy+d/2, cx+d, cy-d/2, w, t.Dim)
}

func (s *Select) open(f *Form) {
	if len(s.Options) == 0 {
		return
	}
	f.popup = s
	s.hoverIdx = s.Index
	if s.hoverIdx < 0 {
		s.hoverIdx = 0
	}
}

func (s *Select) pointer(f *Form, ev PointerEvent) bool {
	if ev.Kind == PointerUp && within(s.rect, ev.X, ev.Y) {
		s.open(f)
	}
	return true
}

func (s *Select) key(f *Form, ev KeyEvent) bool {
	switch {
	case ev.Key == KeyUp:
		s.choose(s.Index - 1)
	case ev.Key == KeyDown:
		if s.Index < 0 {
			s.choose(0)
		} else {
			s.choose(s.Index + 1)
		}
	case isActivate(ev):
		s.open(f)
	}
	return true
}

func (s *Select) rowH(f *Form) int { return f.px(30) }

// listRect is where the open list is drawn: below the field if it fits,
// otherwise above it.
func (s *Select) listRect(f *Form) image.Rectangle {
	rh := s.rowH(f)
	n := len(s.Options)
	h := n * rh
	gap := f.px(2)
	below := f.size.Y - (s.rect.Max.Y + gap)
	above := s.rect.Min.Y - gap
	switch {
	case h <= below:
		return image.Rect(s.rect.Min.X, s.rect.Max.Y+gap, s.rect.Max.X, s.rect.Max.Y+gap+h)
	case h <= above:
		return image.Rect(s.rect.Min.X, s.rect.Min.Y-gap-h, s.rect.Max.X, s.rect.Min.Y-gap)
	case below >= above:
		rows := below / rh
		if rows < 1 {
			rows = 1
		}
		return image.Rect(s.rect.Min.X, s.rect.Max.Y+gap, s.rect.Max.X, s.rect.Max.Y+gap+rows*rh)
	default:
		rows := above / rh
		if rows < 1 {
			rows = 1
		}
		return image.Rect(s.rect.Min.X, s.rect.Min.Y-gap-rows*rh, s.rect.Max.X, s.rect.Min.Y-gap)
	}
}

func (s *Select) drawList(f *Form, c *Canvas) {
	t := f.Theme
	lr := s.listRect(f)
	rh := s.rowH(f)
	c.RRect(lr, float64(f.px(6)), t.Field, t.Border, 1)
	fc := face(false, 14*f.S)
	rows := lr.Dy() / rh
	for i := 0; i < rows && i < len(s.Options); i++ {
		row := image.Rect(lr.Min.X+f.px(3), lr.Min.Y+i*rh+f.px(2), lr.Max.X-f.px(3), lr.Min.Y+(i+1)*rh-f.px(2))
		fg := t.FG
		switch {
		case i == s.hoverIdx:
			c.RRect(row, float64(f.px(4)), t.Accent, t.Accent, 0)
			fg = t.AccentFG
		case i == s.Index:
			c.RRect(row, float64(f.px(4)), t.Btn, t.Btn, 0)
		}
		y := row.Min.Y + (row.Dy()-lineHeight(fc))/2 + fc.Metrics().Ascent.Ceil()
		c.TextIn(c.Sub(row), row.Min.X+f.px(7), y, s.Options[i], fc, fg)
	}
}

func (s *Select) rowAt(f *Form, x, y int) int {
	lr := s.listRect(f)
	if !within(lr, x, y) {
		return -1
	}
	i := (y - lr.Min.Y) / s.rowH(f)
	if i >= len(s.Options) {
		return -1
	}
	return i
}

func (s *Select) popupPointer(f *Form, ev PointerEvent) bool {
	switch ev.Kind {
	case PointerMove:
		if i := s.rowAt(f, ev.X, ev.Y); i >= 0 && i != s.hoverIdx {
			s.hoverIdx = i
			return true
		}
		return false
	case PointerDown:
		f.popup = nil
		if i := s.rowAt(f, ev.X, ev.Y); i >= 0 {
			s.choose(i)
		}
		return true
	}
	return false
}

func (s *Select) popupKey(f *Form, ev KeyEvent) bool {
	switch {
	case ev.Key == KeyUp:
		if s.hoverIdx > 0 {
			s.hoverIdx--
		}
	case ev.Key == KeyDown:
		if s.hoverIdx < len(s.Options)-1 {
			s.hoverIdx++
		}
	case isActivate(ev):
		f.popup = nil
		s.choose(s.hoverIdx)
	case ev.Key == KeyEscape, ev.Key == KeyTab:
		f.popup = nil
	}
	return true
}
