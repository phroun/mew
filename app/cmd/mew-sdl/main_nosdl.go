//go:build !sdl

// mew-sdl without the sdl build tag has no graphical backend to run: the SDL
// platform (and its cgo/SDL2 dependency) is only compiled under -tags sdl, so a
// tag-less build stays cgo-free. This stub explains how to build the real host.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "mew-sdl needs the SDL backend: build with -tags sdl (and -tags mew for the real editor):")
	fmt.Fprintln(os.Stderr, `  go run -tags "sdl mew" ./app/cmd/mew-sdl [file ...]`)
	os.Exit(1)
}
