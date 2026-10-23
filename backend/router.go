package backend

import (
	"fmt"
	"io"
	"sort"
	"sync"
)

// SystemPID is the pseudo node that stands for the whole output mix.
const SystemPID uint32 = 0

// Opener starts capturing one source into buf until the returned Closer is
// closed. pid == SystemPID means "everything that is playing".
type Opener func(pid uint32, buf *AudioBuffer) (io.Closer, error)

type routeSource struct {
	buf    *AudioBuffer
	closer io.Closer
}

// Router is the Windows counterpart of a PipeWire patchbay: the user links
// sources (apps, or the whole system mix) to the single Sonidex output and the
// router mixes exactly those into the stream. Links are kept while stopped
// and only capture while a stream is running.
//
// The system mix already contains every app, so linking it replaces any app
// links and linking an app replaces the system link (no doubled audio).
type Router struct {
	mu      sync.Mutex
	open    Opener
	links   map[uint32]*routeSource // nil value = linked but not capturing
	active  bool
	notify  func(string)
	scratch []byte
}

func NewRouter(open Opener) *Router {
	return &Router{open: open, links: map[uint32]*routeSource{}}
}

// SetNotify sets a callback for asynchronous problems (e.g. app gone).
func (r *Router) SetNotify(f func(string)) {
	r.mu.Lock()
	r.notify = f
	r.mu.Unlock()
}

func (r *Router) IsLinked(pid uint32) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.links[pid]
	return ok
}

func (r *Router) Linked() []uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]uint32, 0, len(r.links))
	for pid := range r.links {
		out = append(out, pid)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Link connects pid to the Sonidex output. Opening a capture can take a few
// hundred ms, so call it off the UI thread.
func (r *Router) Link(pid uint32) error {
	r.mu.Lock()
	if _, ok := r.links[pid]; ok {
		r.mu.Unlock()
		return nil
	}
	var drop []*routeSource
	for other, src := range r.links {
		if (pid == SystemPID) != (other == SystemPID) { // system <-> app are exclusive
			if src != nil {
				drop = append(drop, src)
			}
			delete(r.links, other)
		}
	}
	active := r.active
	r.links[pid] = nil
	r.mu.Unlock()

	for _, s := range drop {
		_ = s.closer.Close()
	}
	if !active {
		return nil
	}
	return r.openLink(pid)
}

func (r *Router) openLink(pid uint32) error {
	buf := NewAudioBuffer(receiverBufferBytes)
	c, err := r.open(pid, buf)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, still := r.links[pid]; !still || !r.active {
		if c != nil {
			_ = c.Close()
		}
		return nil
	}
	if err != nil {
		delete(r.links, pid)
		return fmt.Errorf("link %d: %w", pid, err)
	}
	r.links[pid] = &routeSource{buf: buf, closer: c}
	return nil
}

func (r *Router) Unlink(pid uint32) {
	r.mu.Lock()
	src := r.links[pid]
	delete(r.links, pid)
	r.mu.Unlock()
	if src != nil {
		_ = src.closer.Close()
	}
}

// Start begins capturing every linked source.
func (r *Router) Start() {
	r.mu.Lock()
	r.active = true
	pids := make([]uint32, 0, len(r.links))
	for pid, s := range r.links {
		if s == nil {
			pids = append(pids, pid)
		}
	}
	notify := r.notify
	r.mu.Unlock()
	for _, pid := range pids {
		if err := r.openLink(pid); err != nil && notify != nil {
			notify(err.Error())
		}
	}
}

// Stop releases all captures but keeps the links.
func (r *Router) Stop() {
	r.mu.Lock()
	r.active = false
	var srcs []*routeSource
	for pid, s := range r.links {
		if s != nil {
			srcs = append(srcs, s)
			r.links[pid] = nil
		}
	}
	r.mu.Unlock()
	for _, s := range srcs {
		_ = s.closer.Close()
	}
}

// Mix fills dst (S16 LE stereo) with the sum of all linked sources; silence
// when nothing is linked or playing.
func (r *Router) Mix(dst []byte) {
	clear(dst)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.scratch) < len(dst) {
		r.scratch = make([]byte, len(dst))
	}
	tmp := r.scratch[:len(dst)]
	for _, s := range r.links {
		if s == nil {
			continue
		}
		// Bound latency: if a source ran ahead, drop the oldest audio.
		if over := s.buf.Len() - 3*len(dst); over > 0 {
			s.buf.Discard(over &^ 3)
		}
		n := s.buf.Pop(tmp) &^ 3
		for i := 0; i+1 < n; i += 2 {
			a := int32(int16(uint16(dst[i]) | uint16(dst[i+1])<<8))
			b := int32(int16(uint16(tmp[i]) | uint16(tmp[i+1])<<8))
			v := a + b
			if v > 32767 {
				v = 32767
			} else if v < -32768 {
				v = -32768
			}
			dst[i] = byte(v)
			dst[i+1] = byte(v >> 8)
		}
	}
}
