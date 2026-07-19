// Package mewhost builds the KittyTK host that fronts a root mew editor,
// independent of which backend renders it. A caller sets up a backend (a
// terminal grid, or an SDL pixel surface), hands the backed desktop to
// BuildHost, and runs it (Run for TUI, RunOn for SDL). Everything on screen is
// declared as protocol-style text - the same command language remote apps speak
// over the socket - so the terminal and graphical hosts are identical but for
// the backend.
//
// The root window holds an `editor` trinket; built with -tags mew that is the
// real mew-backed editor, otherwise the vanilla placeholder. The host injects
// its process argv through the editor's SetLaunchArgv seam, so mew runs its full
// command-line launch (multi-file, per-file options, +N) inside the trinket.
package mewhost

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/hostcfg"
	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/mew"
)

// SplitArgs separates the meta flags from the mew command line: it reports
// whether --version/-v or --help/-h was seen (so the caller can print and
// exit), and returns everything else verbatim as the launch argv for the root
// editor.
func SplitArgs(args []string) (launch []string, wantVersion, wantHelp bool) {
	for _, a := range args {
		switch a {
		case "--version", "-v":
			wantVersion = true
		case "--help", "-h":
			wantHelp = true
		default:
			launch = append(launch, a)
		}
	}
	return launch, wantVersion, wantHelp
}

// BuildHost wires the host application onto an already-backed desktop: the menu
// and status chrome, and a startup callback that opens the maximized root mew
// editor (launching launchArgs) and serves the display socket. The caller runs
// the desktop afterward.
//
// Experimental single-app chrome toggles - developer flags, flip in source and
// rebuild. They take effect only when a single application owns the desktop (the
// mew host case) and only bite on the TUI host: on the graphical host solo mode
// already drops the desktop chrome. Off by default (all chrome shown).
const (
	// hideMenuBarSoleApp hides the desktop menu bar (mew / Edit / Help), leaving
	// mew with no menu chrome at all. Its actions become unreachable by pointer,
	// so this waits on the keybinding overhaul before it can be a default.
	hideMenuBarSoleApp = true
	// hideTitleBarSoleApp hides the root window's title bar while it is maximized
	// in the TUI (no [x][.][o] buttons); closing then relies on mew's own quit.
	hideTitleBarSoleApp = true
)

// BuildHost's application is single-window on its own and upgrades to
// multi-window (New Window plus the system Window menu) whenever another app is
// present - see the applications-changed hook below. graphical selects the
// desktop-reveal mechanism for mew's show_desktop / hide_desktop commands: the
// graphical (SDL) host toggles the built-in solo mode, the TUI host forces /
// releases multi-window (it has no separate desktop surface to reveal).
func BuildHost(desktop *trinkets.Desktop, cfg hostcfg.Config, launchArgs []string, graphical bool) {
	application := app.New(nil)
	application.SetName("mew")
	application.SetMultiWindow(false) // alone to start; the hook below tracks peers
	application.SetMenuBarContent(buildMenus(desktop, application, false))
	application.SetStatusBarContent(buildStatus(
		`sb=new statusbar children={new section children={new span text="mew - a KittyTK host. Other apps can dock their own mew editors."}}`))
	desktop.SetHideMenuBarForSoleApp(hideMenuBarSoleApp)
	desktop.AddApplication(application)

	var root *window.Window
	forceMulti := false // set by show_desktop on the TUI (see below)

	// applyMulti recomputes mew's multi-window status - multi when a peer app is
	// present OR when show_desktop forced it - and re-fits the maximized root
	// window to the resulting client area. It follows the presence of OTHER
	// (non-context) apps: single-window alone, multi-window once a peer joins the
	// server, and back again when the last peer leaves; mew's own
	// maximized/restored state has no bearing on it. Rebuilding the menus keeps
	// mew's New Window item in step, and SetMenuBarContent recomposes the visible
	// bar (including the system Window menu). The re-fit keeps the maximized
	// window sized to the client area, which the Ψ menu and status bar (shown
	// once mew is no longer the sole app) shrink.
	applyMulti := func() {
		multi := forceMulti || otherForegroundApps(desktop, application) > 0
		if multi != application.MultiWindow() {
			application.SetMultiWindow(multi)
			application.SetMenuBarContent(buildMenus(desktop, application, multi))
		}
		refitRoot(desktop, root)
	}
	desktop.SetOnApplicationsChanged(applyMulti)

	// mew's show_desktop / hide_desktop commands, wired onto the root editor at
	// startup. On the graphical host they toggle the built-in solo mode (reveal /
	// hide the desktop); on the TUI they force / release multi-window. Both post
	// to the platform thread - the commands fire on mew's session goroutine.
	showDesktop := func() {
		desktop.Post(func() {
			if graphical {
				desktop.ExitSoloMode()
			} else {
				forceMulti = true
				applyMulti()
			}
		})
	}
	hideDesktop := func() {
		desktop.Post(func() {
			if graphical {
				desktop.EnterSoloFromDesktop()
			} else {
				forceMulti = false
				applyMulti()
			}
		})
	}

	// Windows are created once the screen bounds are known.
	desktop.SetOnStartup(func() {
		root = startRootWindow(desktop, application, launchArgs)
		if ed, ok := root.Content().(*trinkets.Editor); ok {
			ed.SetShowDesktop(showDesktop)
			ed.SetHideDesktop(hideDesktop)
		}
		serveSocket(desktop, cfg)
	})
}

