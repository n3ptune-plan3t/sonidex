//go:build windows

package backend

/*
#cgo LDFLAGS: -lole32 -lmmdevapi -lwinmm -luuid
#include <stdlib.h>
#include "wasapi_windows.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"unsafe"

	"github.com/gen2brain/malgo"
)

// AudioSession describes an app that currently has an audio session.
type AudioSession struct {
	PID    uint32
	Name   string
	Active bool
	Peak   float32
}

func ListAudioSessions() ([]AudioSession, error) {
	var raw [128]C.SxSession
	n := int(C.sx_list_sessions(&raw[0], C.int(len(raw))))
	if n < 0 {
		return nil, fmt.Errorf("enumerate audio sessions: 0x%08x", uint32(int32(n)))
	}
	out := make([]AudioSession, 0, n)
	for i := 0; i < n; i++ {
		s := raw[i]
		out = append(out, AudioSession{
			PID:    uint32(s.pid),
			Name:   C.GoString(&s.name[0]),
			Active: s.active != 0,
			Peak:   float32(s.peak),
		})
	}
	return out, nil
}

func beginHighResTimer() func() {
	C.sx_timer_begin()
	return func() { C.sx_timer_end() }
}

// NewPlatformRouter builds the Windows patchbay router: per-app capture via
// WASAPI process loopback, system mix via the existing miniaudio loopback.
func NewPlatformRouter() *Router { return NewRouter(openWindowsSource) }

func openWindowsSource(pid uint32, buf *AudioBuffer) (io.Closer, error) {
	if pid == SystemPID {
		return openSystemLoopback(buf)
	}
	return openProcessLoopback(pid, buf)
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func openSystemLoopback(buf *AudioBuffer) (io.Closer, error) {
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}
	cfg := malgo.DefaultDeviceConfig(malgo.Loopback)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = 2
	cfg.SampleRate = sampleRate
	cfg.PeriodSizeInFrames = 480
	dev, err := malgo.InitDevice(mctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(_ []byte, in []byte, _ uint32) { buf.Push(in) },
	})
	if err != nil {
		_ = mctx.Uninit()
		mctx.Free()
		return nil, err
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		_ = mctx.Uninit()
		mctx.Free()
		return nil, err
	}
	var once sync.Once
	return closerFunc(func() error {
		once.Do(func() {
			dev.Uninit()
			_ = mctx.Uninit()
			mctx.Free()
		})
		return nil
	}), nil
}

func openProcessLoopback(pid uint32, buf *AudioBuffer) (io.Closer, error) {
	type result struct{ err error }
	opened := make(chan result, 1)
	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		// COM objects, event handle and every call must stay on one thread.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		var hr C.long
		c := C.sx_cap_open(C.ulong(pid), 1, &hr)
		if c == nil {
			opened <- result{processLoopbackError(int32(hr))}
			return
		}
		defer C.sx_cap_close(c)
		opened <- result{nil}

		tmp := (*C.uchar)(C.malloc(32768))
		defer C.free(unsafe.Pointer(tmp))
		view := unsafe.Slice((*byte)(unsafe.Pointer(tmp)), 32768)
		for {
			select {
			case <-stop:
				return
			default:
			}
			n := int(C.sx_cap_read(c, tmp, 32768, 20))
			if n < 0 {
				return // process gone / device invalidated
			}
			if n > 0 {
				buf.Push(view[:n])
			}
		}
	}()

	if r := <-opened; r.err != nil {
		<-done
		return nil, r.err
	}
	var once sync.Once
	return closerFunc(func() error {
		once.Do(func() { close(stop); <-done })
		return nil
	}), nil
}

func processLoopbackError(hr int32) error {
	switch uint32(hr) {
	case 0x80004001, 0x80070057, 0x88890008: // E_NOTIMPL, E_INVALIDARG, unsupported format
		return errors.New("per-app capture needs Windows 10 2004+ / Windows 11")
	}
	return fmt.Errorf("WASAPI process loopback failed (0x%08x)", uint32(hr))
}
