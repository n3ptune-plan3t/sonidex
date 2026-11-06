// Package mini is a tiny software-rendered GUI toolkit: a vertical form of
// labels, buttons, check boxes, text entries and drop-downs painted into a
// plain RGBA buffer. It has no GTK/Qt/OpenGL dependency; a Platform (X11 or
// Wayland, in sub-packages) only has to present that buffer and deliver
// input. The UI is event-driven: nothing is drawn or woken while idle.
package mini

import "image"

type PointerKind int

const (
	PointerMove PointerKind = iota
	PointerDown
	PointerUp
	PointerLeave
)

// PointerEvent is a left-button/motion event in window pixels.
type PointerEvent struct {
	Kind PointerKind
	X, Y int
}

type Key int

const (
	KeyNone Key = iota
	KeyBackspace
	KeyDelete
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyHome
	KeyEnd
	KeyEnter
	KeyEscape
	KeyTab
)

// KeyEvent is one key press (or auto-repeat). Text holds the typed
// character(s) for printable keys; Key names navigation/editing keys.
type KeyEvent struct {
	Key              Key
	Text             string
	Shift, Ctrl, Alt bool
}

// WindowOptions are in physical pixels.
type WindowOptions struct {
	Title, AppID string
	W, H         int
	MinW, MinH   int
}

// Platform presents frames and delivers input. All Sink calls happen on the
// goroutine that runs Run; only Wake may be called from other goroutines.
type Platform interface {
	// Scale is the UI scale (1 = 96 dpi) to render at.
	Scale() float64
	// Run opens the window and runs the event loop until it is closed.
	Run(opt WindowOptions, sink Sink) error
	// Wake makes the loop call Sink.Wake soon. Safe from any goroutine.
	Wake()
}

// Sink is implemented by App. Methods that return bool report whether the
// window needs repainting.
type Sink interface {
	Pointer(PointerEvent) bool
	Key(KeyEvent) bool
	Focus(bool) bool
	Wake() bool
	// Draw paints a full frame into img (its bounds are the window size).
	Draw(img *image.RGBA)
}
