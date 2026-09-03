package backend

import (
	"sync"
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
