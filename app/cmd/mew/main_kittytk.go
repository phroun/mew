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
      --detach            like --window, but return the shell immediately
                          instead of waiting for the window to close
      --install           (Windows) install mew: copy the binaries, add a Start
                          Menu shortcut, and put mew on your PATH
      --uninstall         (Windows) reverse --install

Build the standalone terminal editor without -tags kittytk.
`

func main() {
	// Windows self-installer (--install / --uninstall). Handled before anything
	// else so the flags never reach the editor as file operands; a no-op that
	// returns (…, false) on other platforms and other flags.
	if code, done := maybeInstall(os.Args[1:]); done {
		os.Exit(code)
	}

	launchArgs, wantVersion, wantHelp, wantWindow, wantDetach := mewhost.SplitArgs(os.Args[1:])
	switch {
	case wantVersion:
		fmt.Printf("mew %s (kittytk host)\n", mew.FullVersion())
		return
	case wantHelp:
		fmt.Print(usage)
		return
	case wantWindow || wantDetach:
		// --window / --detach open the graphical build: hand off to the mew-sdl
		// binary next to us, passing the same arguments, so one command opens
		// either a terminal or a window. Without --detach we wait for it and exit
		// with its status (as if mew-sdl ran in our place); with --detach we start
		// it in its own session and return the shell immediately. (--detach
		// implies a window - a terminal editor can't detach from its own
		// terminal.)
		launchGraphical(wantDetach)
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
// forwarding our arguments verbatim (mew-sdl ignores the --window/--detach that
// got us here). It never returns. This gives the illusion of a single binary
// that opens either a terminal or a window.
//
// Without detach we inherit the terminal, wait for mew-sdl, and exit with its
// status (as if it ran in our place). With detach we start it in its own
// session with no controlling terminal and return the shell immediately, so the
// window outlives the shell.
func launchGraphical(detach bool) {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "mew: cannot locate self: %v\n", err)
		os.Exit(1)
	}
	target := graphicalTarget(filepath.Dir(self))
	if target == "" {
		fmt.Fprintln(os.Stderr, "mew: no graphical build found (expected mew.app or mew-sdl beside me)")
		os.Exit(1)
	}

	cmd := exec.Command(target, os.Args[1:]...)
	if detach {
		// Own session, no inherited terminal: the shell returns at once and the
		// window survives the terminal closing.
		cmd.SysProcAttr = detachSysProcAttr()
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "mew: cannot launch %s: %v\n", target, err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "mew: cannot launch %s: %v\n", target, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// graphicalTarget returns the executable to run for the graphical build. On
// macOS it prefers a mew.app bundle's inner binary - Contents/MacOS/mew - so the
// window inherits the bundle's Dock icon and name (macOS derives them from the
// enclosing .app); it looks beside us first, then ~/Applications and
// /Applications. Otherwise (and on every other platform) it uses the mew-sdl
// binary beside us. Returns "" when neither is found.
func graphicalTarget(dir string) string {
	if runtime.GOOS == "darwin" {
		apps := []string{filepath.Join(dir, "mew.app")}
		if home, err := os.UserHomeDir(); err == nil {
			apps = append(apps, filepath.Join(home, "Applications", "mew.app"))
		}
		apps = append(apps, "/Applications/mew.app")
		for _, app := range apps {
			inner := filepath.Join(app, "Contents", "MacOS", "mew")
			if fi, err := os.Stat(inner); err == nil && !fi.IsDir() {
				return inner
			}
		}
	}
	sdl := filepath.Join(dir, "mew-sdl")
	if runtime.GOOS == "windows" {
		sdl = filepath.Join(dir, "mew-sdl.exe")
	}
	if fi, err := os.Stat(sdl); err == nil && !fi.IsDir() {
		return sdl
	}
	return ""
}
