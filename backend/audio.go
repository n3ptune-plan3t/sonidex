package backend

import (
	"sync"

	"github.com/gen2brain/malgo"
)

const (
	SampleRate = 48000
	Channels   = 2
	Format     = malgo.FormatS16
	ChunkSize  = 1920
)

type AudioBuffer struct {
	sync.Mutex
	data []byte
}

func (b *AudioBuffer) Push(p []byte) {
	b.Lock()
	defer b.Unlock()
	if len(b.data) > ChunkSize*2 {
		b.data = b.data[len(p):]
	}
	b.data = append(b.data, p...)
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
