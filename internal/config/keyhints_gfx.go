//go:build sdl

package config

// hostIsGraphical is what the (gfx) / (con) hints are tested against. No binary
// is both, so the answer is a property of the build rather than something to
// detect: sdl is the graphical host's tag (see the Makefile's SDL_TAGS), and
// every other build draws characters.
const hostIsGraphical = true
