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
	"strings"

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
// whether --version/-v, --help/-h, --window, or --detach was seen (so the caller
// can act and exit), and returns everything else verbatim as the launch argv for
// the root editor. --window / --detach are mode selectors - the TUI host hands
// off to mew-sdl, the graphical host just ignores them - so they are stripped
// from the launch argv either way.
func SplitArgs(args []string) (launch []string, wantVersion, wantHelp, wantWindow, wantDetach bool) {
	for _, a := range args {
		switch a {
		case "--version", "-v":
			wantVersion = true
		case "--help", "-h":
			wantHelp = true
		case "--window", "-window":
			wantWindow = true
		case "--detach", "-detach":
			wantDetach = true
		default:
			launch = append(launch, a)
		}
	}
	return launch, wantVersion, wantHelp, wantWindow, wantDetach
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
// BuildHost returns an "about" invoker: calling it shows mew's About dialog on
// the platform thread. The graphical main wires this to the native macOS
// application-menu About item (plat.SetAboutHandler) so "mew ▸ About mew" shows
// the same dialog as Help ▸ About; the TUI main ignores it.
func BuildHost(desktop *trinkets.Desktop, cfg hostcfg.Config, launchArgs []string, graphical bool) func() {
	application := app.New(nil)
	application.SetName("mew")
	application.SetMultiWindow(false) // alone to start; the hook below tracks peers
	application.SetMenuBarContent(buildMenus(desktop, application, false))
	application.SetStatusBarContent(buildStatus(
		`sb=new statusbar children={new section children={new span text="mew - a KittyTK host. Other apps can dock their own mew editors."}}`))
	// The sole-app chrome suppression (hide Ψ / menu bar / status bar for a lone
	// fullscreen app) is a TUI concern only; on the graphical host solo mode
	// already handles fullscreen, so leave the desktop chrome alone there.
	desktop.SetSoleAppChromeSuppression(!graphical)
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
				// Bring the revealed desktop's own OS window to the front:
				// ExitSoloMode raises it but then re-homes mew above it, so raise
				// the desktop once more so IT ends up on top.
				desktop.RaiseToFront()
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

	// The Viewport menu's desktop toggle. Reading "is it showing" differs per
	// host - solo mode on the graphical one, forced multi-window on the TUI -
	// so it is resolved here, where both facts are in scope.
	desktopReveal.visible = func() bool {
		if graphical {
			return !desktop.IsSolo()
		}
		return forceMulti
	}
	desktopReveal.toggle = func() {
		if desktopReveal.visible() {
			hideDesktop()
		} else {
			showDesktop()
		}
	}

	// Windows are created once the screen bounds are known.
	desktop.SetOnStartup(func() {
		root = startRootWindow(desktop, application, launchArgs)
		if ed, ok := root.Content().(*trinkets.Editor); ok {
			ed.SetShowDesktop(showDesktop)
			ed.SetHideDesktop(hideDesktop)
		}
		serveSocket(desktop, cfg)
		// On the graphical host, bring our window to the front and focus it. A
		// detached launch (mew --detach) otherwise opens in the background: the
		// window manager doesn't focus a session-detached process's window. No-op
		// on the TUI (single surface, nothing to reorder).
		if graphical {
			desktop.RaiseToFront()
		}
		// First-run welcome (graphical + Windows + not yet installed): a modal
		// owned by the root editor offering Install or Try. No-op otherwise.
		maybeShowWelcome(desktop, application, root, launchArgs, graphical)
	})

	// The about invoker posts to the platform thread (the native menu action
	// fires from AppKit's menu-tracking loop, off the desktop's own pump).
	return func() { desktop.Post(func() { showMewAbout(application) }) }
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
// desktopReveal is the Viewport menu's "KittyTK Desktop" seam, filled in by
// BuildHost. Only BuildHost knows which host this is and owns the state the
// reveal turns on: the graphical host hides the desktop with solo mode, the TUI
// with forced multi-window, and there is no single fact buildMenus could read
// for both. Both halves stay nil until BuildHost runs (and in tests, which
// build menus against a bare desktop), so both are nil-checked at use.
var desktopReveal struct {
	visible func() bool // is the desktop showing right now
	toggle  func()      // show it if hidden, hide it if showing
}

func buildMenus(desktop *trinkets.Desktop, application *app.Application, multiWindow bool) []*trinkets.Menu {
	// No shortcut= on these: like the host accelerators cleared in
	// ClearHostShortcuts, New Window and Raw Key Input are menu-only for now so
	// their keys stay free for the mew editor. Rebinding comes later.
	//
	// The app menu and Window menu carry New Window only in the multi-window
	// (graphical) host; on the TUI both are dropped.
	// The app menu is declared in BOTH modes, and carries New Window only in
	// the multi-window one. Leaving it to the host to synthesize gave it the
	// title "&mew" - createStandardAppMenu prepends an ampersand to make the
	// first letter an accelerator - so the product name picked up an underlined
	// M that mew never asked for. Declaring it, even with nothing in it, means
	// our title is used verbatim; the desktop still appends its Hide and Quit
	// sections either way.
	appMenu := `
	new menu caption="mew" wellknown="app"`
	windowMenu := ""
	if multiWindow {
		appMenu += ` children={
		new menuitem caption="&New Window" action=mew.window.new
	}`
		windowMenu = `
	new menu caption="Window" wellknown="window" children={
		new menuitem caption="&New Window" action=mew.window.new
	}`
	}
	// One placeholder item per menu for now: this is a shape check on the
	// menu bar's well-known merge (canonical order app, file, edit, select,
	// format, view, custom..., window, help) rather than a finished menu set.
	// The captions are mew's own — a well-known tag declares a menu's ROLE, so
	// "File Buffer" still merges as file and "Viewport" as view. Input, Search
	// and History carry no tag and land in the custom bucket, in declared
	// order, between view and window.
	script := fmt.Sprintf(`
bar=new menubar children={%s
	new menu caption="File Buffer" wellknown="file" children={
		new menuitem caption="New &Empty Buffer" action=mew.buffer.new
		new menuitem caption="&Open File Buffer..." action=mew.buffer.open
		new menuitem caption="Inse&rt File at Cursor..." action=mew.buffer.read
		new menuitem caption="Dupli&cate Buffer" action=mew.buffer.duplicate
		new menuitem caption="Close Prompt/Viewport" action=mew.buffer.close
		new menuitem separator
		new menuitem caption="&Save Current Buffer" action=mew.buffer.save
		new menuitem caption="Save &All Buffers" action=mew.buffer.saveall
		new menuitem caption="Save As (Write && &Become)..." action=mew.buffer.saveas
		new menuitem caption="Export (&Write)..." action=mew.buffer.write
		new menuitem separator
		new menuitem caption="Document &Direction [?]" action=mew.options.direction
		new menuitem caption="Read Only" action=mew.options.readonly checkable=true
		new menuitem caption="Automatic S&yntax" action=mew.options.syntaxdetect checkable=true
		new menuitem caption="Document Synta&x [?]..." action=mew.options.syntax
	}
	new menu caption="Edit Block" wellknown="edit" children={
		new menuitem caption="Mark &Block Beginning" action=mew.block.setbegin
		new menuitem caption="Mark Bloc&k Ending" action=mew.block.setend
		new menuitem separator
		new menuitem caption="&Move Block to Cursor" action=mew.block.move
		new menuitem caption="&Copy Block to Cursor" action=mew.block.copy
		new menuitem caption="E&xchange with OS Clipboard" action=mew.block.osexchange
		new menuitem caption="Cut to OS Clipboard" wellknown="cut"
		new menuitem caption="Copy to OS Clipboard" wellknown="copy"
		new menuitem caption="Paste from OS Clipboard" wellknown="paste"
		new menuitem separator
		new menuitem caption="&Write Block to File..." action=mew.block.write
		new menuitem caption="&Replace Block with File..." action=mew.block.read
		new menuitem separator
		new menuitem caption="Delete to Kill Ring" action=mew.block.delete
		new menuitem caption="Reclaim from Kill Ring (yank)" action=mew.nerd.yank
		new menuitem caption="Cycle Kill Ring (yankpop)" action=mew.nerd.yankpop
		new menuitem separator
		new menuitem caption="Select All" wellknown="selectall"
	}
	new menu caption="Format" wellknown="format" children={
		new menuitem caption="Unindent Block" action=mew.format.unindent
		new menuitem caption="Indent Block" action=mew.format.indent
		new menuitem caption="&Justify Block" action=mew.format.justify
		new menuitem caption="Filter Block through Cmd" action=mew.format.filter
		new menuitem caption="Convert to UPPERCASE" action=mew.format.upper
		new menuitem caption="Convert to lowercase" action=mew.format.lower
		new menuitem caption="Convert to Title Case" action=mew.format.title
		new menuitem caption="Swap camelCase snake_case" action=mew.format.swapcase
	}
	new menu caption="Viewport" wellknown="view" children={
		new menuitem caption="&Clone Viewport" action=mew.view.clone
		new menuitem caption="&Prior Viewport" action=mew.view.prior
		new menuitem caption="&Next Viewport" action=mew.view.next
		new menuitem separator
		new menuitem caption="Follow Link at Cursor" action=mew.view.follow
		new menuitem caption="Link Navigation Mode" action=mew.view.navmode checkable=true
		new menuitem caption="Pr&ior Link" action=mew.view.linkprior
		new menuitem caption="N&ext Link" action=mew.view.linknext
		new menuitem separator
		new menuitem caption="Document &Direction [?]" action=mew.view.direction
		new menuitem caption="Show Bidi Control Points" action=mew.view.showbidi checkable=true
		new menuitem caption="KittyTK Desktop" action=mew.view.desktop checkable=true
	}
	new menu caption="Input" after="file" children={
		new menuitem caption="&Insert/Overwrite Mode" action=mew.input.insertmode checkable=true
		new menuitem caption="&Mac-style Option Chars" action=mew.input.macoption checkable=true
		new menuitem separator
		new menuitem caption="Document &Direction [?]" action=mew.options.direction
		new menuitem caption="Show Bidi Control Points" action=mew.input.showbidi checkable=true
		new menuitem separator
		new menuitem caption="Insert Unicode Left-to-Right Mark" action=mew.input.lrm
		new menuitem caption="Insert Unicode Right-to-Left Mark" action=mew.input.rlm
		new menuitem caption="Insert Unicode Arabic Letter Mark" action=mew.input.alm
		new menuitem caption="Begin Unicode Isolated Phrase (Auto)" action=mew.input.fsi
		new menuitem caption="Begin Unicode Isolated Phrase (LTR)" action=mew.input.lri
		new menuitem caption="Begin Unicode Isolated Phrase (RTL)" action=mew.input.rli
		new menuitem caption="End Unicode Isolated Phrase" action=mew.input.pdi
		new menuitem separator
		new menuitem caption="Insert Raw Byte..." action=mew.input.rawbyte
		new menuitem caption="Insert Rune by Unicode Code Point..." action=mew.input.rune
		new menuitem separator
		new menuitem caption="Raw Key Input" action=mew.edit.rawkey
	}
	new menu caption="Search" after="file" children={
		new menuitem caption="&Find..." action=mew.search.find
		new menuitem caption="Find Next" action=mew.search.findnext
		new menuitem caption="Incremental &Search" action=mew.search.incremental
		new menuitem separator
		new menuitem caption="Case &Insensitive" action=mew.search.ignorecase checkable=true
		new menuitem caption="Search with &RegEx" action=mew.search.regex checkable=true
		new menuitem caption="Search Backwards" action=mew.search.backwards checkable=true
		new menuitem caption="&Wrap Search" action=mew.search.wrap checkable=true
		new menuitem caption="Search All Buffers" action=mew.search.allbuffers checkable=true
	}
	new menu caption="History" after="format" children={
		new menuitem caption="&Undo" action=mew.history.undo
		new menuitem caption="&Redo / Enter Branch" action=mew.history.redo
		new menuitem caption="Rever&t to Last Save Point" action=mew.history.revert
		new menuitem separator
		new menuitem caption="Navigation History:" enabled=false
		new menuitem caption="&Prior History Item" action=mew.history.navprior
		new menuitem caption="&Next History Item" action=mew.history.navnext
		new menuitem caption="&Clear Link Visit Status" action=mew.history.navclear
	}%s
	new menu caption="Help" wellknown="help" children={
		new menuitem caption="&Using mew" action=mew.help.usingmew
		new menuitem caption="&Quick Help" action=mew.help.quickhelp checkable=true
		new menuitem separator
		new menuitem caption="&Terminal Diagnostics" action=mew.help.ptydiag
		new menuitem separator
		new menuitem caption="&About" action=mew.help.about
	}
}
`, appMenu, windowMenu)
	byID, reply := execProtocol(script, nil)
	menus := byID[reply.IDs["bar"]].(interface{ Menus() []*trinkets.Menu }).Menus()

	commands := application.Commands()
	// The Help ▸ "Using mew" / "Quick Help" items drive the ROOT mew editor
	// (the app's main window), never a peer scratch window: they are the
	// product's own top-level help affordances.
	commands.Register("mew.help.usingmew", func() {
		if ed, ok := rootMewEditor(application); ok {
			ed.Execute(`help_toggle "help:/"`) // the help index, in the docked help window
		}
	})
	commands.Register("mew.help.quickhelp", func() {
		if ed, ok := rootMewEditor(application); ok {
			ed.Execute("help_toggle")
		}
	})
	// Keep the Quick Help checkmark in sync with the root editor's built-in
	// help window each time the Help menu opens (help_toggle, or closing the
	// window, can change it out from under the menu).
	syncQuickHelpCheckmark(menus, application)
	syncPlaceholderShortcuts(menus, application)
	// Raw Key Input: pass the next keystroke straight to the focused trinket, so
	// a control key mew binds (and the host would otherwise consume) reaches the
	// editor. Same handler the demo wires.
	// Every other item fires its mew command on the root editor, so the bar is
	// exercised end to end rather than being inert scenery. One table drives
	// both this and the shortcut column (menuActions), so an item can never be
	// advertised with a key it does not run, or run a command it does not
	// advertise.
	for action, spec := range menuActions {
		if action == rawKeyAction {
			continue // registered by hand below: it does more than run a command
		}
		cmd := spec.cmd // one binding per closure
		commands.Register(action, func() {
			if ed, ok := rootMewEditor(application); ok {
				ed.Execute(cmd)
			}
		})
	}
	commands.Register(rawKeyAction, func() {
		desktop.ActivatePassNextKeyToTrinket()
		// ...and one level deeper. Stopping the HOST from eating the key only
		// gets it as far as mew, which then eats it as a mew binding — and a
		// shell running inside a mew viewport still never sees it. Arming
		// mew's own one-shot too means the keystroke passes the whole way
		// down when a terminal has the focus, and behaves exactly as before
		// when one does not.
		if ed, ok := rootMewEditor(application); ok {
			ed.Execute(menuActions[rawKeyAction].cmd)
		}
	})
	// Viewport ▸ KittyTK Desktop toggles the HOST's desktop, not anything in
	// mew, so it goes through the seam BuildHost filled in rather than the
	// editor's command port. Nil before BuildHost has run (and in tests).
	commands.Register("mew.view.desktop", func() {
		if desktopReveal.toggle != nil {
			desktopReveal.toggle()
		}
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

// rootMewEditor returns the mew editor trinket behind the app's main (root)
// window, or ok=false before it exists. The Help ▸ Using mew / Quick Help
// items act on this editor specifically — the product's root session, not a
// peer scratch window.
func rootMewEditor(application *app.Application) (*trinkets.Editor, bool) {
	w := application.MainWindow()
	if w == nil {
		return nil, false
	}
	ed, ok := w.Content().(*trinkets.Editor)
	return ed, ok
}

// menuAction is one bar item's binding: the mew command it runs, and the key
// spelling to prefer when several sequences are bound to that command. A blank
// key means "no canonical default" — the shortcut column then shows whatever
// the live keymap has, or nothing at all.
type menuAction struct {
	cmd string
	key string
}

// menuActions is the single source of truth for what each item DOES and what
// key it ADVERTISES. Keeping both in one table is deliberate: an item can never
// drift into advertising a key that runs something else.
//
// The keys are mew's own defaults (internal/editor/resources/defaults/*.conf),
// not invention — but they are only a PREFERENCE. Everything is resolved
// against the live keymap at menu-open time, so a rebind shows through and an
// unbound command advertises nothing.
//
// Cut / Copy / Paste / Select All are deliberately absent: the desktop
// synthesizes those itself against the FOCUSED trinket, with its own host
// shortcuts and enable/disable logic (see appendStandardEditItems). Routing
// them through the root editor here would quietly lose all three.
// rawKeyAction is Raw Key Input's id, named because two places need to agree
// about it: the table it takes its key from, and the hand-written handler that
// replaces the one the table would have generated.
const rawKeyAction = "mew.edit.rawkey"

var menuActions = map[string]menuAction{
	// File Buffer
	"mew.buffer.new":       {"buffer_new", "^B E"},
	"mew.buffer.open":      {"buffer_open_file", "^B O"},
	"mew.buffer.read":      {"buffer_insert_file", "^B R"},
	"mew.buffer.duplicate": {"buffer_duplicate", "^B C"},
	"mew.buffer.close":     {"cancel|buffer_close", "^C"},
	"mew.buffer.save":      {"buffer_save", "^B S"},
	"mew.buffer.saveall":   {"buffer_save_all true", "^B A"},
	// Two different operations, two items. buffer_save_as re-homes the buffer:
	// it writes to the new name and the buffer BECOMES that file, carrying its
	// identity, modified flag and save history over. buffer_write exports a
	// copy (SaveCopyTo) and the buffer keeps the file it had, so it prompts
	// before overwriting anything at all — its own source included, since an
	// export is never the buffer's own save.
	"mew.buffer.saveas": {"buffer_save_as", "^B B"},
	"mew.buffer.write":  {"buffer_write", "^B W"},

	// File Buffer ▸ per-document options
	"mew.options.direction":    {`set_option_next "direction"`, "^O D"},
	"mew.options.readonly":     {`set_option_next "readOnly"`, ""}, // no default key
	"mew.options.syntaxdetect": {`set_option_next "syntaxDetect"`, "^O Y"},
	"mew.options.syntax":       {`set_option "syntax"`, "^O X"}, // no value: prompts

	// Edit Block
	"mew.block.osexchange": {"os_exchange", ""}, // no default key
	"mew.block.setbegin":   {"set_block_begin", "^K B"},
	"mew.block.setend":     {"set_block_end", "^K K"},
	"mew.block.move":       {"block_move", "^K M"},
	"mew.block.copy":       {"block_copy", "^K C"},
	"mew.block.write":      {"block_write", "^K W"},
	"mew.block.read":       {"block_from_file", "^K R"},
	"mew.block.delete":     {"block_delete", "^K Y"},
	"mew.nerd.yank":        {"kill_ring_yank", "esc y"},
	"mew.nerd.yankpop":     {"kill_ring_pop", "esc Y"},

	// The remaining menus, still one placeholder each.
	// Format. Two real items; the rest are planned and run not_implemented,
	// which warns and names what was asked for. Their keys are bound to the
	// same thing in keys_block_menu.conf, so pressing ^K J answers exactly as
	// clicking Justify Block does - an advertised key that did nothing would be
	// the worse half of a placeholder.
	"mew.format.unindent": {"block_unindent", "^K ,"},
	"mew.format.indent":   {"block_indent", "^K ."},
	"mew.format.justify":  {`not_implemented "Justify Block"`, "^K J"},
	"mew.format.filter":   {`not_implemented "Filter Block through Cmd"`, "^K /"},
	"mew.format.upper":    {`not_implemented "Convert to UPPERCASE"`, "^K T"},
	"mew.format.lower":    {`not_implemented "Convert to lowercase"`, ""},
	"mew.format.title":    {`not_implemented "Convert to Title Case"`, ""},
	"mew.format.swapcase": {`not_implemented "Swap camelCase snake_case"`, "^K _"},
	// Viewport. mew.view.desktop is absent on purpose: it toggles the HOST's
	// desktop rather than running a mew command, and is registered separately
	// (like Raw Key Input) against the desktopReveal seam.
	"mew.view.clone":       {"viewport_clone", "^B 2"},
	"mew.view.prior":       {"viewport_prior", "^B P"},
	"mew.view.next":        {"viewport_next", "^B N"},
	"mew.view.follow":      {"nav_follow", "^B F"},
	"mew.view.navmode":     {`set_option_next "navigationMode"`, "^O N"},
	"mew.view.linkprior":   {"nav_prior", "^B S-tab"},
	"mew.view.linknext":    {"nav_next", "^B tab"},
	"mew.view.direction":   {`set_option_next "direction"`, "^O D"},
	"mew.view.showbidi":    {`set_option_next "showBidi"`, "^O B"},
	"mew.input.insertmode": {`set_option_next "insertMode"`, "^O I"},

	// Input. The toggles, then the bidi controls insert_bidi_control already
	// knows by name, then the two "type what you cannot type" prompts. Raw Key
	// Input is NOT here: it is a host command (pass the next key straight to
	// the trinket), registered separately below.
	"mew.input.macoption": {`set_option_next "macOptionKeys"`, "^O G M"},
	"mew.input.showbidi":  {`set_option_next "showBidi"`, "^O B"},
	"mew.input.lrm":       {`insert_bidi_control "lrm"`, ""},
	"mew.input.rlm":       {`insert_bidi_control "rlm"`, ""},
	"mew.input.alm":       {`insert_bidi_control "alm"`, ""},
	"mew.input.fsi":       {`insert_bidi_control "fsi"`, ""},
	"mew.input.lri":       {`insert_bidi_control "lri"`, ""},
	"mew.input.rli":       {`insert_bidi_control "rli"`, ""},
	"mew.input.pdi":       {`insert_bidi_control "pdi"`, ""},
	"mew.input.rawbyte":   {"insert_raw_byte", `esc \`},
	"mew.input.rune":      {"insert_rune", ""},
	// Terminal Diagnostics tests the host's own terminal plumbing and writes
	// the account into the buffer AND a log file beside it. A menu item
	// because the person who needs it is by definition on a machine where
	// something does not work, and may not be at home on that machine.
	"mew.help.ptydiag": {"pty_diag", ""},
	// Raw Key Input is in the table for its KEY: the item advertises whatever
	// raw_key_input is bound to, live, like every other item. Its handler is
	// registered by hand rather than generated from here, because it does one
	// thing more than run the command (see the registration).
	"mew.edit.rawkey": {"raw_key_input", `s-\`},
	// History. The undo-TREE items from the template (Undo History, Rewind to
	// Last Branch, Fast Forward to Leaf/Branch, Show Full History) are absent
	// on purpose: garland has the fork model under it, but Buffer exposes only
	// Undo and Redo, so there is nothing yet for them to run. An item that
	// advertises a key and does nothing is worse than an item that is not there.
	"mew.history.undo":     {"buffer_undo", "^B -"},
	"mew.history.redo":     {"buffer_redo", "^B ="},
	"mew.history.revert":   {"buffer_revert", "^B ~"},
	"mew.history.navprior": {"nav_history_prior", "^B ["},
	"mew.history.navnext":  {"nav_history_next", "^B ]"},
	// nav_clear forgets which links have been VISITED (repainting them to the
	// unvisited style). Not nav_history_clear, which empties the viewport's
	// back/forward stack - a different thing with a confusingly close name.
	"mew.history.navclear": {"nav_clear", ""},

	// Search. The three actions, then the toggles. Every toggle is a
	// set_option_next, so a click cycles the option exactly as its key does —
	// the menu and the keyboard drive one mechanism, not two.
	//
	// searchAllBuffers sits at ^O G (the GLOBAL group) rather than ^O S with
	// the other three, and that is not a naming accident: it is about the SET
	// of buffers a search reaches, not a property of any one document, so
	// unlike ignoreCase/regex/backwards it takes no per-document overlay.
	"mew.search.find":        {"find", "^Q F"},
	"mew.search.findnext":    {"find_next", "^L"},
	"mew.search.incremental": {"search", "^S"},
	"mew.search.ignorecase":  {`set_option_next "searchIgnoreCase"`, "^O S I"},
	"mew.search.regex":       {`set_option_next "searchRegex"`, "^O S R"},
	"mew.search.backwards":   {`set_option_next "searchBackwards"`, "^O S B"},
	"mew.search.wrap":        {`set_option_next "searchWrap"`, "^O S W"},
	"mew.search.allbuffers":  {`set_option_next "searchAllBuffers"`, "^O G S"},
}

// syncPlaceholderShortcuts advertises each placeholder item's key against the
// LIVE keymap, the same way the Help items do: these are mew's keys, not the
// toolkit's, so KittyTK has no Shortcut of its own to show and the text is
// resolved per menu open — which both follows a rebind and covers the menu
// being built before the session binds its port.
func syncPlaceholderShortcuts(menus []*trinkets.Menu, application *app.Application) {
	for _, m := range menus {
		var items []*trinkets.MenuItem
		for _, it := range m.Items() {
			if _, ok := menuActions[it.ID()]; ok {
				items = append(items, it)
			}
		}
		if len(items) == 0 {
			continue
		}
		menuItems := items // captured per menu
		m.SetOnAboutToShow(func() {
			ed, ok := rootMewEditor(application)
			if !ok {
				return
			}
			for _, it := range menuItems {
				spec := menuActions[it.ID()]
				it.SetShortcutText(liveKeyFor(ed, spec))
				syncOptionItem(ed, it)
				// Not a mew option: the desktop's visibility is the HOST's
				// state, so its checkmark comes from the reveal seam.
				if it.ID() == "mew.view.desktop" && desktopReveal.visible != nil {
					it.SetChecked(desktopReveal.visible())
				}
			}
		})
	}
}

// optionItems are the items that READ an option as well as setting it: a
// checkable reflects the option's current state, and a "[?]" caption is filled
// in with the current value. The caption template keeps the "[?]" so a rebuilt
// menu that has not been shown yet still reads as a placeholder rather than
// asserting a value it has not looked up.
var optionItems = map[string]struct {
	option   string            // the option to read
	template string            // caption with "[?]" where the value goes, "" for checkables
	display  map[string]string // raw value -> bracket text, where they differ
}{
	"mew.options.direction": {"direction", "Document &Direction [?]",
		map[string]string{"ltr": "LTR", "rtl": "RTL"}},
	"mew.options.syntax": {"syntax", "Document Synta&x [?]...", nil},
	// insertMode is a BOOLEAN, so its raw value is yes/no - but the bracket
	// names the mode the editor is in, which is what the caption was written to
	// say. Map it back to the words rather than showing "[yes]".
	"mew.input.insertmode": {"insertMode", "&Insert Mode [?]",
		map[string]string{"yes": "Insert", "true": "Insert", "no": "Overwrite", "false": "Overwrite"}},
	"mew.options.readonly":     {"readOnly", "", nil},
	"mew.options.syntaxdetect": {"syntaxDetect", "", nil},
	"mew.search.ignorecase":    {"searchIgnoreCase", "", nil},
	"mew.search.regex":         {"searchRegex", "", nil},
	"mew.search.backwards":     {"searchBackwards", "", nil},
	"mew.search.wrap":          {"searchWrap", "", nil},
	"mew.search.allbuffers":    {"searchAllBuffers", "", nil},
	"mew.input.macoption":      {"macOptionKeys", "", nil},
	"mew.input.showbidi":       {"showBidi", "", nil},
	"mew.view.navmode":         {"navigationMode", "", nil},
	"mew.view.showbidi":        {"showBidi", "", nil},
	"mew.view.direction": {"direction", "Document &Direction [?]",
		map[string]string{"ltr": "LTR", "rtl": "RTL"}},
}

// syncOptionItem refreshes one item's checkmark or "[?]" value from the live
// option cascade, so what the menu shows is what the editor currently holds
// rather than what it held when the bar was built.
func syncOptionItem(ed *trinkets.Editor, it *trinkets.MenuItem) {
	spec, ok := optionItems[it.ID()]
	if !ok {
		return
	}
	value := ed.Option(spec.option)
	if spec.template == "" {
		it.SetChecked(value == "true" || value == "on" || value == "yes")
		return
	}
	if shown, ok := spec.display[value]; ok {
		value = shown
	} else if value == "" {
		// Unset, or read before the session bound its port. "none" is honest
		// for a blank syntax; it beats an empty pair of brackets.
		value = "none"
	}
	it.SetText(strings.Replace(spec.template, "[?]", "["+value+"]", 1))
}

// liveKeyFor resolves an item's shortcut text against the live keymap, or ""
// when the command is bound to nothing.
//
// KeyBinding falls back to the PREFERRED spelling when a command has no key,
// and to the command NAME when there is no preference either — so a command
// that is genuinely unbound would otherwise advertise either a key that does
// nothing or the literal string "set_option_next \"readOnly\"". Both are worse
// than an empty shortcut column, so an unresolved lookup is blanked.
func liveKeyFor(ed *trinkets.Editor, spec menuAction) string {
	key := ed.KeyBinding(spec.cmd, spec.key)
	if key == spec.cmd {
		return "" // nothing bound, and no preferred spelling to fall back on
	}
	return key
}

// syncQuickHelpCheckmark wires the Help menu's about-to-show hook so the Quick
// Help item's checkmark reflects whether the root editor's built-in help
// window is open at the moment the menu is dropped.
func syncQuickHelpCheckmark(menus []*trinkets.Menu, application *app.Application) {
	for _, m := range menus {
		if m.WellKnownID() != "help" {
			continue
		}
		var quickHelp, usingMew *trinkets.MenuItem
		for _, it := range m.Items() {
			switch it.ID() {
			case "mew.help.quickhelp":
				quickHelp = it
			case "mew.help.usingmew":
				usingMew = it
			}
		}
		if quickHelp == nil {
			return
		}
		m.SetOnAboutToShow(func() {
			ed, ok := rootMewEditor(application)
			if ok {
				quickHelp.SetChecked(ed.QuickHelpOpen())
			} else {
				quickHelp.SetChecked(false)
			}
			// These two keys are mew's, not the toolkit's: mew handles them
			// itself, so KittyTK has no Shortcut to show. Advertise them as
			// literal shortcut text instead, resolved against the LIVE keymap
			// exactly as mew's own %keys#…% codes would — so a rebound key
			// shows through here too. Refreshed on every menu open, which also
			// covers the menu being built before the session binds its port.
			if ok {
				quickHelp.SetShortcutText(ed.KeyBinding("help_toggle", "^Q H"))
				if usingMew != nil {
					usingMew.SetShortcutText(ed.KeyBinding(`help_toggle "help:/"`, "^B H"))
				}
			}
		})
		return
	}
}

// showMewAbout opens mew's About dialog as a modal message box on the app. It
// backs both mew's own Help > About and the system (Ψ) menu's About (wired via
// desktop.SetAboutHandler in BuildHost), so the Ψ About shows mew's dialog
// instead of the built-in About KittyTK one.
func showMewAbout(application *app.Application) {
	byID, reply := execProtocol(fmt.Sprintf(
		`dlg=new messagebox icon=information ok title="About mew" text=%s`,
		protocol.Quote("mew "+mew.FullVersion()+"\n\n"+
			"A programmable cross-platform text, prose,\n"+
			"and code editor in the WordStar tradition.\n\n"+
			"mew edits words.")), nil)
	mb := byID[reply.IDs["dlg"]].(*trinkets.MessageBox)
	// Own the dialog by the app's main window so its modal is scoped to (and
	// floats above) that window. Without an owner the manager files it as a
	// system modal, which surfaces but doesn't gate the app's own (solo) surface.
	mb.SetOwner(application.MainWindow())
	application.AddWindow(&mb.Window)
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
