//go:build sdl

package main

import (
	"fmt"
	"os"

	"github.com/phroun/kittytk/objects/trinkets"
	sdlplat "github.com/phroun/kittytk/sdl"
)

// The graphical demo: the SAME demo application, rendered as pixels
// in an SDL window (selection by binary, D23/O3). Window > New
// Window opens PurfecTerm running a real shell on the full graphical
// terminal path.
//
//	go run -tags sdl ./examples/demo
func main() {
	plat := sdlplat.New("KittyTK demo", 1280, 800)
	plat.SetScale(2) // 2x font/cell size for now (per owner request)
	pixelBackend, err := plat.EnsureBackend()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	desktop := trinkets.NewDesktop()
	desktop.SetBackend(pixelBackend)

	buildDemo(desktop)
	os.Exit(desktop.RunOn(plat))
}
