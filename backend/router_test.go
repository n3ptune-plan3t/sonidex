package backend

import (
	"io"
	"sync"
	"testing"
)

type fakeCloser struct{ closed bool }

func (f *fakeCloser) Close() error { f.closed = true; return nil }

func s16(v int16, frames int) []byte {
	b := make([]byte, frames*4)
	for i := 0; i < len(b); i += 2 {
		b[i], b[i+1] = byte(uint16(v)), byte(uint16(v)>>8)
	}
	return b
}

func newFakeRouter() (*Router, map[uint32]*AudioBuffer, *sync.Map) {
	bufs := map[uint32]*AudioBuffer{}
	closers := &sync.Map{}
	r := NewRouter(func(pid uint32, b *AudioBuffer) (io.Closer, error) {
		bufs[pid] = b
		c := &fakeCloser{}
		closers.Store(pid, c)
		return c, nil
	})
	return r, bufs, closers
}

func TestRouterMixSumsAndClips(t *testing.T) {
	r, bufs, _ := newFakeRouter()
	r.Start()
	_ = r.Link(10)
	_ = r.Link(20)
	bufs[10].Push(s16(1000, 4))
	bufs[20].Push(s16(2000, 4))
	dst := make([]byte, 16)
	r.Mix(dst)
	if got := int16(uint16(dst[0]) | uint16(dst[1])<<8); got != 3000 {
		t.Fatalf("sum = %d", got)
	}
	bufs[10].Push(s16(30000, 4))
	bufs[20].Push(s16(30000, 4))
	r.Mix(dst)
	if got := int16(uint16(dst[0]) | uint16(dst[1])<<8); got != 32767 {
		t.Fatalf("clip = %d", got)
	}
}

func TestRouterSilentWhenNothingLinked(t *testing.T) {
	r, _, _ := newFakeRouter()
	r.Start()
	dst := s16(5, 4)
	r.Mix(dst)
	for _, b := range dst {
		if b != 0 {
			t.Fatal("expected silence")
		}
	}
}

func TestRouterSystemAndAppsAreExclusive(t *testing.T) {
	r, _, closers := newFakeRouter()
	r.Start()
	_ = r.Link(SystemPID)
	_ = r.Link(7)
	if r.IsLinked(SystemPID) || !r.IsLinked(7) {
		t.Fatal("linking an app must drop the system link")
	}
	if c, _ := closers.Load(SystemPID); !c.(*fakeCloser).closed {
		t.Fatal("system capture not closed")
	}
	_ = r.Link(SystemPID)
	if r.IsLinked(7) {
		t.Fatal("linking system must drop app links")
	}
}

func TestRouterLinksSurviveStopAndCaptureOnlyWhileActive(t *testing.T) {
	r, _, closers := newFakeRouter()
	_ = r.Link(5) // not started: intent only
	if _, ok := closers.Load(uint32(5)); ok {
		t.Fatal("must not capture while stopped")
	}
	r.Start()
	if _, ok := closers.Load(uint32(5)); !ok {
		t.Fatal("Start should open linked sources")
	}
	r.Stop()
	if c, _ := closers.Load(uint32(5)); !c.(*fakeCloser).closed {
		t.Fatal("Stop should close captures")
	}
	if !r.IsLinked(5) {
		t.Fatal("link must survive Stop")
	}
}

func TestRouterDropsOldAudioWhenSourceRunsAhead(t *testing.T) {
	r, bufs, _ := newFakeRouter()
	r.Start()
	_ = r.Link(1)
	bufs[1].Push(s16(100, 400))
	dst := make([]byte, 16)
	r.Mix(dst)
	if bufs[1].Len() > 3*16 {
		t.Fatalf("backlog not bounded: %d", bufs[1].Len())
	}
}
