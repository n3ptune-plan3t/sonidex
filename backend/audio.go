package backend

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/malgo"
)

// Profile trades end-to-end latency for host wakeups (and so battery).
// Every capture period is one wakeup of the audio server, this process and
// the network stack, so a bigger period means fewer wakeups and deeper CPU
// sleep states.
type Profile int

const (
	ProfileLowLatency   Profile = iota // 10 ms period, ~100 wakeups/s
	ProfileBalanced                    // 20 ms period, ~50 wakeups/s
	ProfileBatterySaver                // 40 ms period, ~25 wakeups/s
)

var ProfileNames = []string{
	"Low latency (10 ms)",
	"Balanced (20 ms)",
	"Battery saver (40 ms)",
}

func (p Profile) String() string {
	if int(p) >= 0 && int(p) < len(ProfileNames) {
		return ProfileNames[p]
	}
	return ProfileNames[ProfileLowLatency]
}

func (p Profile) Next() Profile { return (p + 1) % Profile(len(ProfileNames)) }

// ProfileFromName maps a UI label back to a Profile (unknown -> low latency).
func ProfileFromName(name string) Profile {
	for i, n := range ProfileNames {
		if n == name {
			return Profile(i)
		}
	}
	return ProfileLowLatency
}

func (p Profile) frames() uint32 {
	switch p {
	case ProfileBalanced:
		return 960
	case ProfileBatterySaver:
		return 1920
	default:
		return 480
	}
}

// periodSizeFrames: SONIDEX_PERIOD_FRAMES (if set) always wins, otherwise the
// profile decides. The receiver keeps using the low-latency default.
func periodSizeFrames(p Profile) uint32 {
	if v := strings.TrimSpace(os.Getenv("SONIDEX_PERIOD_FRAMES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return uint32(n)
		}
	}
	return p.frames()
}

// StreamOptions configures the desktop (streamer) side.
type StreamOptions struct {
	Profile Profile
	// SuppressSilence stops transmitting once the captured audio has been
	// digitally silent for a while and resumes instantly on sound. The
	// receiver already outputs zeros when its buffer runs dry, so no protocol
	// change is needed. Keeps the WiFi radio / USB link idle while nothing plays.
	SuppressSilence bool
}

func DefaultStreamOptions() StreamOptions {
	return StreamOptions{
		Profile:         ProfileLowLatency,
		SuppressSilence: os.Getenv("SONIDEX_SILENCE_SUPPRESS") != "0",
	}
}

const (
	// |sample| <= silenceThreshold counts as silence (~ -78 dBFS).
	silenceThreshold = 4
	// Keep sending this long after the last audible sample so track gaps and
	// fade-out tails are never chopped.
	silenceHangover = 750 * time.Millisecond
	sampleRate      = 48000
	bytesPerFrame   = 4 // S16 stereo
)

// isSilent reports whether every little-endian S16 sample in p is inaudible.
func isSilent(p []byte) bool {
	for i := 0; i+1 < len(p); i += 2 {
		v := int16(uint16(p[i]) | uint16(p[i+1])<<8)
		if v > silenceThreshold || v < -silenceThreshold {
			return false
		}
	}
	return true
}

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

// StartDesktopStream streams with the default options (kept for compatibility).
func StartDesktopStream(ctx context.Context, addr string) error {
	return StartDesktopStreamOpts(ctx, addr, DefaultStreamOptions())
}

func StartDesktopStreamOpts(ctx context.Context, addr string, opts StreamOptions) error {
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
	cfg.SampleRate = sampleRate
	cfg.PeriodSizeInFrames = periodSizeFrames(opts.Profile)

	const chanDepth = 16
	ch := make(chan []byte, chanDepth)
	// Recycled buffers: the capture callback runs every period, so allocating
	// there would churn the GC (and wake the runtime) for no reason.
	free := make(chan []byte, chanDepth*2)
	getBuf := func(n int) []byte {
		select {
		case b := <-free:
			if cap(b) >= n {
				return b[:n]
			}
		default:
		}
		return make([]byte, n)
	}
	release := func(b []byte) {
		select {
		case free <- b[:cap(b)]:
		default:
		}
	}

	// Only touched from the (single) miniaudio capture thread.
	var quietFor time.Duration
	onRecv := func(_ []byte, pInput []byte, _ uint32) {
		if len(pInput) == 0 || ctx.Err() != nil {
			return
		}
		if opts.SuppressSilence {
			if isSilent(pInput) {
				quietFor += time.Duration(len(pInput)/bytesPerFrame) * time.Second / sampleRate
				if quietFor > silenceHangover {
					return // nothing audible: send nothing, wake nobody
				}
			} else {
				quietFor = 0
			}
		}
		buf := getBuf(len(pInput))
		copy(buf, pInput)
		select {
		case ch <- buf:
		default:
			// Sender is behind: drop the oldest chunk to bound latency.
			select {
			case old := <-ch:
				release(old)
			default:
			}
			select {
			case ch <- buf:
			default:
				release(buf)
			}
		}
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
		senderDone <- startTCPSender(ctx, addr, ch, release)
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
	cfg.PeriodSizeInFrames = periodSizeFrames(ProfileLowLatency)
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
