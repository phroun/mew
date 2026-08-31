package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/ptydriver"
)

// wireTerminal drives a terminal surface's child process from the client
// side: it spawns a PTY, streams the child's output in through feed=, and
// writes the terminal's input/resize events back to the PTY. The driver is
// registered for cleanup when the app quits.
func (a *app) wireTerminal(term client.Handle) {
	drv, err := ptydriver.Start("", func(b []byte) {
		_ = term.Set("feed=" + protocol.Quote(string(b)))
	})
	if err != nil {
		return
	}
	a.drivers = append(a.drivers, drv)
	term.On("input", func(ev *protocol.Event) {
		if s, ok := ev.Text("data"); ok {
			drv.Input([]byte(s))
		}
	})
	term.On("resize", func(ev *protocol.Event) {
		cols, okc := ev.Int("cols")
		rows, okr := ev.Int("rows")
		if okc && okr {
			drv.Resize(cols, rows)
		}
	})
}

// wireMainWindow subscribes the demo window's interactive trinkets:
// the basic-trinket narration, the font/denomination toggles (window
// and desktop properties), and the tab background-color radios.
func (a *app) wireMainWindow() {
	ui := a.ui
	win := ui.Object("w")
	tabs := ui.Object("tabs")

	// Basic Trinkets: the text input narrates changes to the status bar.
	ui.TextInput("binput").OnChange(func(s string) { a.setStatus("Text: " + s) })

	// Selection: font / denomination toggles are window and desktop
	// properties, set over the wire.
	ui.Checkbox("wfont").OnToggle(func(s protocol.FlagState) {
		if s == protocol.FlagTrue {
			_ = win.Set(`font="tuesday12"`)
		} else {
			_ = win.Set(`font="default"`)
		}
	})
	ui.Checkbox("dfont").OnToggle(func(s protocol.FlagState) {
		if s == protocol.FlagTrue {
			_, _ = a.conn.Exec("desktopfont tuesday")
		} else {
			_, _ = a.conn.Exec("desktopfont default")
		}
	})
	ui.Checkbox("grid").OnToggle(func(s protocol.FlagState) {
		if s == protocol.FlagTrue {
			_ = win.Set("denomination=32")
		} else {
			_ = win.Set("denomination=0")
		}
	})

	// Tab background radios: the Selection and Scroll Selection tabs
	// share the same three options (default, dark green, TrueColor).
	setBG := func(arg string) func(*protocol.Event) {
		return func(ev *protocol.Event) {
			if ev.Flag("checked") == protocol.FlagTrue {
				_ = tabs.Set("background=" + arg)
			}
		}
	}
	green := "green"
	gray := `"#333333"`
	def := "default"
	ui.Object("bgdef").On("toggle", setBG(def))
	ui.Object("bggreen").On("toggle", setBG(green))
	ui.Object("bggray").On("toggle", setBG(gray))
	ui.Object("sbgdef").On("toggle", setBG(def))
	ui.Object("sbggreen").On("toggle", setBG(green))
	ui.Object("sbggray").On("toggle", setBG(gray))

	// Text Fields: the two events a field raises, side by side. change
	// fires per edit and complete once, when the person says they are done.
	tfecho := ui.Object("tfecho")
	watch := ui.TextInput("tfwatch")
	watch.OnChange(func(s string) {
		_ = tfecho.Set("caption=" + protocol.Quote("change: "+s))
	})
	watch.OnComplete(func(s string) {
		_ = tfecho.Set("caption=" + protocol.Quote("COMPLETE: "+s))
	})

	// ...and the mask character, which is a property of the field rather
	// than of the echo mode: switching to "show" turns masking off without
	// disturbing which character it would have used. Written the way the
	// other radio groups here are -- the raw toggle event, checking the
	// flag, because a radio group reports every button that changed.
	tfmask := ui.Object("tfmask")
	setMask := func(arg string) func(*protocol.Event) {
		return func(ev *protocol.Event) {
			if ev.Flag("checked") == protocol.FlagTrue {
				_ = tfmask.Set(arg)
			}
		}
	}
	ui.Object("tfmb").On("toggle", setMask(`echo=password mask="•"`))
	ui.Object("tfms").On("toggle", setMask(`echo=password mask="*"`))
	ui.Object("tfmh").On("toggle", setMask(`echo=password mask="#"`))
	ui.Object("tfmn").On("toggle", setMask(`echo=normal`))

	a.wireDenomination(win)
	a.wireTerminalTab(tabs)
}

