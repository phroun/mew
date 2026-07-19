// Package main demonstrates the KittyTK capabilities.
package main

import (
	"fmt"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/ptydriver"
	"github.com/phroun/kittytk/style"
)

// fixedWidthBox is a bordered panel whose width is pinned to a raw unit
// width (denomination units), so the column occupies the same number of
// units at any font_size and simply grows in pixels as the cell grows.
// Word wrap only happens when width is genuinely constrained. Height is
// NOT pinned: it flows from the content via height-for-width, so wrapping
// onto more lines makes the box taller rather than overflow its border.
type fixedWidthBox struct {
	*trinkets.Panel
	width core.Unit // pinned width in units
}

func newFixedWidthBox(width core.Unit, content core.Trinket) *fixedWidthBox {
	f := &fixedWidthBox{Panel: trinkets.NewPanel(), width: width}
	f.SetBorder(true)
	f.SetBorderStyle(style.BorderSingle) // zero-value BorderStyle renders invisibly

	boxLayout := layout.NewBoxLayout(core.Vertical)
	f.AddChild(content)
	f.SetLayoutManager(boxLayout)
	boxLayout.ItemAt(0).WithAlign(core.AlignFill)
	return f
}

func (f *fixedWidthBox) SizeHint() core.UnitSize {
	w := f.width
	// Height is the content's height AT the pinned width (height-for-
	// width): the width is fixed, so wrapping onto more lines must make
	// the box taller rather than overflow its border. The plain SizeHint
	// height is the content's height at its natural (unwrapped) width,
	// which is too short once the text wraps into the narrow column.
	h := f.Panel.SizeHint().Height
	if hfw, ok := any(f.Panel).(core.HeightForWidther); ok && hfw.HasHeightForWidth() {
		if hh := hfw.HeightForWidth(w); hh > h {
			h = hh
		}
	}
	return core.UnitSize{Width: w, Height: h}
}

// idCaptureFactory records built targets by object ID so the app can
// reach the real trinkets behind surfaced reply names.
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

// Forward EventControl so the script's sub statements and D20 echo
// suppression reach the wrapped RegistryFactory.
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

// protocolWindowScript is the Protocol Demo window's entire content,
// expressed in the display-protocol command language (D10-D18).
const protocolWindowScript = `
alias C="caption"

root=new panel layout=vbox children={
	new label C="This window's content was built from protocol text." wrap
	status=new label C="Interact below; events appear here."
	new separator
	cb=new checkbox C="Tri-state checkbox (watch the label above)" tristate
	inp=new textinput placeholder="Type here..."
	combo=new combobox children={new item C="Alpha"; new item C="Beta"; new item C="Gamma"} selected=0
	btn=new button C="Dispatch demo.hello" action=demo.hello
}
watch=root.status
wcb=root.cb
winp=root.inp
wcombo=root.combo
`

// createProtocolWindow builds the Protocol Demo window through the
// client veneer: typed handles over the wire objects, replica-backed
// reads, write-through setters. No raw dispatcher, no sub statements
// in the script - handles subscribe what they mirror.
func createProtocolWindow(application *app.Application, desktop *trinkets.Desktop) *window.Window {
	conn := client.NewInProcess(func(id string) { application.Commands().Dispatch(id) })
	ui, err := conn.Build(protocolWindowScript)
	if err != nil {
		return nil
	}

	status := ui.Label("watch")

	ui.Checkbox("wcb").OnToggle(func(s protocol.FlagState) {
		state := "off"
		switch s {
		case protocol.FlagTrue:
			state = "on"
		case protocol.FlagIndeterminate:
			state = "mixed"
		}
		status.SetCaption("event toggle checked=" + state)
	})
	ui.TextInput("winp").OnChange(func(s string) {
		status.SetCaption(`event change text="` + s + `"`)
	})
	ui.Selector("wcombo").OnChange(func(i int) {
		status.SetCaption(fmt.Sprintf("event change selected=%d", i))
	})
	conn.OnCommand("demo.hello", func() {
		status.SetCaption("event command action=demo.hello")
	})

	// The command also lands in the app's registry (slice-1 seam).
	application.Commands().Register("demo.hello", func() {
		if sb := desktop.StatusBar(); sb != nil {
			sb.SetText("demo.hello dispatched from protocol-built button!")
		}
	})

	rootTrinket, ok := ui.Object("root").Target().(core.Trinket)
	if !ok {
		return nil
	}
	w := window.NewWindow("Protocol Demo")
	w.SetBounds(core.UnitRect{X: 8 * 8, Y: 16 * 4, Width: 8 * 56, Height: 16 * 16})
	w.SetContent(rootTrinket)
	return w
}

