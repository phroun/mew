//go:build sdl && darwin

package sdl

/*
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore
#include <objc/runtime.h>
#include <objc/message.h>

static void kittytk_layer_nonopaque(id layer) {
	if (!layer) {
		return;
	}
	((void (*)(id, SEL, signed char))objc_msgSend)(
		layer, sel_registerName("setOpaque:"), 0);
	id subs = ((id (*)(id, SEL))objc_msgSend)(layer, sel_registerName("sublayers"));
	if (subs) {
		unsigned long n = ((unsigned long (*)(id, SEL))objc_msgSend)(
			subs, sel_registerName("count"));
		unsigned long i;
		for (i = 0; i < n; i++) {
			id sub = ((id (*)(id, SEL, unsigned long))objc_msgSend)(
				subs, sel_registerName("objectAtIndex:"), i);
			((void (*)(id, SEL, signed char))objc_msgSend)(
				sub, sel_registerName("setOpaque:"), 0);
		}
	}
}

// kittytk_make_window_transparent flips an NSWindow (and every backing
// layer under its content view - SDL's Metal/GL surface lives on a
// SUBVIEW, not a sublayer) to non-opaque with a clear background so
// the framebuffer's alpha channel composites against whatever is
// behind the window. SDL2 has no portable per-pixel window alpha;
// this is the standard Cocoa-side arrangement for it.
static void kittytk_make_window_transparent(void *nswindow) {
	id win = (id)nswindow;
	if (!win) {
		return;
	}
	((void (*)(id, SEL, signed char))objc_msgSend)(
		win, sel_registerName("setOpaque:"), 0);
	id clear = ((id (*)(Class, SEL))objc_msgSend)(
		objc_getClass("NSColor"), sel_registerName("clearColor"));
	((void (*)(id, SEL, id))objc_msgSend)(
		win, sel_registerName("setBackgroundColor:"), clear);

	id view = ((id (*)(id, SEL))objc_msgSend)(win, sel_registerName("contentView"));
	if (!view) {
		return;
	}
	kittytk_layer_nonopaque(((id (*)(id, SEL))objc_msgSend)(view, sel_registerName("layer")));
	id subviews = ((id (*)(id, SEL))objc_msgSend)(view, sel_registerName("subviews"));
	if (subviews) {
		unsigned long n = ((unsigned long (*)(id, SEL))objc_msgSend)(
			subviews, sel_registerName("count"));
		unsigned long i;
		for (i = 0; i < n; i++) {
			id sv = ((id (*)(id, SEL, unsigned long))objc_msgSend)(
				subviews, sel_registerName("objectAtIndex:"), i);
			kittytk_layer_nonopaque(((id (*)(id, SEL))objc_msgSend)(sv, sel_registerName("layer")));
		}
	}
}

// kittytk_enable_miniaturize adds NSWindowStyleMaskMiniaturizable
// (1 << 2) to a borderless window's style mask: without it Cocoa
// silently refuses to miniaturize borderless windows, so torn-off
// windows couldn't go to the Dock.
static void kittytk_enable_miniaturize(void *nswindow) {
	id win = (id)nswindow;
	if (!win) {
		return;
	}
	unsigned long mask = ((unsigned long (*)(id, SEL))objc_msgSend)(
		win, sel_registerName("styleMask"));
	((void (*)(id, SEL, unsigned long))objc_msgSend)(
		win, sel_registerName("setStyleMask:"), mask|(1UL<<2));
}
*/
import "C"

import (
	"unsafe"

	sdl2 "github.com/veandco/go-sdl2/sdl"
)

// platformPerPixelAlpha: macOS composites per-pixel window alpha via
// the Cocoa shim, so rounded borderless surfaces skip SDL's shaped-
// window machinery entirely.
const platformPerPixelAlpha = true

// makeWindowTransparent enables per-pixel window alpha on macOS.
// Call after the renderer exists (its layer must be reachable).
func makeWindowTransparent(win *sdl2.Window) bool {
	cocoa := cocoaWindow(win)
	if cocoa == nil {
		return false
	}
	C.kittytk_make_window_transparent(cocoa)
	return true
}

// makeWindowMiniaturizable lets a borderless window go to the Dock.
func makeWindowMiniaturizable(win *sdl2.Window) {
	if cocoa := cocoaWindow(win); cocoa != nil {
		C.kittytk_enable_miniaturize(cocoa)
	}
}

func cocoaWindow(win *sdl2.Window) unsafe.Pointer {
	info, err := win.GetWMInfo()
	if err != nil {
		return nil
	}
	cocoa := info.GetCocoaInfo()
	if cocoa == nil {
		return nil
	}
	return cocoa.Window
}