// refitRoot re-maximizes the docked root window to the current client area, so
// it tracks the desktop chrome (Ψ menu, status bar) appearing or disappearing
// as the app set changes. No-op before the window exists, when it is not
// maximized (a deliberately restored/floating window is left alone), or on the
// graphical host (the window is lifted onto its own surface, not manager-
// managed).
func refitRoot(desktop *trinkets.Desktop, root *window.Window) {
	if root == nil || !root.IsMaximized() {
		return
	}
	wm := desktop.WindowManager()
	if wm == nil || !windowManaged(wm, root) {
		return
	}
	wm.MaximizeWindow(root) // recomputes bounds to the current client area
	desktop.Update()
}

// otherForegroundApps counts the non-context-only applications on the desktop
// other than self - i.e. how many peer apps are present.
func otherForegroundApps(desktop *trinkets.Desktop, self *app.Application) int {
	n := 0
	for _, a := range desktop.Applications() {
		if a != trinkets.ApplicationProvider(self) && !a.ContextOnly() {
			n++
		}
	}
	return n
}

// startRootWindow opens the root mew editor and makes it the whole display via
// solo mode. Runs on the platform thread at startup (the surface and screen
// bounds are ready), returning the root window.
func startRootWindow(desktop *trinkets.Desktop, application *app.Application, launchArgs []string) *window.Window {
	root := newEditorWindow(desktop, application, launchArgs, true)
	application.AddWindow(root)
	application.SetMainWindow(root)
	// (Multi-window status is managed by BuildHost's applications-changed hook,
	// following the presence of other apps - not set here.)
	// Enter solo mode: the root mew window becomes the WHOLE display - no desktop
	// wallpaper, no system (Psi) menu, no window border behind it, just mew's own
	// menu/status chrome filling the surface. mew is the server here, so it
	// drives the desktop directly (the "everything goes through the protocol"
	// rule is for the apps that connect to it, not the host itself). Apps that
	// connect become peers; any client can still reveal a desktop via the
	// spawndesktop verb.
	desktop.EnterSoloMode(root)

	// On a graphical surface, solo mode re-hosts the root window onto its own OS
	// surface to fill the display, and the tear-off host focuses it. On a
	// single-surface backend (the TUI) the window instead stays docked in the
	// desktop's window manager - so maximize it WITHIN the TUI desktop (its
	// client area) and activate it so keystrokes reach the editor. (After a
	// graphical solo the window is no longer manager-managed, so this is
	// naturally skipped there.)
	if wm := desktop.WindowManager(); wm != nil && windowManaged(wm, root) {
		// Experimental: drop all chrome WHILE the root window is maximized so the
		// editor fills the whole surface (the TUI single-app circumstance).
		// NoTitleWhenMaximized is state-aware: no title and no frame while
		// maximized, but the window regains its normal title bar and border when
		// it is restored (e.g. once another app joins the server and the desktop
		// is revealed), unlike a static Frameless/NoTitle.
		if hideTitleBarSoleApp {
			root.SetFlags(root.Flags() | window.WindowFlagNoTitleWhenMaximized)
		}
		wm.MaximizeWindow(root)
		wm.ActivateWindow(root)
	}
	return root
}