// buildDemo assembles the entire demo application onto a desktop
// whose backend is already set. The same construction runs on the
// text backend and on the SDL pixel backend (selection by binary,
// D23/O3): see main_tui.go and main_sdl.go.
func buildDemo(desktop *trinkets.Desktop) {
	// Create the application - owns windows, provides menu/status content
	application := app.New(nil) // nil backend - Desktop owns it now
	application.SetName("TUI Demo")

	// Set up application's menu bar content
	application.SetMenuBarContent(createMenus(desktop, application))

	// Status bar content is protocol data too: sections of styled
	// text spans.
	application.SetStatusBarContent(buildStatusSections(`
sb=new statusbar children={
	new section children={
		new span text="Ready - Press "
		new span text="F10" fg=red bg=white
		new span text=" for menu, Tab to navigate, "
		new span text="Ctrl+Q" fg=red bg=white
		new span text=" to quit"
	}
}
`))

	// Register application with desktop
	desktop.AddApplication(application)

	// Add event filter for debugging (shows key presses in status bar)
	desktop.AddEventFilter(func(event core.Event) bool {
		if keyEvent, ok := event.(core.KeyPressEvent); ok {
			wm := desktop.WindowManager()
			focusInfo := "no window"
			winInfo := ""
			if wm != nil {
				if activeWin := wm.ActiveWindow(); activeWin != nil {
					fm := activeWin.FocusManager()
					if fm != nil {
						chain := fm.FocusChain()
						focused := fm.FocusedTrinket()

						focusable := 0
						for _, w := range chain {
							if w.IsVisible() && w.IsEnabled() {
								focusable++
							}
						}

						if focused != nil {
							focusInfo = fmt.Sprintf("%T chain:%d ok:%d", focused, len(chain), focusable)
						} else {
							focusInfo = fmt.Sprintf("nil chain:%d ok:%d", len(chain), focusable)
						}
					}

					if keyEvent.Key == "Tab" || keyEvent.Key == "Shift+Tab" {
						bounds := activeWin.Bounds()
						offset := activeWin.ClientAreaOffset()
						state := "normal"
						if activeWin.IsMaximized() {
							state = "MAX"
						}
						winInfo = fmt.Sprintf(" | win:%dx%d@%d,%d content:@%d,%d %s",
							bounds.Width, bounds.Height, bounds.X, bounds.Y,
							offset.X, offset.Y, state)
					}
				}
			}
			if statusBar := desktop.StatusBar(); statusBar != nil {
				statusBar.SetText(fmt.Sprintf("Key: %q  %s%s", keyEvent.Key, focusInfo, winInfo))
			}
		}
		return false // Don't consume the event
	})

	// Create windows in startup callback (after screen bounds are set)
	desktop.SetOnStartup(func() {
		// P0 step 4: a window whose content is built entirely from
		// protocol text, with interactions flowing back as event
		// records. See createProtocolWindow.
		if pw := createProtocolWindow(application, desktop); pw != nil {
			application.AddWindow(pw)
		}
		// Create the main demo window - owned by the application, and
		// mark it the app's main window (its menus/status move to its own
		// chrome when it is torn off).
		mainWindow := createMainWindow(desktop, application)
		application.AddWindow(mainWindow)
		application.SetMainWindow(mainWindow)
		// The demo opens secondary windows (New Window, dialogs, terminals),
		// so it's multi-window: the system supplies its Window menu.
		application.SetMultiWindow(true)

		// D22: this desktop IS a display service. Remote apps dial
		// the socket and appear as full applications:
		//   go run ./examples/remoteapp
		if _, err := display.Serve(desktop, client.DefaultSocketPath()); err != nil {
			if sb := desktop.StatusBar(); sb != nil {
				sb.SetText("display service unavailable: " + err.Error())
			}
		}
	})

}

// demoWindowScript builds the Demo Window, terminal included. The
// feed= pseudo-property streams bytes (with \e / \xNN escapes) into
// the terminal - the banner arrives over the wire before the local
// shell starts.
const demoWindowScript = `
w=new window title="Demo Window" width=480 height=320 children={
	sp=new splitter orientation=vertical position=0.3 caption="Terminal" children={
		tp=new panel layout=vbox spacing=8 children={
			new label caption="This is a child window."
			new textinput placeholder="Type something..."
			closebtn=new button caption="Close"
		}
		term=new terminal
	}
}
closer=w.sp.tp.closebtn
term=w.sp.term

set term feed="\e[1;36mThis banner arrived as protocol text:\e[0m set term feed=\"...\"\r\n\r\n"

sub closer click
`

