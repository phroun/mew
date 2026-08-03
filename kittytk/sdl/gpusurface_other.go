//go:build sdl && !darwin

package sdl

import (
	"fmt"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// reassertWindowAlpha is the non-macOS stub: no per-pixel window alpha
// arrangement to maintain (see the darwin build for what this does).
func reassertWindowAlpha(*sdl3.Window) {}

// roundWindowLayer is the non-macOS stub: no Core Animation layer to
// round. The shape comes from the framebuffer's cleared corners, which
// needs no window-system help.
func roundWindowLayer(*sdl3.Window, int) bool { return false }

// nativeSurfaceHandles resolves the platform handles WebGPU surface
// creation needs for one SDL window. SDL3 replaced SDL_GetWindowWMInfo
// with typed window properties: X11 wants (Display*, Window) and
// Windows wants (0, HWND). A Wayland session reaches here through
// XWayland; a pure-Wayland window reports unsupported rather than
// being guessed at.
func nativeSurfaceHandles(win *sdl3.Window) (display, window uintptr, err error) {
	if d, w := win.X11Handles(); w != 0 {
		return d, w, nil
	}
	if hwnd := win.Win32HWND(); hwnd != 0 {
		return 0, hwnd, nil
	}
	return 0, 0, fmt.Errorf("no supported native window handle for WebGPU surface")
}