// terminalTabIndex is the Terminal tab's position in the main window's
// strip. The change event reports an index, so the tab has to be named by
// one; a test checks the caption at this index is still "Terminal".
const terminalTabIndex = 12

// wireTerminalTab drives the Terminal tab's surface. The PTY starts the
// first time the tab is selected rather than at build: a shell is a child
// process, and a tab nobody opened should not have one.
func (a *app) wireTerminalTab(tabs client.Handle) {
	ui := a.ui
	term := ui.Object("mterm")

	// ESC [ 2 J clears the screen, ESC [ H puts the cursor home -- fed in
	// as if the child had written them, which is the only direction the
	// display accepts.
	ui.Button("mtclear").OnClick(func() {
		_ = term.Set(`feed="\e[2J\e[H"`)
	})

	tabs.On("change", func(ev *protocol.Event) {
		if i, ok := ev.Int("selected"); !ok || i != terminalTabIndex {
			return
		}
		if a.terminalStarted {
			return
		}
		a.terminalStarted = true
		a.wireTerminal(term)
	})
}

// What the Denomination tab will apply. One unit per cell is the floor
// because the cell conversions divide by it -- UnitsToCellX and its row
// counterpart -- so zero is not a small denomination, it is a division by
// zero. The ceiling is a demo's own limit rather than the model's.
const (
	denomMin = 1
	denomMax = 64
)

// wireDenomination drives the window's own column_units and row_units --
// the per-axis D8 spelling, which is what makes X and Y independent. The
// Selection tab's grid checkbox reaches the same machinery through the
// window's denomination property, but that one sets the row height alone
// and resets the column to its default.
func (a *app) wireDenomination(win client.Handle) {
	ui := a.ui
	echo := ui.Object("dnecho")
	x := ui.TextInput("dnx")
	y := ui.TextInput("dny")

	say := func(s string) {
		_ = echo.Set("caption=" + protocol.Quote(s))
		a.setStatus(s)
	}

	// apply reads both fields, so pressing Return in either one commits
	// the pair the person can see rather than half of it.
	apply := func() {
		cx, errX := strconv.Atoi(strings.TrimSpace(x.Text()))
		cy, errY := strconv.Atoi(strings.TrimSpace(y.Text()))
		switch {
		case errX != nil || errY != nil:
			say("Denomination: X and Y must both be whole numbers.")
			return
		case cx < denomMin || cx > denomMax || cy < denomMin || cy > denomMax:
			say(fmt.Sprintf("Denomination: %d x %d is out of range (%d to %d).",
				cx, cy, denomMin, denomMax))
			return
		}
		if err := win.Set(fmt.Sprintf("column_units=%d row_units=%d", cx, cy)); err != nil {
			say("Denomination: " + err.Error())
			return
		}
		say(fmt.Sprintf("Denomination is now %d x %d.", cx, cy))
	}

	// Return in either field applies, which is what complete is for: the
	// person saying they are done with the value, not every keystroke.
	x.OnComplete(func(string) { apply() })
	y.OnComplete(func(string) { apply() })
	ui.Button("dnap").OnClick(apply)

	// A preset fills the fields as well as applying, so the two never
	// disagree about what the window is showing.
	preset := func(cx, cy int) func() {
		return func() {
			_ = x.SetText(strconv.Itoa(cx))
			_ = y.SetText(strconv.Itoa(cy))
			apply()
		}
	}
	ui.Button("dnd").OnClick(preset(8, 16))
	ui.Button("dnh").OnClick(preset(4, 8))
	ui.Button("dnt").OnClick(preset(16, 32))
	ui.Button("dns").OnClick(preset(16, 16))
	ui.Button("dnn").OnClick(preset(8, 32))
}