// createDemoWindow builds a child window with an embedded terminal
// from protocol text.
func createDemoWindow(desktop *trinkets.Desktop, application *app.Application) *window.Window {
	dispatcher := protocol.NewEventDispatcher()
	ctx := &protocol.BindContext{
		Emit: func(ev *protocol.Event) { dispatcher.Dispatch(ev) },
	}
	factory := &idCaptureFactory{
		inner: protocol.NewRegistryFactory(ctx),
		byID:  make(map[uint64]any),
	}
	script, err := protocol.Parse(demoWindowScript)
	if err != nil {
		panic(fmt.Sprintf("demo window script: %v", err))
	}
	reply, err := protocol.NewSession().Execute(script, factory)
	if err != nil {
		panic(fmt.Sprintf("demo window script: %v", err))
	}

	w := factory.byID[reply.IDs["w"]].(*window.Window)
	// The terminal's child process runs client-side: spawn a PTY and
	// bridge it to the built terminal surface (feed in, input/resize out).
	if term, ok := factory.byID[reply.IDs["term"]].(*trinkets.PurfecTerm); ok {
		if drv, err := ptydriver.Start("", term.Feed); err == nil {
			term.SetInputSink(drv.Input)
			term.SetResizeSink(drv.Resize)
		}
	}
	// Main demo windows can be torn off (the %/# title handle);
	// dialogs deliberately can't, so the difference is visible.
	w.SetTearable(true)
	// The close button is subscribed per-connection, so any number of
	// demo windows coexist without command-ID collisions.
	dispatcher.On(reply.IDs["closer"], "click", func(*protocol.Event) {
		w.Close()
	})
	return w
}

// Secondary application counter for unique naming
var secondaryAppCount int

// createSecondaryApplication creates a new application with its own menus and status bar.
func createSecondaryApplication(desktop *trinkets.Desktop) *app.Application {
	secondaryAppCount++
	appNum := secondaryAppCount

	// Create new application (nil backend - Desktop owns it)
	newApp := app.New(nil)
	newApp.SetName(fmt.Sprintf("App %d", appNum))

	// Create simple menu bar for this application
	menus := createSecondaryMenus(desktop, newApp, appNum)
	newApp.SetMenuBarContent(menus)

	// Create unique status bar content
	newApp.SetStatusBarContent(buildStatusSections(fmt.Sprintf(`
sb=new statusbar children={new section children={new span text="Secondary Application #%d"}}
`, appNum)))

	// Create window for this application
	w := window.NewWindow(fmt.Sprintf("App %d Window", appNum))
	offset := (appNum - 1) % 5
	w.SetBounds(core.UnitRect{
		X:      core.Unit((offset*3 + 5) * 8),
		Y:      core.Unit((offset*2 + 3) * 16),
		Width:  core.Unit(60 * 8),
		Height: core.Unit(20 * 16),
	})

	// Create a vertical splitter to divide the window
	splitter := trinkets.NewVSplitter()
	splitter.SetTitle("Terminal")
	splitter.SetPosition(0.3) // Top panel gets 30% of space

	// Top panel with controls
	topPanel := trinkets.NewPanel()
	boxLayout := layout.NewBoxLayout(core.Vertical)
	boxLayout.SetSpacing(8)

	label := trinkets.NewLabel(fmt.Sprintf("This window belongs to Application #%d", appNum))
	topPanel.AddChild(label)

	infoLabel := trinkets.NewLabel("Notice the menu bar and status bar change\nwhen this window is focused.")
	topPanel.AddChild(infoLabel)

	textInput := trinkets.NewTextInput()
	textInput.SetPlaceholder("Enter text here...")
	topPanel.AddChild(textInput)

	closeButton := trinkets.NewButton("Close Window")
	closeButton.SetOnClick(func() {
		w.Close()
	})
	topPanel.AddChild(closeButton)

	topPanel.SetLayoutManager(boxLayout)
	splitter.SetFirst(topPanel)

	// Bottom panel with PurfecTerm terminal
	terminal := trinkets.NewPurfecTerm()

	// Debug callback - show clicked cell info in status bar
	terminal.SetOnCellClicked(func(info trinkets.CellDebugInfo) {
		// Format attributes (B=bold, U=underline, R=reverse)
		attrs := ""
		if info.Bold {
			attrs += "B"
		}
		if info.Underline {
			attrs += "U"
		}
		if info.Reverse {
			attrs += "R"
		}
		if attrs == "" {
			attrs = "-"
		}

		// Format colors
		var fg, bg string
		switch info.FgType {
		case "RGB":
			fg = fmt.Sprintf("RGB(%d,%d,%d)", info.FgR, info.FgG, info.FgB)
		case "256":
			fg = fmt.Sprintf("256[%d]", info.FgIndex)
		case "Std":
			fg = fmt.Sprintf("Std[%d]", info.FgIndex)
		default:
			fg = "Def"
		}
		switch info.BgType {
		case "RGB":
			bg = fmt.Sprintf("RGB(%d,%d,%d)", info.BgR, info.BgG, info.BgB)
		case "256":
			bg = fmt.Sprintf("256[%d]", info.BgIndex)
		case "Std":
			bg = fmt.Sprintf("Std[%d]", info.BgIndex)
		default:
			bg = "Def"
		}

		// Format character (handle non-printable)
		charStr := fmt.Sprintf("'%c'", info.Char)
		if info.Char < 32 || info.Char == 127 {
			charStr = fmt.Sprintf("0x%02X", info.Char)
		}

		// Dynamic update: rebuild the status content as protocol
		// text; protocol.Quote makes arbitrary cell contents safe.
		debugText := fmt.Sprintf("[%d,%d] %s Fg:%s Bg:%s Attr:%s",
			info.Col, info.Row, charStr, fg, bg, attrs)
		newApp.SetStatusBarContent(buildStatusSections(
			`sb=new statusbar children={new section text=` + protocol.Quote(debugText) + `}`))
	})

	splitter.SetSecond(terminal)

	// The child process runs client-side: spawn a PTY here and bridge it
	// to the terminal surface - its output is fed in, and the user's input
	// and grid resizes are written back to the PTY.
	if drv, err := ptydriver.Start("", terminal.Feed); err == nil {
		terminal.SetInputSink(drv.Input)
		terminal.SetResizeSink(drv.Resize)
	}

	w.SetContent(splitter)

	// This window is the main window of its own application, and can be
	// torn off the desktop to become a standalone OS surface.
	w.SetTearable(true)

	newApp.AddWindow(w)
	newApp.SetMainWindow(w)

	return newApp
}

