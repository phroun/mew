//go:build sdl

// Command mew-sdl is the graphical (SDL) mew host: a KittyTK SDL-mode host that
// opens a maximized window holding a root-level mew editor and serves the
// KittyTK protocol, so other apps can connect and embed their own (sub-)mew
// editor trinkets. It is the graphical twin of the terminal host (cmd/mew built
// -tags kittytk); both share the same host in internal/mewhost and differ only
// in the backend they set up.
//
// Build with -tags sdl (needs SDL2 dev libraries and cgo), plus -tags mew for
// the real mew-backed editor rather than the placeholder:
//
//	go run -tags "sdl mew" ./app/cmd/mew-sdl file.txt
package main

import (
	"fmt"
	"os"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
	sdlplat "github.com/phroun/kittytk/sdl"
	"github.com/phroun/mew"
	"github.com/phroun/mew-app/internal/mewhost"
)

const usage = `mew — a programmable cross-platform text, prose, and code editor
in the WordStar tradition. (KittyTK SDL host build)

Usage:
  mew-sdl [options] [file ...]

Hosts mew inside a maximized graphical KittyTK window and serves the KittyTK
protocol. The command line is mew's own: switches, +N, and files apply as in
the plain build (one editor, first file focused, the rest as background
buffers).

  -v, --version           print version and exit
  -h, --help              print this help and exit
`

func main() {
	launchArgs, wantVersion, wantHelp, _, _ := mewhost.SplitArgs(os.Args[1:])
	switch {
	case wantVersion:
		fmt.Printf("mew %s (kittytk sdl host)\n", mew.FullVersion())
		return
	case wantHelp:
		fmt.Print(usage)
		return
	}

	// Launch options come from the launching user's ~/.mew/editor.conf
	// ([window]/[service]/[system] sections), not the standalone-KittyTK
	// kittytk.ini; env vars still override. Same knobs as the kittytk-sdl host.
	cfg := mewhost.LoadHostConfig()

	plat := sdlplat.New(cfg.Title, cfg.Width, cfg.Height)
	plat.SetAppName("mew")       // OS app name is "mew", not the binary's "mew-sdl"
	plat.SetScale(cfg.Scale)     // device zoom: pixels per unit at the base font
	plat.SetShowFPS(cfg.ShowFPS) // [window] fps overlays the frame rate
	plat.SetVSync(cfg.VSync)
	plat.SetFontSize(cfg.FontSize) // pixel size of a cell
	core.SetWindowFrameBorderPx(cfg.BorderWidth)
	core.SetMacNativeShortcuts(cfg.UseMacNativeShortcuts())

	backend, err := plat.EnsureBackend()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// SDL2's clipboard covers macOS, Windows and X11/Wayland - the whole
	// cross-platform clipboard integration for the graphical host.
	backend.SetSystemClipboard(plat.Clipboard, plat.SetClipboard)

	// Free the host's built-in accelerators before the desktop is created: the Ψ
	// system menu is built inside NewDesktop and never rebuilt, so its Exit
	// Desktop shortcut must be cleared first.
	mewhost.ClearHostShortcuts()

	desktop := trinkets.NewDesktop()
	desktop.SetBackend(backend) // seeds root metrics from the raster font
	// The UI font stays one cell tall in UNITS (12); font_size makes it render
	// larger by growing the cell's pixel size, not its unit count.
	desktop.SetFont(&core.Font{Name: "ui-text", Size: 12})

	// Graphical host: show_desktop/hide_desktop toggle solo mode (graphical=true).
	mewhost.BuildHost(desktop, cfg, launchArgs, true)
	os.Exit(desktop.RunOn(plat))
}
