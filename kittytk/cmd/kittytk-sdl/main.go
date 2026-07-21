//go:build sdl

// Command kittytk-sdl is the GRAPHICAL display service (D23): a blank
// KittyTK desktop rendered as pixels in an SDL window, serving the
// protocol socket. It is one of two interchangeable hosts - its terminal
// twin is kittytk-tui. Applications dial the socket and attach to
// whichever host is running, without knowing (or caring) which renderer
// is on the other end:
//
//	terminal 1:  go run -tags sdl ./cmd/kittytk-sdl   (graphical)
//	         or:  go run ./cmd/kittytk-tui             (terminal)
//	terminal 2:  go run ./examples/demoapp   (or ./examples/remoteapp)
package main

import (
	"fmt"
	"os"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/hostcfg"
	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
	sdlplat "github.com/phroun/kittytk/sdl"
)

func main() {
	// Launch options come from kittytk.ini (current dir, then the exe's
	// folder, then the user config dir), so a non-technical user can
	// configure the app without the command line. Env vars still override.
	cfg := hostcfg.Load()

	plat := sdlplat.New(cfg.Title, cfg.Width, cfg.Height)
	plat.SetScale(cfg.Scale) // device zoom: pixels per unit at the base font

	// [window] fps=true overlays the render frame rate on the OS title bar;
	// vsync=false uncaps presents (lets fps read raw throughput).
	plat.SetShowFPS(cfg.ShowFPS)
	plat.SetVSync(cfg.VSync)

	// font_size sets the PIXEL size of a cell: it scales pixels-per-unit
	// (12pt = the base 8x16-pixel cell), so a bigger font grows every
	// unit-measured length in pixels while the root denomination stays
	// 8x16 and layout is unchanged in units. scale (above) multiplies on
	// top as the device-density knob. Setting it on the platform keeps it
	// across resizes and on every torn-off/secondary surface.
	plat.SetFontSize(cfg.FontSize)

	// border_width sets the graphical window frame thickness (device
	// pixels); it is drawn at that width AND reserved outside the window's
	// content area, so a thicker border shrinks the interior. 0 keeps the
	// default.
	core.SetWindowFrameBorderPx(cfg.BorderWidth)

	// [system] native controls whether menu shortcuts render with macOS's
	// native modifier glyphs (⌃⌥⇧⌘) instead of the compact ^X/M-x notation.
	core.SetMacNativeShortcuts(cfg.UseMacNativeShortcuts())

	backend, err := plat.EnsureBackend()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Bridge Copy/Cut/Paste to the OS clipboard. SDL2's clipboard covers
	// macOS, Windows and X11/Wayland, so this is the whole cross-platform
	// integration for the graphical host.
	backend.SetSystemClipboard(plat.Clipboard, plat.SetClipboard)

	// [fonts] / [window] fonts_path / ui_* from kittytk.ini: register the
	// configured font files and search directories into the shared text engine
	// (embedded terminals resolve fonts from the same set), then re-point any
	// ui-* font aliases at their families. Editor.conf's own [fonts] still
	// applies on top per embedded mew instance.
	if len(cfg.Fonts) > 0 || len(cfg.FontsPath) > 0 || len(cfg.FontAliases) > 0 {
		eng := backend.Engine()
		for _, dir := range cfg.FontsPath {
			eng.AddFontSearchPath(dir)
		}
		for family, path := range cfg.Fonts {
			_ = eng.RegisterFontFile(family, path)
		}
		for alias, names := range cfg.FontAliases {
			eng.UseFont(alias, names...)
		}
	}

	desktop := trinkets.NewDesktop()
	desktop.SetBackend(backend) // seeds root metrics from the raster font
	// The UI font stays one cell tall in UNITS (12); font_size makes it
	// render larger by growing the cell's pixel size, not its unit count.
	desktop.SetFont(&core.Font{Name: "ui-text", Size: 12})

	// The desktop's own (windowless) application owns the base menu bar
	// until a client dials in. It is context-only: the host has no editing
	// surface of its own, so no automatic Edit menu is added for it.
	application := app.New(nil)
	application.SetName("KittyTK (SDL)")
	application.SetContextOnly(true)
	desktop.AddApplication(application)

	// Start the display service: applications appear as they connect.
	desktop.SetOnStartup(func() {
		dcfg := display.DefaultConfig(desktop, cfg.ResolveEndpoint())
		if dcfg.Token == "" {
			dcfg.Token = cfg.ResolveToken()
		}
		srv, err := display.ServeConfig(desktop, dcfg)
		if sb := desktop.StatusBar(); sb != nil {
			switch {
			case err != nil:
				sb.SetText("display service unavailable: " + err.Error())
			case srv.TLSFingerprint != "":
				sb.SetText("display service on " + srv.Addr() + " (" + srv.TLSFingerprint + ")")
			default:
				sb.SetText("display service on " + srv.Addr() + " - run examples/demoapp to connect")
			}
		}
	})

	os.Exit(desktop.RunOn(plat))
}
