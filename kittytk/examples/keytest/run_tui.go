//go:build !sdl

package main

import (
	"github.com/phroun/kittytk/backend/tui"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
)

// attachBackend puts the viewer on the terminal backend — the same one `mew`
// (no tags) uses — and returns the runner for it, plus whatever can answer for
// the keyboard's modes, which here is the backend itself.
//
// Keys reach it as escape sequences from the outer terminal, decoded by
// direct-key-handler, so what this shows is what that terminal chose to send:
// release and repeat events appear only if the terminal implements the "kitty"
// keyboard protocol, because there is no other way to express them. The modes
// are the same story — Caps Lock is only known here because it rides in on a
// keystroke, so the line stays blank until one arrives.
func attachBackend(desktop *trinkets.Desktop) (func() int, core.ModeSource) {
	backend := tui.NewTUIBackend(tui.DefaultTUIOptions())
	desktop.SetBackend(backend)
	return desktop.Run, backend
}
