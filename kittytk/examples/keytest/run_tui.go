//go:build !sdl

package main

import (
	"github.com/phroun/kittytk/backend/tui"
	"github.com/phroun/kittytk/objects/trinkets"
)

// attachBackend puts the viewer on the terminal backend — the same one `mew`
// (no tags) uses — and returns the runner for it.
//
// Keys reach it as escape sequences from the outer terminal, decoded by
// direct-key-handler, so what this shows is what that terminal chose to send:
// release and repeat events appear only if the terminal implements the kitty
// keyboard protocol, because there is no other way to express them.
func attachBackend(desktop *trinkets.Desktop) func() int {
	desktop.SetBackend(tui.NewTUIBackend(tui.DefaultTUIOptions()))
	return desktop.Run
}
