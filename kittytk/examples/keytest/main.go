// Command keytest is an event viewer: a KittyTK host with one solo window that
// shows, row by row, every event the desktop receives.
//
// It exists because a keystroke's trip through this toolkit crosses layers that
// each look correct alone, and the only way to tell which one is lying is to
// read what actually arrived. A browser's own event viewer (the W3C Keyboard
// Event Viewer, say) shows the far end of that trip; this shows the near end,
// in the same shape, so the two can be put side by side.
//
// Deliberately there is no terminal surface in it. A PurfecTerm trinket would
// re-encode every key for a guest and put four more layers between the backend
// and the screen; here an event filter on the desktop sees each event as the
// backend delivered it, before any trinket has had a say.
//
// Build it the way the host it mimics is built: plain for the TUI backend,
// -tags sdl for the graphical one. Comparing the two is the point — the same
// keystroke can arrive differently on each, and this is what shows that.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
)

// maxRows bounds the log. A held key fills a screen in a second and a mouse
// crossing the window fills hundreds of rows, so the oldest are dropped rather
// than letting the flat list grow without limit.
const maxRows = 2000

// viewer holds the trinkets the event filter writes into.
type viewer struct {
	tree      *trinkets.TreeView
	status    *trinkets.Label
	modeLabel *trinkets.Label
	modes     core.ModeSource
	showMouse bool
	seq       int
}

func main() {
	desktop := trinkets.NewDesktop()

	// The backend goes on FIRST — it seeds the desktop's cell metrics, and
	// solo mode below sizes the window against them. This is the order the mew
	// host uses for the same reason.
	//
	// It also hands back whatever knows the keyboard's MODES, which is a
	// different object on each host: the terminal backend itself, or the
	// graphical platform behind the backend. That is the whole reason it is
	// returned from here rather than reached for below.
	runDesktop, modes := attachBackend(desktop)

	application := app.New(nil)
	application.SetName("keytest")
	application.SetMultiWindow(false)

	v := &viewer{modes: modes}
	root := window.NewWindow("KittyTK event viewer")
	// A size to exist at before solo mode maximizes it — a window that starts
	// at zero has nothing to lay the tree out against on the first frame.
	root.SetBounds(core.UnitRect{Width: 8 * 100, Height: 16 * 30})
	root.SetContent(v.build())

	application.AddWindow(root)
	application.SetMainWindow(root)
	desktop.AddApplication(application)

	// One window filling the display, as the mew host does: no wallpaper, no
	// system menu, no frame — just the viewer.
	desktop.EnterSoloMode(root)
	if wm := desktop.WindowManager(); wm != nil {
		wm.MaximizeWindow(root)
		wm.ActivateWindow(root)
	}

	// The filter runs BEFORE any trinket sees the event and returns false, so
	// nothing is consumed: the tree still scrolls, the checkbox still toggles,
	// and what is logged is what the rest of the toolkit is about to be given.
	desktop.AddEventFilter(func(ev core.Event) bool {
		v.log(ev)
		return false
	})

	os.Exit(runDesktop())
}

