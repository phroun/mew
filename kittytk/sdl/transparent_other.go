//go:build sdl && !darwin && !windows

package sdl

import (
	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// platformPerPixelAlpha: no per-pixel window alpha off macOS; rounded
// borderless surfaces fall back to SDL shaped windows (X11).
const platformPerPixelAlpha = false

// makeWindowTransparent is the non-macOS stub.
func makeWindowTransparent(*sdl3.Window) bool { return false }

// makeWindowMiniaturizable is the non-macOS stub: SDL's plain
// Minimize is the best available.
func makeWindowMiniaturizable(*sdl3.Window) {}

// setWindowShadow is a no-op on X11: there is no portable OS drop shadow to
// toggle from here (the window manager owns whatever shadow a window gets).
func setWindowShadow(*sdl3.Window, bool) {}