// wireMenus registers the primary application's command handlers. The
// desktop-reaching actions (edit ops, theme, tiling, announcements) go
// out as display app-verbs; the rest are handled here in the client.
func (a *app) wireMenus() {
	c := a.conn

	// Demo menu.
	c.OnCommand("demo.file.new", func() { a.openTerminalWindow() })

	// Edit menu: Cut/Copy/Paste/Select All are supplied by the host's
	// system Edit menu and act on the focused trinket directly; the client
	// only contributes the custom Raw Key Input item.
	c.OnCommand("demo.edit.rawkey", func() { _, _ = c.Exec("rawkey") })

	// View menu.
	c.OnCommand("demo.view.theme", func() { _, _ = c.Exec("theme") })
	c.OnCommand("demo.view.announce", func() { _, _ = c.Exec("announce_visual") })
	c.OnCommand("demo.view.speak", func() { _, _ = c.Exec("announce_speak") })

	// Window menu: New Window is a whole new application (connection).
	// Dial it on its own goroutine: openSecondary blocks on a fresh
	// handshake (including the host's approval prompt) and build, and the
	// command handler runs on the connection's single event-delivery
	// goroutine - blocking here would starve all further events for this
	// connection until it returns.
	c.OnCommand("demo.window.new", func() { go openSecondary(a.path) })

	// Basic Trinkets buttons narrate to the status bar.
	c.OnCommand("demo.basic.ok", func() { a.setStatus("OK button clicked!") })
	c.OnCommand("demo.basic.cancel", func() { a.setStatus("Cancel button clicked!") })
	c.OnCommand("demo.basic.apply", func() { a.setStatus("Apply button clicked!") })

	// Nested menu. Both narrate to the status bar, which is the point: a
	// trigger from four submenus down arrives as the same command event as
	// one from the menu bar, carrying nothing about the depth it came from.
	c.OnCommand("demo.nested.pick", func() { a.setStatus("Nested: ordinary item") })
	c.OnCommand("demo.nested.deep", func() { a.setStatus("Nested: fired from level 4") })

	// Help menu.
	c.OnCommand("demo.help.about", func() { a.showAbout() })
}

// wireMDI wires the MDI Demo tab: window-management buttons, the dock
// choreography (minimize -> entry, entry click -> restore), and the
// active-document label - all over pane and dock events.
func (a *app) wireMDI() {
	ui := a.ui
	c := a.conn
	mdi := ui.Object("mdi")
	status := ui.Label("mdistatus")

	c.OnCommand("demo.mdi.spawn", func() { a.spawnMDIChild() })
	c.OnCommand("demo.mdi.tile", func() { _ = mdi.Set("tile") })
	c.OnCommand("demo.mdi.cascade", func() { _ = mdi.Set("cascade") })
	c.OnCommand("demo.mdi.next", func() { _ = mdi.Set("next") })
	c.OnCommand("demo.mdi.prior", func() { _ = mdi.Set("prior") })

	entries := make(map[uint64]client.Handle) // window id -> dock entry
	dropEntry := func(winID uint64) {
		if h, ok := entries[winID]; ok {
			_ = h.Destroy()
			delete(entries, winID)
		}
	}

	mdi.On("minimize", func(ev *protocol.Event) {
		winID, _ := ev.Uint("window")
		title, _ := ev.Text("title")
		dropEntry(winID) // never two entries for one window
		a.dockSeq++
		key := fmt.Sprintf("e%d", a.dockSeq)
		entryUI, err := c.Build(fmt.Sprintf(
			"set mdidock children={%s=new dockentry caption=%s window=%d}\nwentry=mdidock.%s",
			key, protocol.Quote(title), winID, key))
		if err != nil {
			return
		}
		entry := entryUI.Object("wentry")
		entry.On("click", func(*protocol.Event) {
			// D20: our own set never echoes a restore event, so the
			// initiator drops its own dock entry.
			if mdi.Set(fmt.Sprintf("restore=%d", winID)) == nil {
				dropEntry(winID)
			}
		})
		entries[winID] = entry
	})
	mdi.On("restore", func(ev *protocol.Event) {
		if id, ok := ev.Uint("window"); ok {
			dropEntry(id)
		}
	})
	mdi.On("remove", func(ev *protocol.Event) {
		if id, ok := ev.Uint("window"); ok {
			dropEntry(id)
		}
	})
	mdi.On("active", func(ev *protocol.Event) {
		if title, ok := ev.Text("title"); ok && title != "" {
			_ = status.SetCaption("Active: " + title)
		} else {
			_ = status.SetCaption("Active: none")
		}
	})

	a.spawnMDIChild() // the initial document
}