// build assembles the window content: the log, a filter row, and a hint line.
func (v *viewer) build() core.Trinket {
	v.tree = trinkets.NewTreeView()
	v.tree.SetShowHeader(true)
	v.tree.SetKeyCaption("#")
	v.tree.SetLedger(true) // alternating row tint: long runs stay readable
	for _, c := range []*trinkets.TreeColumn{
		{ID: "event", Caption: "Event", Width: 14, Resizable: true},
		{ID: "key", Caption: "Key", Width: 16, Resizable: true},
		{ID: "mods", Caption: "Modifiers", Width: 22, Resizable: true},
		{ID: "repeat", Caption: "Repeat", Width: 7, Align: "center"},
		{ID: "text", Caption: "Text", Width: 8, Resizable: true},
		{ID: "detail", Caption: "Detail", Width: 40, Resizable: true},
	} {
		v.tree.AddColumn(c)
	}

	// Mouse motion alone produces an event per pixel of travel, which buries
	// the keystroke that was being looked for. Off by default for that reason;
	// the button and wheel events are grouped with it because they are noisy
	// together and the question is nearly always about the keyboard.
	mouse := trinkets.NewCheckbox("Show mouse events")
	mouse.SetOnToggled(func(checked bool) { v.showMouse = checked })

	clear := trinkets.NewButton("Clear")
	clear.SetOnClick(func() {
		v.tree.Clear()
		v.seq = 0
		v.setStatus("cleared")
	})

	controls := trinkets.NewPanel()
	controlsLayout := layout.NewBoxLayout(core.Horizontal)
	controlsLayout.SetSpacing(2)
	controls.SetLayoutManager(controlsLayout)
	controls.AddChild(mouse)
	controls.AddChild(clear)
	v.status = trinkets.NewLabel("waiting for events")
	controls.AddChild(v.status)

	// The keyboard's standing states, which are not events and so appear in no
	// row: the pad's lock, Caps Lock, and whether this window has the keyboard.
	// A state the host cannot see is left out entirely rather than shown as
	// off — the two are different facts, and which ones a host can see is one
	// of the things worth comparing between the two builds.
	v.modeLabel = trinkets.NewLabel("")
	controls.AddChild(v.modeLabel)
	v.refreshModes()

	rootPanel := trinkets.NewPanel()
	rootLayout := layout.NewBoxLayout(core.Vertical)
	rootLayout.SetSpacing(1)
	rootPanel.SetLayoutManager(rootLayout)
	rootPanel.AddChild(v.tree)
	rootPanel.AddChild(controls)
	// The tree takes the slack; the control row keeps its natural height.
	rootLayout.ItemAt(0).WithStretch(1).WithAlign(core.AlignFill)

	return rootPanel
}

func (v *viewer) setStatus(s string) {
	if v.status != nil {
		v.status.SetText(s)
	}
}

// refreshModes rewrites the mode line: "Num:on  Caps:off  Focus:on".
//
// Read from the host rather than accumulated from the events, so a state that
// moved without an announcement — Caps Lock pressed while another window had
// the keyboard — is right the moment anything else happens. The list is
// whatever the host can answer for, so a mode the host or the application
// published itself appears here with no code added.
func (v *viewer) refreshModes() {
	if v.modeLabel == nil {
		return
	}
	if v.modes == nil {
		v.modeLabel.SetText("modes: not reported by this host")
		return
	}
	var parts []string
	for _, m := range v.modes.Modes() {
		parts = append(parts, capitalize(m.Name)+":"+m.Value)
	}
	if len(parts) == 0 {
		v.modeLabel.SetText("modes: none known yet")
		return
	}
	v.modeLabel.SetText(strings.Join(parts, "  "))
}

// capitalize titles a mode's token for display. The tokens are lowercase
// because they are matched, not read; a status bar is read.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// log appends one row for an event, or drops it when the mouse filter is off.
func (v *viewer) log(ev core.Event) {
	// Before the filter below: a mode can move on an event whose row is
	// hidden, and the line should still be right.
	v.refreshModes()

	name, key, mods, repeat, text, detail, isMouse := describe(ev)
	if isMouse && !v.showMouse {
		return
	}
	if name == "" {
		return // an event kind with nothing worth a row
	}

	v.seq++
	item := trinkets.NewTreeItem(fmt.Sprintf("%d", v.seq))
	item.SetValue("event", name)
	item.SetValue("key", key)
	item.SetValue("mods", mods)
	item.SetValue("repeat", repeat)
	item.SetValue("text", text)
	item.SetValue("detail", detail)
	v.tree.AddRootItem(item)

	if roots := v.tree.RootItems(); len(roots) > maxRows {
		v.tree.RemoveRootItem(roots[0])
	}
	// Follow the tail, the way a log window does.
	v.tree.SetCurrentIndex(len(v.tree.RootItems()) - 1)
	v.setStatus(fmt.Sprintf("%d events", v.seq))
}

