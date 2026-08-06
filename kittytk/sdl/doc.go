// Package sdl is the first graphical substrate (D23/O1): a
// platform.Platform that opens an SDL3 window, blits the raster
// package's framebuffer each frame, and translates SDL input into
// KittyTK events using the D3 key nomenclature.
//
// Build with:
//
//	go build -tags sdl ./...
//
// Requires libSDL3 at RUN time; nothing is linked at build time and no
// SDL headers are needed. Without the tag this package contains no
// implementation, keeping TUI-only builds cgo-free (D23/O3: backends are selected by choosing which
// display-service binary to run, not by app-level switches).
//
// The host owns a live font zoom (a GUI-only affordance — the TUI has no
// say over its terminal's font): Command on macOS, or the Meta/Windows
// key elsewhere, with "+"/"=" grows the font a point, "-" shrinks it, and
// "0" restores the configured font_size; the dynamic size is bounded to
// 4..100pt. The chords are consumed by the platform and never reach the
// application as key events.
package sdl