// spawnMDIChild appends one document window into the MDI pane and wires
// its New/Close buttons through click events (no per-child command IDs).
func (a *app) spawnMDIChild() {
	a.mdiCount++
	ui, err := a.conn.Build(mdiChildScript(a.mdiCount))
	if err != nil {
		return
	}
	winID := ui.ID("wwin")
	ui.Button("wnew").OnClick(func() { a.spawnMDIChild() })
	ui.Button("wclose").OnClick(func() {
		_ = a.ui.Object("mdi").Set(fmt.Sprintf("remove=%d", winID))
	})
}

// openProtocolWindow builds the companion window whose content is all
// protocol text, narrating its interactions to its own label.
func (a *app) openProtocolWindow() {
	ui, err := a.conn.Build(protocolWindowScript)
	if err != nil {
		return
	}
	status := ui.Label("pstatus")
	ui.Checkbox("pcb").OnToggle(func(s protocol.FlagState) {
		state := "off"
		switch s {
		case protocol.FlagTrue:
			state = "on"
		case protocol.FlagIndeterminate:
			state = "mixed"
		}
		_ = status.SetCaption("event toggle checked=" + state)
	})
	ui.TextInput("pinp").OnChange(func(s string) {
		_ = status.SetCaption(`event change text="` + s + `"`)
	})
	ui.Selector("pcombo").OnChange(func(i int) {
		_ = status.SetCaption(fmt.Sprintf("event change selected=%d", i))
	})
	// demo.hello is dispatched by the button; it also lands on the
	// primary connection's command handler (see wireMenus' sibling).
	a.conn.OnCommand("demo.hello", func() {
		_ = status.SetCaption("event command action=demo.hello")
		a.setStatus("demo.hello dispatched from protocol-built button!")
	})
}

// openTerminalWindow builds the "Demo Window" (Demo > New): a control
// panel over an embedded shell terminal, with a working Close button.
func (a *app) openTerminalWindow() {
	a.mdiCount++ // reuse the counter for a unique key/offset per window
	n := a.mdiCount
	ui, err := a.conn.Build(demoTerminalScript(n))
	if err != nil {
		return
	}
	win := ui.Window("dwin")
	ui.Button("dcloser").OnClick(func() { _ = win.Close() })
	a.wireTerminal(ui.Object("dterm"))
}

// showAbout opens the About message box.
func (a *app) showAbout() {
	_, _ = a.conn.Exec(aboutDialogScript)
}

// openSecondary dials a new connection - a new Application with its own
// window, menu bar and status bar - the meaning of "New Window".
func openSecondary(path string) {
	secondaryMu.Lock()
	secondaryCount++
	n := secondaryCount
	secondaryMu.Unlock()

	sec, err := newApp(path, fmt.Sprintf("App %d", n), false)
	if err != nil {
		return
	}
	ui, err := sec.conn.Build(secondaryBuildScript(n))
	if err != nil {
		sec.conn.Close()
		return
	}
	sec.ui = ui
	sec.wireSecondary(n)
}

