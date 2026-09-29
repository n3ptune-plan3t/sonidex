package backend

import "testing"

func TestIsSilent(t *testing.T) {
	if !isSilent(make([]byte, 1920)) {
		t.Fatal("zeros should be silent")
	}
	b := make([]byte, 1920)
	b[1000], b[1001] = 0x10, 0x00 // sample value 16 > threshold
	if isSilent(b) {
		t.Fatal("audible sample should not be silent")
	}
	b[1000], b[1001] = 0xFE, 0xFF // -2, within threshold
	if !isSilent(b) {
		t.Fatal("-2 should count as silent")
	}
}

func TestAudioBufferWrap(t *testing.T) {
	ab := NewAudioBuffer(8)
	ab.Push([]byte{1, 2, 3, 4, 5, 6})
	out := make([]byte, 4)
	ab.Pop(out)
	ab.Push([]byte{7, 8, 9, 10})
	all := make([]byte, 8)
	n := ab.Pop(all)
	want := []byte{5, 6, 7, 8, 9, 10}
	if n != len(want) {
		t.Fatalf("got %d bytes", n)
	}
	for i := range want {
		if all[i] != want[i] {
			t.Fatalf("idx %d: %d != %d", i, all[i], want[i])
		}
	}
}
