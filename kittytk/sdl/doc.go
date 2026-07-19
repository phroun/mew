// Package sdl is the first graphical substrate (D23/O1): a
// platform.Platform that opens an SDL2 window, blits the raster
// package's framebuffer each frame, and translates SDL input into
// KittyTK events using the D3 key nomenclature.
//
// Build with:
//
//	go build -tags sdl ./...
//
// Requires SDL2 development libraries (libsdl2-dev) and cgo. Without
// the tag this package contains no implementation, keeping TUI-only
// builds cgo-free (D23/O3: backends are selected by choosing which
// display-service binary to run, not by app-level switches).
package sdl
