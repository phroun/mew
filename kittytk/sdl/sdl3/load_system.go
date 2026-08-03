//go:build sdl && !sdlembed

package sdl3

// System SDL3 (default): the host links nothing and opens the
// platform's installed libSDL3 at startup. Build with -tags sdlembed
// to embed SDL instead and ship a single self-contained binary.
const embeddedSDL = false

func loadEmbedded() error { return nil }
