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

const maxBufferedBytes = ChunkSize * 4

// Push appends p to the jitter buffer, then trims from the front if the
// buffer has grown past maxBufferedBytes. Previously this trimmed len(p)
// bytes *before* appending len(p) bytes back, which is a net-zero change -
// once the buffer exceeded the threshold once (e.g. from a network burst or
// a slow playback callback), it never actually shrank again and buffered
// latency would just accumulate for the life of the stream.
func (b *AudioBuffer) Push(p []byte) {
	b.Lock()
	defer b.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > maxBufferedBytes {
		b.data = b.data[len(b.data)-maxBufferedBytes:]
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
