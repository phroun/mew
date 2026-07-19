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
	"os/exec"
	"path/filepath"
	"runtime"

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
      --window            open in a graphical window instead (hands off to mew-sdl)

Build the standalone terminal editor without -tags kittytk.
`

func main() {
	launchArgs, wantVersion, wantHelp, wantWindow := mewhost.SplitArgs(os.Args[1:])
	switch {
	case wantVersion:
		fmt.Printf("mew %s (kittytk host)\n", mew.FullVersion())
		return
	case wantHelp:
		fmt.Print(usage)
		return
	case wantWindow:
		// --window opens the graphical build: hand off to the mew-sdl binary next
		// to us, passing the same arguments, so one command opens either a
		// terminal or a window. We wait for it and exit with its status, so from
		// the shell's view mew-sdl simply ran in our place.
		launchGraphical()
		return // unreachable: launchGraphical exits the process
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

// launchGraphical runs the mew-sdl binary sitting next to this executable,
// forwarding our arguments verbatim (mew-sdl ignores the --window that got us
// here), and exits with its status. It never returns. This gives the illusion
// of a single binary that opens either a terminal or a window.
func launchGraphical() {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "mew: cannot locate self: %v\n", err)
		os.Exit(1)
	}
	sdlName := "mew-sdl"
	if runtime.GOOS == "windows" {
		sdlName = "mew-sdl.exe"
	}
	sdlPath := filepath.Join(filepath.Dir(self), sdlName)

	cmd := exec.Command(sdlPath, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "mew: cannot launch %s: %v\n", sdlPath, err)
		os.Exit(1)
	}
	os.Exit(0)
}