// showAboutDialog shows the about dialog.
// buildStatusSections executes a statusbar script and returns the
// section list for SetStatusBarContent.
func buildStatusSections(script string) []trinkets.StatusSection {
	factory := &idCaptureFactory{
		inner: protocol.NewRegistryFactory(&protocol.BindContext{}),
		byID:  make(map[uint64]any),
	}
	parsed, err := protocol.Parse(script)
	if err != nil {
		panic(fmt.Sprintf("statusbar script: %v", err))
	}
	reply, err := protocol.NewSession().Execute(parsed, factory)
	if err != nil {
		panic(fmt.Sprintf("statusbar script: %v", err))
	}
	bar := factory.byID[reply.IDs["sb"]].(interface {
		Sections() []trinkets.StatusSection
	})
	return bar.Sections()
}

// protocolMessageBox executes a messagebox script and shows the
// dialog. Dialogs are one-shot protocol objects: built from text,
// closed by their own buttons (the finish event is available via a
// sub statement in the script when a caller cares).
// buildMessageBox executes a messagebox script and returns the dialog.
func buildMessageBox(script string) *trinkets.MessageBox {
	factory := &idCaptureFactory{
		inner: protocol.NewRegistryFactory(&protocol.BindContext{}),
		byID:  make(map[uint64]any),
	}
	parsed, err := protocol.Parse(script)
	if err != nil {
		panic(fmt.Sprintf("messagebox script: %v", err))
	}
	reply, err := protocol.NewSession().Execute(parsed, factory)
	if err != nil {
		panic(fmt.Sprintf("messagebox script: %v", err))
	}
	return factory.byID[reply.IDs["dlg"]].(*trinkets.MessageBox)
}

func protocolMessageBox(application *app.Application, script string) {
	application.AddWindow(&buildMessageBox(script).Window)
}

func showAboutDialog(desktop *trinkets.Desktop, application *app.Application) {
	dlg := buildMessageBox(fmt.Sprintf(`
dlg=new messagebox title="About %s" icon=information ok text="%s Demo\n\nA comprehensive cross-surface UI toolkit.\n\nVersion %s"
`, core.Name, core.Name, core.Version))
	// The About belongs to this application, so add it through the app: as a
	// modal-type window it becomes an APPLICATION modal (blocking this app's
	// windows across the desktop and any torn-off surfaces), never a system
	// modal. System modals are reserved for the desktop's own prompts.
	_ = desktop
	application.AddWindow(&dlg.Window)
}
