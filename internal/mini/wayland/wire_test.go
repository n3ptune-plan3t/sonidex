//go:build linux

package wayland

import (
	"bytes"
	"testing"
)

// A strict parser reads: length (incl. one NUL), then exactly that many
// bytes ending in a single NUL, then padding to 4.
func checkString(t *testing.T, b []byte, want string) []byte {
	t.Helper()
	n := int(readU32(b))
	if n != len(want)+1 {
		t.Fatalf("string length field = %d, want %d", n, len(want)+1)
	}
	if !bytes.Equal(b[4:4+n], append([]byte(want), 0)) || bytes.IndexByte(b[4:4+n-1], 0) >= 0 {
		t.Fatalf("string bytes %q", b[4:4+n])
	}
	padded := (n + 3) &^ 3
	for _, c := range b[4+n : 4+padded] {
		if c != 0 {
			t.Fatal("non-zero padding")
		}
	}
	return b[4+padded:]
}

func readU32(b []byte) uint32 {
	var v [4]byte
	copy(v[:], b)
	return uint32(v[0]) | uint32(v[1])<<8 | uint32(v[2])<<16 | uint32(v[3])<<24
}

func TestBindRequest(t *testing.T) {
	for _, iface := range []string{"wl_compositor", "wl_shm", "xdg_wm_base", "zxdg_decoration_manager_v1", "wl_output"} {
		m := bindRequest(2, 7, iface, 4, 9)
		if got := int(readU32(m[4:]) >> 16); got != len(m) || len(m)%4 != 0 {
			t.Fatalf("%s: size field %d, len %d", iface, got, len(m))
		}
		rest := checkString(t, m[12:], iface)
		if len(rest) != 8 || readU32(rest) != 4 || readU32(rest[4:]) != 9 {
			t.Fatalf("%s: bad version/new_id tail %v", iface, rest)
		}
	}
}

func TestStringRequest(t *testing.T) {
	for _, s := range []string{"", "a", "sonidex", "Sonidex Streamer", "abcdefghijk"} {
		m := stringRequest(5, 2, s)
		if int(readU32(m[4:])>>16) != len(m) || len(m)%4 != 0 {
			t.Fatalf("%q: bad size", s)
		}
		if rest := checkString(t, m[8:], s); len(rest) != 0 {
			t.Fatalf("%q: trailing bytes", s)
		}
	}
}