// describe renders one event as the row's cells. isMouse marks the ones the
// filter checkbox hides.
func describe(ev core.Event) (name, key, mods, repeat, text, detail string, isMouse bool) {
	switch e := ev.(type) {
	case core.KeyPressEvent:
		repeat = "-"
		if e.Repeat {
			repeat = "yes"
		}
		return "KeyPress", quote(e.Key), modString(e.Modifiers), repeat, quote(e.Text), "", false

	case core.KeyReleaseEvent:
		// No Text and no Repeat on a release: the event carries neither, which
		// is itself worth seeing next to the press that preceded it.
		return "KeyRelease", quote(e.Key), modString(e.Modifiers), "-", "-", "", false

	case core.PasteEvent:
		return "Paste", "", "", "-", quote(abbreviate(e.Text)), fmt.Sprintf("%d bytes", len(e.Text)), false

	case core.MousePressEvent:
		return "MousePress", buttonName(e.Button), modString(e.Modifiers), "-", "",
			fmt.Sprintf("at %d,%d", e.X, e.Y), true

	case core.MouseReleaseEvent:
		return "MouseRelease", buttonName(e.Button), modString(e.Modifiers), "-", "",
			fmt.Sprintf("at %d,%d", e.X, e.Y), true

	case core.MouseMoveEvent:
		held := ""
		if e.Buttons != core.NoButton {
			held = " holding " + buttonName(e.Buttons)
		}
		return "MouseMove", "", modString(e.Modifiers), "-", "",
			fmt.Sprintf("at %d,%d%s", e.X, e.Y, held), true

	case core.MouseWheelEvent:
		d := fmt.Sprintf("at %d,%d delta %d,%d", e.X, e.Y, e.DeltaX, e.DeltaY)
		if e.PreciseX != 0 || e.PreciseY != 0 {
			d += fmt.Sprintf(" precise %.2f,%.2f", e.PreciseX, e.PreciseY)
		}
		return "MouseWheel", "", modString(e.Modifiers), "-", "", d, true

	case core.MouseLeaveEvent:
		return "MouseLeave", "", "", "-", "", "pointer left the surface", true

	case core.ResizeEvent:
		return "Resize", "", "", "-", "",
			fmt.Sprintf("%dx%d units, %dx%d cells", e.Width, e.Height, e.Cols, e.Rows), false

	case core.FocusEvent:
		state := "lost"
		if e.Focused {
			state = "gained"
		}
		return "Focus", "", "", "-", "", state, false

	case core.ModeEvent:
		// A state moved. The row records WHEN, which the mode line cannot
		// show: the line is the state now, this is the moment it changed.
		return "Mode", e.Name, "", "-", "", "now " + e.Value, false

	case core.QuitEvent:
		return "Quit", "", "", "-", "", "", false
	}
	return "", "", "", "", "", "", false
}

// modString names the modifiers a KeyModifiers mask holds, in the canonical
// order, spelled the way this toolkit spells them. Mega and Micro are named
// apart deliberately: both have a claim on "Meta", so neither is given it.
func modString(m core.KeyModifiers) string {
	if m == 0 {
		return "-"
	}
	var parts []string
	for _, mod := range []struct {
		bit  core.KeyModifiers
		name string
	}{
		{core.ControlModifier, "Ctrl"},
		{core.GlyphModifier, "Glyph"},
		{core.MegaModifier, "Mega"},
		{core.MicroModifier, "Micro"},
		{core.ShiftModifier, "Shift"},
		{core.SuperModifier, "Super"},
		{core.HyperModifier, "Hyper"},
	} {
		if m&mod.bit != 0 {
			parts = append(parts, mod.name)
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("(%d)", int(m))
	}
	return strings.Join(parts, "+")
}

func buttonName(b core.MouseButton) string {
	switch b {
	case core.LeftButton:
		return "Left"
	case core.MiddleButton:
		return "Middle"
	case core.RightButton:
		return "Right"
	case core.NoButton:
		return ""
	}
	return fmt.Sprintf("button %d", int(b))
}

// quote shows an empty string as a visible marker rather than a blank cell:
// "the field was empty" and "the field was not filled in" look identical
// otherwise, and telling them apart is often the whole question.
func quote(s string) string {
	if s == "" {
		return "-"
	}
	return fmt.Sprintf("%q", s)
}

func abbreviate(s string) string {
	const limit = 24
	if len([]rune(s)) <= limit {
		return s
	}
	return string([]rune(s)[:limit]) + "…"
}
