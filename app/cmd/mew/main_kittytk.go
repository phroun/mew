//go:build kittytk

// Command mew, KittyTK host build (-tags kittytk).
//
// This build fronts the same mew editor with a KittyTK TUI host: it opens a
// maximized window holding a root-level mew editor and serves the KittyTK
// protocol, so other apps can connect and embed their own (sub-)mew editor
// trinkets. The plain build (no tags) keeps mew driving the terminal directly
// and remains the reference for comparison.
//
// The host itself lives in internal/mewhost (backend-agnostic, protocol-built);
// this file only sets up the terminal backend and runs it. mew-sdl is the
// graphical twin. Built with -tags mew as well (`go build -tags "kittytk mew"`),
// the root `editor` resolves to the real mew-backed trinket; without it, to the
// vanilla placeholder.
package main

import (
	"fmt"
	"os"

	"github.com/phroun/kittytk/backend/tui"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/mew"
	"github.com/phroun/mew-app/internal/mewhost"
)

const usage = `mew edits words (KittyTK host build)

Usage:
  mew [options] [file ...]

This build hosts mew inside a maximized KittyTK TUI window and serves the
KittyTK protocol, so other apps can connect and embed their own mew editor
trinkets. The command line is mew's own: switches, +N, and files apply as in
the plain build (one editor, first file focused, the rest as background
buffers).

  -v, --version           print version and exit
  -h, --help              print this help and exit

Build the standalone terminal editor without -tags kittytk.
`

func main() {
	launchArgs, wantVersion, wantHelp := mewhost.SplitArgs(os.Args[1:])
	switch {
	case wantVersion:
		fmt.Printf("mew %s (kittytk host)\n", mew.FullVersion())
		return
	case wantHelp:
		fmt.Print(usage)
		return
	}

	// Launch config comes from the launching user's ~/.mew/editor.conf [kittytk]
	// section (not the standalone-KittyTK kittytk.ini): the service endpoint/
	// token plus the terminal host's native/clipboard knobs. Env vars still win.
	cfg := mewhost.LoadHostConfig()
	core.SetMacNativeShortcuts(cfg.UseTUIMacNativeShortcuts())

	tuiOpts := tui.DefaultTUIOptions()
	tuiOpts.OSC52Clipboard = cfg.UseTUIOSC52Clipboard()
	tuiOpts.OSC52Paste = cfg.UseTUIOSC52Paste()

	// Free the host's built-in accelerators before the desktop is created: the Ψ
	// system menu is built inside NewDesktop and never rebuilt, so its Exit
	// Desktop shortcut must be cleared first.
	mewhost.ClearHostShortcuts()

	desktop := trinkets.NewDesktop()
	desktop.SetBackend(tui.NewTUIBackend(tuiOpts))

	// TUI host: show_desktop/hide_desktop toggle multi-window (graphical=false).
	mewhost.BuildHost(desktop, cfg, launchArgs, false)
	os.Exit(desktop.Run())
}
