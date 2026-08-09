//go:build sdl && sdlembed

package sdl3

import "github.com/Zyko0/go-sdl3/bin/binsdl"

// Embedded SDL3 (-tags sdlembed): libSDL3 is compiled INTO the binary,
// so the host ships as one file with nothing to install. purego
// resolves symbols through dlopen and cannot link statically, so the
// embedded copy is unpacked to a temp directory at startup and opened
// from there — self-contained in distribution terms, though not a true
// static link. Costs ~1MB of binary and one extraction per run.
const embeddedSDL = true

func loadEmbedded() error {
	binsdl.Load() // fatals internally on failure
	return nil
}
