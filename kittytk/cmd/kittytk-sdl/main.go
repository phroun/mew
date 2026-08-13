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

	"github.com/phroun/argwild"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/hostcfg"
	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
	sdlplat "github.com/phroun/kittytk/sdl"
)

// wallpaperFromArgs applies --wallpaper=PATH on top of the configured
// image. Last one on the line wins; "--wallpaper-" turns it off and goes
// back to the built-in pattern.
func wallpaperFromArgs(r *argwild.Result, configured string) string {
	wallpaper := configured
	if r == nil {
		return wallpaper
	}
	for _, set := range r.ArgSets() {
		for _, sw := range set.Switches {
			if sw.Name != "wallpaper" {
				continue
			}
			if sw.IsOff() {
				wallpaper = ""
				continue
			}
			if v, ok := sw.First(); ok {
				wallpaper = v.AsString()
			}
		}
	}
	return wallpaper
}

// rendererFromArgs applies command-line renderer switches on top of the
// configured engine: --webgpu and --software (alias --sdl) pick one
// directly, --renderer=NAME passes a name through (validated by the
// platform). The last switch on the line wins; an explicitly disabled
// switch ("--webgpu-") is ignored.
func rendererFromArgs(r *argwild.Result, configured string) string {
	renderer := configured
	if r == nil {
		return renderer
	}
	for _, set := range r.ArgSets() {
		for _, sw := range set.Switches {
			if sw.IsOff() {
				continue
			}
			switch sw.Name {
			case "webgpu":
				renderer = "webgpu"
			case "software", "sdl":
				renderer = "software"
			case "renderer":
				if v, ok := sw.First(); ok {
					renderer = v.AsString()
				}
			}
		}
	}
	return renderer
}

func main() {
	// Launch options come from kittytk.ini (current dir, then the exe's
	// folder, then the user config dir), so a non-technical user can
	// configure the app without the command line. Env vars still override.
	cfg := hostcfg.Load()

	// The [mappings] section and [window] accelerator_chord overlay the
	// toolkit's own keymap: the file says what it changes rather than
	// restating the whole table.
	core.ApplyHostKeymap(cfg.Mappings, cfg.AcceleratorChord)

	// Command-line switches beat the file for this launch:
	// --webgpu | --software | --renderer=NAME.
	if parsed, err := argwild.Parse(); err == nil {
		cfg.Renderer = rendererFromArgs(parsed, cfg.Renderer)
		cfg.Wallpaper = wallpaperFromArgs(parsed, cfg.Wallpaper)
	}

	// Create platform with configured renderer (software or webgpu)
	plat, err := sdlplat.New(cfg.Title, cfg.Width, cfg.Height, cfg.Renderer)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create platform: %v\n", err)
		os.Exit(1)
	}

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

	// Bridge Copy/Cut/Paste to the OS clipboard. SDL3's clipboard covers
	// macOS, Windows and X11/Wayland, so this is the whole cross-platform
	// integration for the graphical host.
	backend.SetSystemClipboard(plat.Clipboard, plat.SetClipboard)

	// [fonts] / [window] fonts_path / ui_* from kittytk.ini: register the
	// configured font files and search directories into the shared text engine
	// (embedded terminals resolve fonts from the same set), then re-point any
	// ui-* font aliases at their families. Editor.conf's own [fonts] still
	// applies on top per embedded editor instance.
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
	// [window] desktop_frame: themed (the desktop paints its own title bar
	// and handles its own moving/resizing), native_titlebar, or native.
	desktop.SetDesktopFrame(cfg.DesktopFrame)
	// The UI font stays one cell tall in UNITS (12); font_size makes it
	// render larger by growing the cell's pixel size, not its unit count.
	desktop.SetFont(&core.Font{Name: "ui-text", Size: 12})

	// Wallpaper: KITTYTK_WALLPAPER, then --wallpaper=PATH, then the ini's
	// [window] wallpaper. Any size — it is repeated from the surface
	// origin, by the GPU's sampler under the compositor. A bad path is
	// reported and the built-in pattern stays, so a typo cannot leave the
	// desktop blank.
	if path := cfg.ResolveWallpaper(); path != "" {
		if err := desktop.SetWallpaperFile(path); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: %v\n", err)
		}
	}

	// How that image (or the built-in pattern) covers the desktop:
	// wallpaper_mode, _scale, _align, _tile and _filter. A name that
	// does not parse is reported and the default kept — silently
	// papering the desktop the old way would leave no way to tell a
	// typo from a setting that does not do what it sounds like.
	layout, layoutErrs := cfg.ResolveWallpaperLayout()
	for _, err := range layoutErrs {
		fmt.Fprintf(os.Stderr, "WARNING: %v\n", err)
	}
	desktop.SetWallpaperLayout(layout)

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

	exitCode := desktop.RunOn(plat)
	os.Exit(exitCode)
}
