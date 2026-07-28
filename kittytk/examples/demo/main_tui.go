//go:build !sdl

package main

import (
	"github.com/phroun/kittytk/backend/tui"
	"github.com/phroun/kittytk/objects/trinkets"
)

// The text-mode demo: the classic TUI desktop in your terminal.
//
//	go run ./examples/demo
func main() {
	opts := tui.DefaultTUIOptions()
	tuiBackend := tui.NewTUIBackend(opts)

	desktop := trinkets.NewDesktop()
	desktop.SetBackend(tuiBackend)

	buildDemo(desktop)
	desktop.Run()
}
