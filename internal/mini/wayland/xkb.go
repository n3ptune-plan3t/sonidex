//go:build linux

package wayland

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>

// libxkbcommon is loaded at run time, so the binary has no link-time
// dependency on it (and no headers are needed to build): X11-only machines
// that lack it still run, and only Wayland keyboard input needs it.
static void *xkb_lib;
static void *(*f_ctx_new)(int);
static void *(*f_km_new)(void *, const char *, int, int);
static void (*f_km_unref)(void *);
static void *(*f_st_new)(void *);
static void (*f_st_unref)(void *);
static int (*f_st_update)(void *, uint32_t, uint32_t, uint32_t, uint32_t, uint32_t, uint32_t);
static uint32_t (*f_sym)(void *, uint32_t);
static uint32_t (*f_utf32)(void *, uint32_t);
static int (*f_repeats)(void *, uint32_t);
static int (*f_mod)(void *, const char *, int);

static int sx_xkb_open(void) {
	if (xkb_lib) return 1;
	xkb_lib = dlopen("libxkbcommon.so.0", RTLD_NOW);
	if (!xkb_lib) return 0;
	*(void **)(&f_ctx_new) = dlsym(xkb_lib, "xkb_context_new");
	*(void **)(&f_km_new) = dlsym(xkb_lib, "xkb_keymap_new_from_string");
	*(void **)(&f_km_unref) = dlsym(xkb_lib, "xkb_keymap_unref");
	*(void **)(&f_st_new) = dlsym(xkb_lib, "xkb_state_new");
	*(void **)(&f_st_unref) = dlsym(xkb_lib, "xkb_state_unref");
	*(void **)(&f_st_update) = dlsym(xkb_lib, "xkb_state_update_mask");
	*(void **)(&f_sym) = dlsym(xkb_lib, "xkb_state_key_get_one_sym");
	*(void **)(&f_utf32) = dlsym(xkb_lib, "xkb_state_key_get_utf32");
	*(void **)(&f_repeats) = dlsym(xkb_lib, "xkb_keymap_key_repeats");
	*(void **)(&f_mod) = dlsym(xkb_lib, "xkb_state_mod_name_is_active");
	if (!f_ctx_new || !f_km_new || !f_km_unref || !f_st_new || !f_st_unref || !f_st_update ||
	    !f_sym || !f_utf32 || !f_repeats || !f_mod) {
		xkb_lib = 0;
		return 0;
	}
	return 1;
}
static void *sx_context(void) { return f_ctx_new(0); }                 // XKB_CONTEXT_NO_FLAGS
static void *sx_keymap(void *ctx, const char *s) { return f_km_new(ctx, s, 1, 0); } // TEXT_V1
static void sx_keymap_free(void *k) { f_km_unref(k); }
static void *sx_state(void *k) { return f_st_new(k); }
static void sx_state_free(void *s) { f_st_unref(s); }
static void sx_update(void *s, uint32_t d, uint32_t l, uint32_t lk, uint32_t g) { f_st_update(s, d, l, lk, 0, 0, g); }
static uint32_t sx_sym(void *s, uint32_t k) { return f_sym(s, k); }
static uint32_t sx_utf32(void *s, uint32_t k) { return f_utf32(s, k); }
static int sx_repeats(void *k, uint32_t key) { return f_repeats(k, key); }
static int sx_mod(void *s, const char *n) { return f_mod(s, n, 8); }     // XKB_STATE_MODS_EFFECTIVE
*/
import "C"

import (
	"errors"
	"unsafe"
)

type xkb struct {
	ctx, keymap, state unsafe.Pointer
}

func newXkb() (*xkb, error) {
	if C.sx_xkb_open() == 0 {
		return nil, errors.New("libxkbcommon.so.0 not found")
	}
	ctx := C.sx_context()
	if ctx == nil {
		return nil, errors.New("xkb_context_new failed")
	}
	return &xkb{ctx: ctx}, nil
}

func (x *xkb) ready() bool { return x != nil && x.state != nil }

// setKeymap installs the compositor's keymap (xkb text format).
func (x *xkb) setKeymap(text string) error {
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	km := C.sx_keymap(x.ctx, cs)
	if km == nil {
		return errors.New("xkb: bad keymap")
	}
	st := C.sx_state(km)
	if st == nil {
		C.sx_keymap_free(km)
		return errors.New("xkb: no state")
	}
	if x.state != nil {
		C.sx_state_free(x.state)
		C.sx_keymap_free(x.keymap)
	}
	x.keymap, x.state = km, st
	return nil
}

func (x *xkb) updateMask(dep, lat, lock, group uint32) {
	if x.ready() {
		C.sx_update(x.state, C.uint32_t(dep), C.uint32_t(lat), C.uint32_t(lock), C.uint32_t(group))
	}
}

// sym and utf32 take an xkb keycode (evdev code + 8).
func (x *xkb) sym(kc uint32) uint32   { return uint32(C.sx_sym(x.state, C.uint32_t(kc))) }
func (x *xkb) utf32(kc uint32) uint32 { return uint32(C.sx_utf32(x.state, C.uint32_t(kc))) }
func (x *xkb) repeats(kc uint32) bool { return C.sx_repeats(x.keymap, C.uint32_t(kc)) != 0 }

func (x *xkb) mod(name string) bool {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return C.sx_mod(x.state, cs) > 0
}