// windowManaged reports whether win is still tracked by the window manager (i.e.
// docked, not lifted onto its own surface).
func windowManaged(wm *window.WindowManager, win *window.Window) bool {
	for _, w := range wm.Windows() {
		if w == win {
			return true
		}
	}
	return false
}

// serveSocket starts the display service so apps appear as they connect,
// reporting the outcome in the status bar.
func serveSocket(desktop *trinkets.Desktop, cfg hostcfg.Config) {
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
}

// newEditorWindow builds a window holding a mew `editor` trinket from protocol
// text, then injects the launch argv (if any) through the editor's host seam so
// mew runs its full command-line launch inside the trinket. When main, ending
// the mew session (the editor's commit event) quits the host; a secondary
// window just closes on session end.
func newEditorWindow(desktop *trinkets.Desktop, application *app.Application, argv []string, isMain bool) *window.Window {
	title := "mew"
	if f := firstOperand(argv); f != "" {
		title = "mew - " + f
	}
	// `edref=w.ed` surfaces the nested editor as a top-level name so `sub` (and
	// the id lookup below) can reach it. Files/options are not protocol
	// properties here: mew parses the whole argv itself (below).
	script := fmt.Sprintf(`
w=new window title=%s children={
	ed=new editor
}
edref=w.ed
sub edref commit
`, protocol.Quote(title))

	dispatcher := protocol.NewEventDispatcher()
	ctx := &protocol.BindContext{Emit: func(ev *protocol.Event) { dispatcher.Dispatch(ev) }}
	byID, reply := execProtocol(script, ctx)

	w := byID[reply.IDs["w"]].(*window.Window)
	// Hand mew the command line through the editor's host seam: the trinket runs
	// mew.EditArgv, so per-file options, +N, and multi-file open all apply as in
	// the plain build (one editor, first file focused, the rest as background
	// buffers). Secondary windows get no argv - a scratch editor.
	if len(argv) > 0 {
		if ed, ok := byID[reply.IDs["edref"]].(*trinkets.Editor); ok {
			ed.SetLaunchArgv(argv)
		}
	}
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

// firstOperand returns the first non-switch argument (a file) for the window
// title only; mew itself does the authoritative parse. Best-effort: a valued
// switch's separate value can be mistaken for a file here, but the editor's own
// modeline shows the real filename.
func firstOperand(argv []string) string {
	for _, a := range argv {
		if a == "" || a[0] == '-' || a[0] == '+' {
			continue
		}
		return a
	}
	return ""
}

// ClearHostShortcuts removes the KittyTK host's built-in menu accelerators from
// the global keybinding registry so their keys fall through to the mew editor
// instead of being swallowed by the host: ^Q (Quit), ^H/M-^H (Hide/Hide Others),
// M-^X (Exit Desktop), and ^X/^C/^V/M-a (Cut/Copy/Paste/Select All). mew is a
// full text editor and binds most of these itself, so the host must not
// intercept them.
//
// Call this BEFORE trinkets.NewDesktop(): the Ψ system menu (which carries Exit
// Desktop) is built once inside NewDesktop by reading this registry, and is not
// rebuilt afterward - so clearing later leaves M-^X on it. The app/edit/window
// menus are rebuilt on every menu-bar composition, so those pick up the cleared
// registry regardless; the system menu is the one that must be cleared up front.
//
// The actions stay reachable from the menus (clicking still works; the
// synthesized items just render without an accelerator - all synthesis sites
// guard on len(keys) > 0, so an empty binding is safe). New Window and Raw Key
// Input are app-declared in buildMenus below and simply carry no shortcut.
//
// This is a deliberate stopgap: it removes the conflicts now. Real rebinding and
// the accessibility story (keyboard reachability of these actions) come with the
// planned keybinding overhaul.
func ClearHostShortcuts() {
	for _, action := range []string{
		core.ActionQuit,
		core.ActionAppHide,
		core.ActionAppHideOthers,
		core.ActionAppShowAll,
		core.ActionExitDesktop,
		core.ActionCut,
		core.ActionCopy,
		core.ActionPaste,
		core.ActionSelectAll,
	} {
		core.DefaultKeyBindings.ClearAction(action)
	}
}

// buildMenus builds the host menu bar from protocol text and registers the
// action handlers. Raw Key Input passes the next keystroke straight to the
// focused trinket (so control keys reach the mew editor), exactly as the demo.
//
// When multiWindow is false (the TUI host), the New Window items and the Window
// menu are omitted entirely - the app menu is left for the host to synthesize
// (Hide/Quit), so the bar is just mew / Edit / Help. New windows only make sense
// where they can be peers (the graphical host).
func buildMenus(desktop *trinkets.Desktop, application *app.Application, multiWindow bool) []*trinkets.Menu {
	// No shortcut= on these: like the host accelerators cleared in
	// ClearHostShortcuts, New Window and Raw Key Input are menu-only for now so
	// their keys stay free for the mew editor. Rebinding comes later.
	//
	// The app menu and Window menu carry New Window only in the multi-window
	// (graphical) host; on the TUI both are dropped.
	appMenu, windowMenu := "", ""
	if multiWindow {
		appMenu = `
	new menu caption="&mew" wellknown="app" children={
		new menuitem caption="&New Window" action=mew.window.new
	}`
		windowMenu = `
	new menu caption="&Window" wellknown="window" children={
		new menuitem caption="&New Window" action=mew.window.new
	}`
	}
	script := fmt.Sprintf(`
bar=new menubar children={%s
	new menu caption="&Edit" wellknown="edit" children={
		new menuitem caption="&Raw Key Input" action=mew.edit.rawkey
	}%s
	new menu caption="&Help" wellknown="help" children={
		new menuitem caption="&About" action=mew.help.about
	}
}
`, appMenu, windowMenu)
	byID, reply := execProtocol(script, nil)
	menus := byID[reply.IDs["bar"]].(interface{ Menus() []*trinkets.Menu }).Menus()

	commands := application.Commands()
	// Raw Key Input: pass the next keystroke straight to the focused trinket, so
	// a control key mew binds (and the host would otherwise consume) reaches the
	// editor. Same handler the demo wires.
	commands.Register("mew.edit.rawkey", func() {
		desktop.ActivatePassNextKeyToTrinket()
	})
	if multiWindow {
		// New Window opens another (scratch) mew editor - a sub-mew of the host.
		commands.Register("mew.window.new", func() {
			application.AddWindow(newEditorWindow(desktop, application, nil, false))
		})
	}
	commands.Register("mew.help.about", func() { showMewAbout(application) })

	return menus
}

// showMewAbout opens mew's About dialog as a modal message box on the app. It
// backs both mew's own Help > About and the system (Ψ) menu's About (wired via
// desktop.SetAboutHandler in BuildHost), so the Ψ About shows mew's dialog
// instead of the built-in About KittyTK one.
func showMewAbout(application *app.Application) {
	byID, reply := execProtocol(fmt.Sprintf(
		`dlg=new messagebox icon=information ok title="About mew" text=%s`,
		protocol.Quote("mew "+mew.FullVersion()+"\n\nmew edits words.")), nil)
	application.AddWindow(&byID[reply.IDs["dlg"]].(*trinkets.MessageBox).Window)
}

// buildStatus executes a statusbar script and returns its sections.
func buildStatus(script string) []trinkets.StatusSection {
	byID, reply := execProtocol(script, nil)
	return byID[reply.IDs["sb"]].(interface {
		Sections() []trinkets.StatusSection
	}).Sections()
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
