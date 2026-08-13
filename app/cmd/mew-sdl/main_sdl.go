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
	// Embeds and unpacks wgpu-native at startup on a Windows -tags wgpuembed
	// build (a no-op otherwise), so the self-contained .exe carries its GPU
	// runtime like sdlembed carries SDL3.
	_ "github.com/phroun/mew-app/internal/wgpuembed"
)

const usage = `mew — a programmable cross-platform text, prose, and code editor
in the WordStar tradition. (KittyTK SDL host build)

Usage:
  mew-sdl [options] [file ...]

Hosts mew inside a maximized graphical KittyTK window and serves the KittyTK
protocol. The command line is mew's own: switches, +N, --eval, and files
apply as in the plain build (one editor, first file focused, the rest as
background buffers).

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

	// [window] renderer selects the software or WebGPU backend (mew defaults to
	// webgpu; see LoadHostConfig). A GPU init failure surfaces here.
	plat, err := sdlplat.New(cfg.Title, cfg.Width, cfg.Height, cfg.Renderer)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create platform: %v\n", err)
		os.Exit(1)
	}
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

	// Declare what kind of host this is before anything reads a keymap: the
	// environment hints in a keymap ((mac), (only_gfx), (kde)...) are evaluated
	// when a binding is added to a registry, and the toolkit cannot tell for
	// itself whether this binary draws pixels or characters.
	core.SetKeymapEnvironment(core.KeymapEnvironment{
		Graphical: true,
		Desktop:   cfg.HostType, // [window] host_type, or blank to detect
	})

	// Free the host's built-in accelerators before the desktop is created: the Ψ
	// system menu is built inside NewDesktop and never rebuilt, so its Exit
	// Desktop shortcut must be cleared first.
	mewhost.ClearHostShortcuts()

	desktop := trinkets.NewDesktop()
	desktop.SetBackend(backend) // seeds root metrics from the raster font
	// [window] desktop_frame: themed (the desktop paints its own title bar
	// and handles its own moving/resizing), native_titlebar, or native.
	// The themed title bar shows the same [window] title the OS bar would.
	desktop.SetDesktopFrame(cfg.DesktopFrame)
	desktop.SetTitle(cfg.Title)
	// The UI font stays one cell tall in UNITS (12); font_size makes it render
	// larger by growing the cell's pixel size, not its unit count.
	desktop.SetFont(&core.Font{Name: "ui-text", Size: 12})

	// Wallpaper: [window] wallpaper names the image; a bad path is reported and
	// the built-in pattern stays, so a typo cannot leave the desktop blank.
	if path := cfg.ResolveWallpaper(); path != "" {
		if err := desktop.SetWallpaperFile(path); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: %v\n", err)
		}
	}
	// How that image (or the built-in pattern) covers the desktop:
	// wallpaper_mode/_scale/_align/_tile/_filter. A name that does not parse is
	// reported and the default kept.
	layout, layoutErrs := cfg.ResolveWallpaperLayout()
	for _, err := range layoutErrs {
		fmt.Fprintf(os.Stderr, "WARNING: %v\n", err)
	}
	desktop.SetWallpaperLayout(layout)

	// Graphical host: show_desktop/hide_desktop toggle solo mode (graphical=true).
	about := mewhost.BuildHost(desktop, cfg, launchArgs, true)
	// Make the macOS application menu's "About mew" show mew's own About dialog
	// (Run installs this once SDL has built the menu). No-op off macOS.
	plat.SetAboutHandler(about)
	os.Exit(desktop.RunOn(plat))
}
