//go:build kittytk

// Command mew, KittyTK host build (-tags kittytk).
//
// This build fronts the same mew editor with a KittyTK TUI host: it opens a
// maximized window holding a root-level mew editor and serves the KittyTK
// protocol, so other apps can connect and embed their own (sub-)mew editor
// trinkets. The plain build (no tags) keeps mew driving the terminal directly
// and remains the reference for comparison.
//
// Everything on screen is declared as protocol-style text (the same command
// language remote apps speak over the socket): the menu bar, the status bar,
// and the root window with its `editor` trinket. Built with -tags mew as well
// (`go build -tags "kittytk mew"`), that `new editor` resolves to the real
// mew-backed trinket; without it, to the vanilla placeholder.
//
// The unified mew/KittyTK argument parser (per-file options, +N, global
// stanzas — mew's argwild plan) is still a follow-up; for now the first
// positional argument opens as the root editor's file.
package main

import (
	"fmt"
	"os"

	"github.com/phroun/kittytk/backend/tui"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/hostcfg"
	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/mew"
)

const usage = `mew edits words (KittyTK host build)

Usage:
  mew [options] [file]

This build hosts mew inside a maximized KittyTK TUI window and serves the
KittyTK protocol, so other apps can connect and embed their own mew editor
trinkets. The first file argument opens as the root editor's file.

  -v, --version           print version and exit
  -h, --help              print this help and exit

Build the standalone terminal editor without -tags kittytk.
`

func main() {
	var file string
	for _, a := range os.Args[1:] {
		switch a {
		case "--version", "-v":
			fmt.Printf("mew %s (kittytk host)\n", mew.FullVersion())
			return
		case "--help", "-h":
			fmt.Print(usage)
			return
		default:
			// First non-flag argument is the root editor's file. Full argwild
			// parsing (per-file options, +N) is the follow-up.
			if file == "" && (len(a) == 0 || a[0] != '-') {
				file = a
			}
		}
	}

	// Shared launch config (kittytk.ini): the [service] socket endpoint/token,
	// plus the [tui] native/clipboard knobs, exactly as the kittytk-tui host.
	cfg := hostcfg.Load()
	core.SetMacNativeShortcuts(cfg.UseTUIMacNativeShortcuts())

	tuiOpts := tui.DefaultTUIOptions()
	tuiOpts.OSC52Clipboard = cfg.UseTUIOSC52Clipboard()
	tuiOpts.OSC52Paste = cfg.UseTUIOSC52Paste()

	desktop := trinkets.NewDesktop()
	desktop.SetBackend(tui.NewTUIBackend(tuiOpts))

	// The host's application owns the menu/status chrome and the root window.
	application := app.New(nil)
	application.SetName("mew")
	application.SetMenuBarContent(buildMenus(desktop, application))
	application.SetStatusBarContent(buildStatus(
		`sb=new statusbar children={new section children={new span text="mew - a KittyTK host. Other apps can dock their own mew editors."}}`))
	desktop.AddApplication(application)

	// Windows are created once the screen bounds are known.
	desktop.SetOnStartup(func() {
		root := newEditorWindow(desktop, application, file, true)
		application.AddWindow(root)
		application.SetMainWindow(root)
		// Other apps dial in and embed their own mew editors, and File > New
		// Window opens more here, so the host is multi-window: the system
		// supplies the Window menu.
		application.SetMultiWindow(true)
		desktop.WindowManager().MaximizeWindow(root)

		// Serve the display socket: apps appear as they connect.
		dcfg := display.DefaultConfig(desktop, cfg.ResolveEndpoint())
		if dcfg.Token == "" {
			dcfg.Token = cfg.ResolveToken()
		}
		if srv, err := display.ServeConfig(desktop, dcfg); err != nil {
			if sb := desktop.StatusBar(); sb != nil {
				sb.SetText("display service unavailable: " + err.Error())
			}
		} else if sb := desktop.StatusBar(); sb != nil {
			sb.SetText("mew hosting on " + srv.Addr() + " - other apps can connect")
		}
	})

	os.Exit(desktop.Run())
}

// newEditorWindow builds a window holding a mew `editor` trinket from protocol
// text. When main, ending the mew session (the editor's commit event) quits the
// host; a secondary window just closes on session end.
func newEditorWindow(desktop *trinkets.Desktop, application *app.Application, file string, isMain bool) *window.Window {
	title := "mew"
	editorLine := "ed=new editor"
	if file != "" {
		editorLine = "ed=new editor filename=" + protocol.Quote(file)
		title = "mew - " + file
	}
	// `edref=w.ed` surfaces the nested editor as a top-level name so `sub` (and
	// the id lookup below) can reach it - the same aliasing the demo host uses
	// for a trinket built inside a window's children.
	script := fmt.Sprintf(`
w=new window title=%s children={
	%s
}
edref=w.ed
sub edref commit
`, protocol.Quote(title), editorLine)

	dispatcher := protocol.NewEventDispatcher()
	ctx := &protocol.BindContext{Emit: func(ev *protocol.Event) { dispatcher.Dispatch(ev) }}
	byID, reply := execProtocol(script, ctx)

	w := byID[reply.IDs["w"]].(*window.Window)
	// Session end (mew quit, or the placeholder's OK): quit the host from the
	// root editor, otherwise just close this window.
	dispatcher.On(reply.IDs["edref"], "commit", func(*protocol.Event) {
		if isMain {
			desktop.Quit()
		} else {
			w.Close()
		}
	})
	return w
}

