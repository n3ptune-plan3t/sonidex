package backend

import (
	"context"
	"io"
	"runtime"
	"strings"
	"sync"

	"github.com/gen2brain/malgo"
)

type AudioBuffer struct {
	mu       sync.Mutex
	buf      []byte
	head     int
	tail     int
	size     int
	capacity int
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func NewAudioBuffer(capacity int) *AudioBuffer {
	return &AudioBuffer{
		buf:      make([]byte, capacity),
		capacity: capacity,
	}
}

func (b *AudioBuffer) Push(p []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if n == 0 {
		return
	}
	if n > b.capacity {
		p = p[n-b.capacity:]
		n = b.capacity
	}
	overflow := (b.size + n) - b.capacity
	if overflow > 0 {
		b.tail = (b.tail + overflow) % b.capacity
		b.size -= overflow
	}
	firstChunk := minInt(n, b.capacity-b.head)
	copy(b.buf[b.head:b.head+firstChunk], p[:firstChunk])
	if secondChunk := n - firstChunk; secondChunk > 0 {
		copy(b.buf[:secondChunk], p[firstChunk:])
	}
	b.head = (b.head + n) % b.capacity
	b.size += n
}

func (b *AudioBuffer) Pop(p []byte) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.size == 0 {
		return 0
	}
	toRead := minInt(len(p), b.size)
	firstChunk := minInt(toRead, b.capacity-b.tail)
	copy(p[:firstChunk], b.buf[b.tail:b.tail+firstChunk])
	if secondChunk := toRead - firstChunk; secondChunk > 0 {
		copy(p[firstChunk:toRead], b.buf[:secondChunk])
	}
	b.tail = (b.tail + toRead) % b.capacity
	b.size -= toRead
	return toRead
}

func (b *AudioBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.size
}

func (b *AudioBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.head = 0
	b.tail = 0
	b.size = 0
}

func ListCaptureSources() ([]string, error) {
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = mctx.Uninit()
		mctx.Free()
	}()
	infos, err := mctx.Devices(malgo.Capture)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(infos))
	for i := range infos {
		names = append(names, infos[i].Name())
	}
	return names, nil
}

func StartDesktopStream(ctx context.Context, addr string) error {
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = mctx.Uninit()
		mctx.Free()
	}()
	var cfg malgo.DeviceConfig
	if runtime.GOOS == "windows" {
		cfg = malgo.DefaultDeviceConfig(malgo.Loopback)
	} else {
		cfg = malgo.DefaultDeviceConfig(malgo.Capture)
		var selectedID malgo.DeviceID
		haveSelected := false
		if infos, derr := mctx.Devices(malgo.Capture); derr == nil {
			for i := range infos {
				if strings.Contains(strings.ToLower(infos[i].Name()), "monitor") {
					selectedID = infos[i].ID
					haveSelected = true
					break
				}
			}
		}
		if haveSelected {
			cfg.Capture.DeviceID = selectedID.Pointer()
		}
	}
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = 2
	cfg.SampleRate = 48000
	pr, pw := io.Pipe()
	defer func() {
		_ = pr.Close()
		_ = pw.Close()
	}()
	go func() {
		<-ctx.Done()
		_ = pw.Close()
		_ = pr.Close()
	}()
	onRecv := func(_ []byte, pInput []byte, _ uint32) {
		if len(pInput) == 0 {
			return
		}
		if ctx.Err() != nil {
			return
		}
		buf := make([]byte, len(pInput))
		copy(buf, pInput)
		_, _ = pw.Write(buf)
	}
	device, err := malgo.InitDevice(mctx.Context, cfg, malgo.DeviceCallbacks{Data: onRecv})
	if err != nil {
		return err
	}
	defer device.Uninit()
	if err := device.Start(); err != nil {
		return err
	}
	senderDone := make(chan error, 1)
	go func() {
		senderDone <- StartTCPSender(ctx, addr, pr)
	}()
	select {
	case <-ctx.Done():
		return nil
	case err := <-senderDone:
		return err
	}
}

func StartReceiverWithPlayback(ctx context.Context, port string) error {
	ab := NewAudioBuffer(384000)
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = mctx.Uninit()
		mctx.Free()
	}()
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatS16
	cfg.Playback.Channels = 2
	cfg.SampleRate = 48000
	onSend := func(pOutput []byte, _ []byte, _ uint32) {
		n := ab.Pop(pOutput)
		for i := n; i < len(pOutput); i++ {
			pOutput[i] = 0
		}
	}
	device, err := malgo.InitDevice(mctx.Context, cfg, malgo.DeviceCallbacks{Data: onSend})
	if err != nil {
		return err
	}
	defer device.Uninit()
	if err := device.Start(); err != nil {
		return err
	}
	return StartTCPReceiver(ctx, port, ab, 1920)
}
