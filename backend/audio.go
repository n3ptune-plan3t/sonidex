package backend

import (
	"sync"

	"github.com/gen2brain/malgo"
)

const (
	SampleRate = 48000
	Channels   = 2
	Format     = malgo.FormatS16
)

var LatencyPresets = map[string]uint32{
	"Ultra Low (5ms)": 240,
	"Low (10ms)":      480,
	"Safe (20ms)":     960,
}

const DefaultLatencyPreset = "Low (10ms)"

func BytesForPeriod(periodFrames uint32) int {
	return int(periodFrames) * Channels * malgo.SampleSizeInBytes(Format)
}

type AudioBuffer struct {
	sync.Mutex
	data []byte
	cap  int
}

func NewAudioBuffer(periodBytes int) *AudioBuffer {
	return &AudioBuffer{cap: periodBytes * 4}
}

func (b *AudioBuffer) Push(p []byte) {
	b.Lock()
	defer b.Unlock()
	b.data = append(b.data, p...)
	if b.cap > 0 && len(b.data) > b.cap {
		b.data = b.data[len(b.data)-b.cap:]
	}
}

func (b *AudioBuffer) Read(p []byte) int {
	b.Lock()
	defer b.Unlock()
	if len(b.data) == 0 {
		for i := range p {
			p[i] = 0
		}
		return len(p)
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n
}

func InitMalgo() (*malgo.AllocatedContext, error) {
	return malgo.InitContext(nil, malgo.ContextConfig{}, nil)
}
