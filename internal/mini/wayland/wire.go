//go:build linux

package wayland

import (
	"encoding/binary"
	"strings"
)

// The generated go-wayland requests write a string's length field as the
// *padded* length, so "wl_compositor" goes out as a 16-byte string with
// extra NULs inside it. libwayland tolerates that; strict servers (niri /
// smithay) reject it with "invalid arguments for wl_registry#2.bind".
// The spec says: length counts the string plus exactly one terminating NUL,
// followed by zero padding to a multiple of four. The few requests that
// carry strings are encoded here instead.

func putU32(b []byte, v uint32) { binary.NativeEndian.PutUint32(b, v) }

// appendString appends a wire string (length, bytes, NUL, padding).
func appendString(b []byte, s string) []byte {
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	n := len(s) + 1
	var l [4]byte
	putU32(l[:], uint32(n))
	b = append(b, l[:]...)
	b = append(b, s...)
	b = append(b, 0)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// finish writes the header (object id, size<<16|opcode) into b[0:8].
func finish(b []byte, obj uint32, opcode uint16) []byte {
	putU32(b[0:], obj)
	putU32(b[4:], uint32(len(b))<<16|uint32(opcode))
	return b
}

// bindRequest is wl_registry.bind(name, interface, version, new_id).
func bindRequest(registry, name uint32, iface string, version, id uint32) []byte {
	b := make([]byte, 8, 48)
	var u [4]byte
	putU32(u[:], name)
	b = append(b, u[:]...)
	b = appendString(b, iface)
	putU32(u[:], version)
	b = append(b, u[:]...)
	putU32(u[:], id)
	b = append(b, u[:]...)
	return finish(b, registry, 0)
}

// stringRequest is a request whose only argument is one string
// (xdg_toplevel.set_title = opcode 2, set_app_id = opcode 3).
func stringRequest(obj uint32, opcode uint16, s string) []byte {
	b := appendString(make([]byte, 8, 48), s)
	return finish(b, obj, opcode)
}
