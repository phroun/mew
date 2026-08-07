//go:build sdl && windows

package sdl

import (
	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// platformPerPixelAlpha: Windows has no per-pixel window alpha through this
// path; rounded borderless surfaces use SDL shaped windows (SetShape), like
// X11.
const platformPerPixelAlpha = false

// makeWindowTransparent is the Windows stub: the shaped-window path (SetShape
// on a window created WINDOW_TRANSPARENT) carries the rounding, so there is no
// separate per-pixel arrangement to make here.
func makeWindowTransparent(*sdl3.Window) bool { return false }

// makeWindowMiniaturizable is the Windows stub: SDL's plain Minimize works.
func makeWindowMiniaturizable(*sdl3.Window) {}

// setWindowShadow is a no-op on Windows for now. The obvious low-risk form —
// DwmExtendFrameIntoClientArea to make DWM cast a shadow — pulls the window
// frame a pixel into the client area, which visibly shrank the content on
// undock. A borderless window that keeps its exact size AND gets a DWM shadow
// needs a WM_NCCALCSIZE window-procedure subclass (reclaim the non-client area
// while leaving the extended frame for the shadow); that is the deliberate
// follow-up. Doing nothing keeps the window's size exact in the meantime.
func setWindowShadow(*sdl3.Window, bool) {}
