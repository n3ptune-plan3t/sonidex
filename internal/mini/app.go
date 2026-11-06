package mini

import (
	"image"
	"sync"
)

// App couples a Form to a Platform and gives other goroutines a safe way to
// touch the UI: queue a function with Do and it runs on the UI goroutine.
type App struct {
	Form *Form

	mu    sync.Mutex
	plat  Platform
	queue []func()
}

func NewApp(scale float64) *App { return &App{Form: NewForm(scale)} }

// Do runs fn on the UI goroutine and repaints afterwards. Safe from any
// goroutine, including before Run.
func (a *App) Do(fn func()) {
	a.mu.Lock()
	a.queue = append(a.queue, fn)
	p := a.plat
	a.mu.Unlock()
	if p != nil {
		p.Wake()
	}
}

// Run opens the window on p and blocks until it is closed.
func (a *App) Run(p Platform, opt WindowOptions) error {
	a.mu.Lock()
	a.plat = p
	a.mu.Unlock()
	return p.Run(opt, a)
}

func (a *App) Pointer(ev PointerEvent) bool { return a.Form.Pointer(ev) }
func (a *App) Key(ev KeyEvent) bool         { return a.Form.Key(ev) }
func (a *App) Draw(img *image.RGBA)         { a.Form.Draw(img) }

func (a *App) Focus(on bool) bool {
	a.Form.active = on
	return true
}

func (a *App) Wake() bool {
	a.mu.Lock()
	q := a.queue
	a.queue = nil
	a.mu.Unlock()
	for _, fn := range q {
		fn()
	}
	return len(q) > 0
}
