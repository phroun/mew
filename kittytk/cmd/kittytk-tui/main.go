// Command kittytk-tui is the TERMINAL display service: a blank KittyTK
// desktop rendered as text in your terminal, serving the protocol socket.
// It is the terminal twin of kittytk-sdl - the two hosts are
// interchangeable. Applications dial the socket and attach to whichever
// host is running, without knowing (or caring) which renderer is on the
// other end; the display protocol is identical either way:
//
//	terminal 1:  go run ./cmd/kittytk-tui             (terminal)
//	         or:  go run -tags sdl ./cmd/kittytk-sdl   (graphical)
//	terminal 2:  go run ./examples/demoapp   (or ./examples/remoteapp)
package main

import (
	"os"

	"github.com/phroun/kittytk/backend/tui"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/hostcfg"
	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
)

func main() {
	// Shared launch config (kittytk.ini): the terminal host uses the
	// [service] keys ([window] settings apply to kittytk-sdl) plus its own
	// [tui] native knob, separate from the graphical host's [system] native.
	cfg := hostcfg.Load()

	// [tui] native controls whether menu shortcuts render with macOS's native
	// modifier glyphs (⌃⌥⇧⌘) instead of the compact ^X/M-x notation.
	core.SetMacNativeShortcuts(cfg.UseTUIMacNativeShortcuts())

	// [tui] clipboard controls the terminal clipboard integration: mirror
	// Copy/Cut via OSC 52 (default), optionally query the terminal on Paste
	// (read-back), or stay internal to the host.
	tuiOpts := tui.DefaultTUIOptions()
	tuiOpts.OSC52Clipboard = cfg.UseTUIOSC52Clipboard()
	tuiOpts.OSC52Paste = cfg.UseTUIOSC52Paste()
	tuiBackend := tui.NewTUIBackend(tuiOpts)

	// [tui] pseudofont_<group> = off disables a by-name cipher pseudo-font (it
	// renders plain); fraktur_mode = native|pseudo|off governs a terminal's
	// VT100 fraktur request (separate concern).
	tui.ConfigurePseudoFonts(cfg.TUIPseudoFontsDisabled, cfg.TUIFrakturMode)

	desktop := trinkets.NewDesktop()
	desktop.SetBackend(tuiBackend) // seeds root metrics from the cell grid

	// The desktop's own (windowless) application owns the base menu bar
	// until a client dials in.
	application := app.New(nil)
	application.SetName("KittyTK (TUI)")
	desktop.AddApplication(application)

	// Start the display service: applications appear as they connect. The
	// service only touches the desktop via Post, so it is agnostic to the
	// backend - the very same Serve call powers kittytk-sdl.
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

	os.Exit(desktop.Run())
}