// wireSecondary wires a secondary application's window: its Close
// button and its own menu commands (edit ops, info/about dialogs).
func (a *app) wireSecondary(n int) {
	ui := a.ui
	c := a.conn

	ui.Button("closer").OnClick(func() { _ = ui.Window("w").Close() })
	a.wireTerminal(ui.Object("term"))

	c.OnCommand("demo.app.close", func() { _ = ui.Window("w").Close() })
	// Cut/Copy/Paste/Select All come from the host's system Edit menu; the
	// client only wires the custom Raw Key Input item.
	c.OnCommand("demo.app.rawkey", func() { _, _ = c.Exec("rawkey") })
	c.OnCommand("demo.app.info", func() {
		_, _ = c.Exec(fmt.Sprintf(
			`dlg=new messagebox icon=information ok title="About App %d" text="This is Secondary Application #%d\n\nIt has its own menus and status bar."`,
			n, n))
	})
	c.OnCommand("demo.app.about", func() {
		_, _ = c.Exec(`dlg=new messagebox icon=information ok title="About" text="Secondary Application\n\nDemonstrates multi-application support."`)
	})

	// Closing the window ends this secondary connection.
	ui.Window("w").OnClosed(func() { a.conn.Close() })
}

// wireDetails fills the Details tab's column values (the two-batch
// pattern: the build surfaced the item IDs, this batch references
// them), narrates sort requests, and wires the feature-toggle row.
// Sorting itself is built into the trinket (visual reorder only; the
// item order the app owns never moves) - the status line just shows
// the app CAN observe it.
func (a *app) wireDetails() {
	ui := a.ui
	dtree := ui.Object("dtree")
	if !dtree.Valid() {
		return
	}
	_, _ = a.conn.Exec(detailsValuesScript(func(name string) uint64 {
		return ui.Object(name).ID()
	}))
	dtree.On("sort", func(ev *protocol.Event) {
		if ev.Flag("sorted") != protocol.FlagTrue {
			a.setStatus("Details: unsorted (app order)")
			return
		}
		by, _ := ev.Int("sortedby")
		dir := "ascending"
		if ev.Flag("descending") == protocol.FlagTrue {
			dir = "descending"
		}
		colName := "Name"
		if by >= 0 {
			colName = fmt.Sprintf("column %d", by)
		}
		a.setStatus(fmt.Sprintf("Details: sort by %s, %s", colName, dir))
	})
	// In-place cell edits (Kind and Tags are editable): the trinket
	// already updated the cell; this is observation only.
	dtree.On("edit", func(ev *protocol.Event) {
		col, _ := ev.Int("column")
		value, _ := ev.Text("value")
		colName := fmt.Sprintf("column %d", col)
		if col < 0 {
			colName = "Name" // the key column reports index -1
		}
		a.setStatus(fmt.Sprintf("Details: edited %s -> %q", colName, value))
	})

	// Feature toggles: key column visibility, the horizontal-scroll
	// model, and pinned columns on either side.
	ui.Checkbox("dshowkey").OnToggle(func(s protocol.FlagState) {
		if s == protocol.FlagTrue {
			_ = dtree.Set("showkey")
		} else {
			_ = dtree.Set("!showkey")
		}
	})
	ui.Checkbox("dhscroll").OnToggle(func(s protocol.FlagState) {
		if s == protocol.FlagTrue {
			_ = dtree.Set("!fit_width")
		} else {
			_ = dtree.Set("fit_width")
		}
	})
	ui.Checkbox("dpinl").OnToggle(func(s protocol.FlagState) {
		n := 0
		if s == protocol.FlagTrue {
			n = 2
		}
		_ = dtree.Set(fmt.Sprintf("fixed_left=%d", n))
	})
	ui.Checkbox("dpinr").OnToggle(func(s protocol.FlagState) {
		n := 0
		if s == protocol.FlagTrue {
			n = 1
		}
		_ = dtree.Set(fmt.Sprintf("fixed_right=%d", n))
	})
	ui.Checkbox("dledger").OnToggle(func(s protocol.FlagState) {
		if s == protocol.FlagTrue {
			_ = dtree.Set("ledger")
		} else {
			_ = dtree.Set("!ledger")
		}
	})
	ui.Checkbox("dlines").OnToggle(func(s protocol.FlagState) {
		if s == protocol.FlagTrue {
			_ = dtree.Set("treelines")
		} else {
			_ = dtree.Set("!treelines")
		}
	})
}
