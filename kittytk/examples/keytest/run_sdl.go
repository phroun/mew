//go:build sdl

package main

import (
	"fmt"
	"os"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
	sdlplat "github.com/phroun/kittytk/sdl"
)

// attachBackend puts the viewer on the graphical backend — the same one
// `mew-sdl` uses — and returns the runner for it.
//
// Keys arrive as native window-system events rather than escape sequences, so
// this is the side to compare against the terminal build: the same keystroke
// can be reported differently by the two, and a difference between them is a
// fact about the backends rather than about anything below.
func attachBackend(desktop *trinkets.Desktop) func() int {
	plat, err := sdlplat.New("KittyTK event viewer", 1000, 700, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "keytest: %v\n", err)
		os.Exit(1)
	}
	backend, err := plat.EnsureBackend()
	if err != nil {
		fmt.Fprintf(os.Stderr, "keytest: %v\n", err)
		os.Exit(1)
	}
	desktop.SetBackend(backend) // seeds root metrics from the raster font
	desktop.SetFont(&core.Font{Name: "ui-text", Size: 12})
	return func() int { return desktop.RunOn(plat) }
}