// idCaptureFactory records built protocol objects by ID so the host can reach
// the concrete window/editor behind reply names, and forwards event-control so
// `sub` statements reach the wrapped registry factory. (Same pattern the demo
// host uses.)
type idCaptureFactory struct {
	inner protocol.Factory
	byID  map[uint64]any
}

func (f *idCaptureFactory) New(typeName string) (protocol.Object, error) {
	o, err := f.inner.New(typeName)
	if err != nil {
		return nil, err
	}
	type built interface {
		Target() any
		ID() uint64
	}
	if b, ok := o.(built); ok {
		f.byID[b.ID()] = b.Target()
	}
	return o, nil
}

func (f *idCaptureFactory) Subscribe(id uint64, typ string) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Subscribe(id, typ)
	}
}
func (f *idCaptureFactory) Unsubscribe(id uint64, typ string) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Unsubscribe(id, typ)
	}
}
func (f *idCaptureFactory) Suppressed(fn func()) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Suppressed(fn)
		return
	}
	fn()
}

// execProtocol parses and runs a protocol script, returning the id->target
// table and the reply (name->id). It panics on script errors: these scripts are
// host-authored constants, so a failure is a bug, not user input.
func execProtocol(script string, ctx *protocol.BindContext) (map[uint64]any, *protocol.Reply) {
	if ctx == nil {
		ctx = &protocol.BindContext{}
	}
	factory := &idCaptureFactory{inner: protocol.NewRegistryFactory(ctx), byID: make(map[uint64]any)}
	parsed, err := protocol.Parse(script)
	if err != nil {
		panic(fmt.Sprintf("protocol parse: %v", err))
	}
	reply, err := protocol.NewSession().Execute(parsed, factory)
	if err != nil {
		panic(fmt.Sprintf("protocol exec: %v", err))
	}
	return factory.byID, reply
}

// buildStatus executes a statusbar script and returns its sections.
func buildStatus(script string) []trinkets.StatusSection {
	byID, reply := execProtocol(script, nil)
	return byID[reply.IDs["sb"]].(interface {
		Sections() []trinkets.StatusSection
	}).Sections()
}

// buildMenus builds the host menu bar from protocol text and registers the
// action handlers. Raw Key Input passes the next keystroke straight to the
// focused trinket (so control keys reach the mew editor), exactly as the demo.
func buildMenus(desktop *trinkets.Desktop, application *app.Application) []*trinkets.Menu {
	const script = `
bar=new menubar children={
	new menu caption="&mew" wellknown="app" children={
		new menuitem caption="&New Window" shortcut="^N" action=mew.window.new
	}
	new menu caption="&Edit" wellknown="edit" children={
		new menuitem caption="&Raw Key Input" shortcut="^\\" action=mew.edit.rawkey
	}
	new menu caption="&Window" wellknown="window" children={
		new menuitem caption="&New Window" action=mew.window.new
	}
	new menu caption="&Help" wellknown="help" children={
		new menuitem caption="&About" action=mew.help.about
	}
}
`
	byID, reply := execProtocol(script, nil)
	menus := byID[reply.IDs["bar"]].(interface{ Menus() []*trinkets.Menu }).Menus()

	commands := application.Commands()
	// Raw Key Input: pass the next keystroke straight to the focused trinket,
	// so a control key mew binds (and the host would otherwise consume) reaches
	// the editor. Same handler the demo wires.
	commands.Register("mew.edit.rawkey", func() {
		desktop.ActivatePassNextKeyToTrinket()
	})
	// New Window opens another (scratch) mew editor - a sub-mew of the host.
	commands.Register("mew.window.new", func() {
		application.AddWindow(newEditorWindow(desktop, application, "", false))
	})
	commands.Register("mew.help.about", func() {
		byID, reply := execProtocol(fmt.Sprintf(
			`dlg=new messagebox icon=information ok title="About mew" text=%s`,
			protocol.Quote("mew "+mew.FullVersion()+"\n\nA KittyTK host presenting a root mew editor.")), nil)
		application.AddWindow(&byID[reply.IDs["dlg"]].(*trinkets.MessageBox).Window)
	})

	return menus
}
