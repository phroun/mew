package trinkets

// The Event Viewer: a desktop accessory that shows, row by row, every event
// the desktop receives.
//
// It reads the near end of a keystroke's trip. An event filter on the desktop
// runs before any trinket has had a say, so what is logged is what the rest of
// the toolkit is about to be given rather than what some trinket made of it.
// A client on the wire cannot see this -- by then the event has already been
// routed and translated -- which is why this is a host facility.
//
// It is desktop-wide, so it observes events bound for any application's
// windows, not only its own. That is the case worth having: open it while the
// program you are debugging is running.

import (
	"fmt"
	"strings"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/objects/window"
)

// eventViewerMaxRows bounds the log. A held key fills a screen in a second and
// a mouse crossing the window fills hundreds of rows, so the oldest are
// dropped rather than letting the flat list grow without limit.
const eventViewerMaxRows = 2000

// eventViewer holds the trinkets the event filter writes into, and the window
// they live in so triggering the menu item again raises that window rather
// than opening a second one.
type eventViewer struct {
	win       *window.Window
	tree      *TreeView
	status    *Label
	modeLabel *Label
	modes     core.ModeSource
	showMouse bool
	seq       int
}

// build assembles the window content: the log, a filter row, and a hint line.
func (v *eventViewer) build() core.Trinket {
	v.tree = NewTreeView()
	v.tree.SetShowHeader(true)
	v.tree.SetLedger(true) // alternating row tint: long runs stay readable

	// The sequence is a DATA column, and the key (tree) column is hidden.
	//
	// It began as the key column, which is the natural home for a row's
	// label - but the key column is not a TreeColumn, so it has nowhere to
	// carry Numeric and sorts as text: 1, 10, 11, 2. It cannot carry a
	// SortProxy either, for the same reason. As an ordinary column it just
	// declares Numeric and sorts correctly, with no toolkit change at all.
	//
	// The log is flat, so nothing is lost by hiding the tree column: there
	// is no hierarchy for it to express.
	v.tree.SetShowKey(false)

	// Natural widths and horizontal panning rather than fit mode. The columns
	// want more room than the window has, and squeezing them to the width is
	// the wrong trade here: a cell that has been narrowed to nothing renders
	// as an ellipsis, and "the field held something I cannot read" is the one
	// answer this viewer must never give.
	v.tree.SetFitWidth(false)

	// Optional puts a column in the [=] chooser. All of them, because which
	// ones are noise depends entirely on what is being chased -- Modifiers and
	// Repeat are the whole question for a keyboard problem and pure clutter
	// for a mouse one. Hiding every one of them does not leave a blank tree:
	// with no visible data column the key column comes back, hidden or not.
	for _, c := range []*TreeColumn{
		{ID: "seq", Caption: "#", Width: 7, Align: "right", Optional: true,
			Sortable: true, Numeric: true},
		{ID: "event", Caption: "Event", Width: 14, Resizable: true, Optional: true},
		{ID: "key", Caption: "Key", Width: 16, Resizable: true, Optional: true},
		{ID: "mods", Caption: "Modifiers", Width: 22, Resizable: true, Optional: true},
		{ID: "repeat", Caption: "Repeat", Width: 7, Align: "center", Optional: true},
		{ID: "text", Caption: "Text", Width: 8, Resizable: true, Optional: true},
		{ID: "detail", Caption: "Detail", Width: 40, Resizable: true, Optional: true},
	} {
		v.tree.AddColumn(c)
	}

	// Mouse motion alone produces an event per pixel of travel, which buries
	// the keystroke that was being looked for. Off by default for that reason;
	// the button and wheel events are grouped with it because they are noisy
	// together and the question is nearly always about the keyboard.
	mouse := NewCheckbox("Show mouse events")
	mouse.SetOnToggled(func(checked bool) { v.showMouse = checked })

	clear := NewButton("Clear")
	clear.SetOnClick(func() {
		v.tree.Clear()
		v.seq = 0
		v.setStatus("cleared")
	})

	controls := NewPanel()
	controlsLayout := layout.NewBoxLayout(core.Horizontal)
	// No explicit spacing, and the gaps are not missing: a checkbox, a button
	// and a label are all core.InlineTrinket - text-style controls - and a box
	// layout already parts those by one cell so they do not butt together.
	//
	// Asking for spacing on top of that was worse than redundant. It is
	// expressed in UNITS, and anything under a cell rounds away to nothing in
	// Layout while SizeHint still counts it in full, so the panel asked for a
	// width the layout never used - and not a whole number of cells either.
	controls.SetLayoutManager(controlsLayout)
	controls.AddChild(mouse)
	controls.AddChild(clear)
	v.status = NewLabel("waiting for events")
	controls.AddChild(v.status)

	// The keyboard's standing states, which are not events and so appear in no
	// row: the pad's lock, Caps Lock, and whether this window has the keyboard.
	// A state the host cannot see is left out entirely rather than shown as
	// off — the two are different facts, and which ones a host can see is one
	// of the things worth comparing between the two builds.
	v.modeLabel = NewLabel("")
	controls.AddChild(v.modeLabel)
	v.refreshModes()

	rootPanel := NewPanel()
	rootLayout := layout.NewBoxLayout(core.Vertical)
	rootPanel.SetLayoutManager(rootLayout)
	rootPanel.AddChild(v.tree)
	rootPanel.AddChild(controls)
	// The tree takes the slack; the control row keeps its natural height.
	rootLayout.ItemAt(0).WithStretch(1).WithAlign(core.AlignFill)

	return rootPanel
}

func (v *eventViewer) setStatus(s string) {
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
func (v *eventViewer) refreshModes() {
	if v.modeLabel == nil {
		return
	}
	if v.modes == nil {
		v.modeLabel.SetText("modes: not reported by this host")
		return
	}
	var parts []string
	for _, m := range v.modes.Modes() {
		parts = append(parts, capitalizeMode(m.Name)+":"+m.Value)
	}
	if len(parts) == 0 {
		v.modeLabel.SetText("modes: none known yet")
		return
	}
	v.modeLabel.SetText(strings.Join(parts, "  "))
}

// capitalize titles a mode's token for display. The tokens are lowercase
// because they are matched, not read; a status bar is read.
func capitalizeMode(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// log appends one row for an event, or drops it when the mouse filter is off.
func (v *eventViewer) log(ev core.Event) {
	// Before the filter below: a mode can move on an event whose row is
	// hidden, and the line should still be right.
	v.refreshModes()

	name, key, mods, repeat, text, detail, isMouse := describeEvent(ev)
	if isMouse && !v.showMouse {
		return
	}
	if name == "" {
		return // an event kind with nothing worth a row
	}

	v.seq++
	// The number goes in the seq COLUMN, where it can sort numerically. It
	// stays on the item's text too: the key column is hidden, but Text is
	// what an item announces itself as to accessibility.
	seq := fmt.Sprintf("%d", v.seq)
	item := NewTreeItem(seq)
	item.SetValue("seq", seq)
	item.SetValue("event", name)
	item.SetValue("key", key)
	item.SetValue("mods", mods)
	item.SetValue("repeat", repeat)
	item.SetValue("text", text)
	item.SetValue("detail", detail)
	v.tree.AddRootItem(item)

	if roots := v.tree.RootItems(); len(roots) > eventViewerMaxRows {
		v.tree.RemoveRootItem(roots[0])
	}
	// Follow the tail, the way a log window does.
	v.tree.SetCurrentIndex(len(v.tree.RootItems()) - 1)
	v.setStatus(fmt.Sprintf("%d events", v.seq))
}

// describe renders one event as the row's cells. isMouse marks the ones the
// filter checkbox hides.
func describeEvent(ev core.Event) (name, key, mods, repeat, text, detail string, isMouse bool) {
	switch e := ev.(type) {
	case core.KeyPressEvent:
		repeat = "-"
		if e.Repeat {
			repeat = "yes"
		}
		return "KeyPress", quoteEventText(e.Key), eventModString(e.Modifiers), repeat, quoteEventText(e.Text), "", false

	case core.KeyReleaseEvent:
		// No Text and no Repeat on a release: the event carries neither, which
		// is itself worth seeing next to the press that preceded it.
		return "KeyRelease", quoteEventText(e.Key), eventModString(e.Modifiers), "-", "-", "", false

	case core.PasteEvent:
		return "Paste", "", "", "-", quoteEventText(abbreviateEventText(e.Text)), fmt.Sprintf("%d bytes", len(e.Text)), false

	case core.MousePressEvent:
		return "MousePress", eventButtonName(e.Button), eventModString(e.Modifiers), "-", "",
			fmt.Sprintf("at %d,%d", e.X, e.Y), true

	case core.MouseReleaseEvent:
		return "MouseRelease", eventButtonName(e.Button), eventModString(e.Modifiers), "-", "",
			fmt.Sprintf("at %d,%d", e.X, e.Y), true

	case core.MouseMoveEvent:
		held := ""
		if e.Buttons != core.NoButton {
			held = " holding " + eventButtonName(e.Buttons)
		}
		return "MouseMove", "", eventModString(e.Modifiers), "-", "",
			fmt.Sprintf("at %d,%d%s", e.X, e.Y, held), true

	case core.MouseWheelEvent:
		d := fmt.Sprintf("at %d,%d delta %d,%d", e.X, e.Y, e.DeltaX, e.DeltaY)
		if e.PreciseX != 0 || e.PreciseY != 0 {
			d += fmt.Sprintf(" precise %.2f,%.2f", e.PreciseX, e.PreciseY)
		}
		return "MouseWheel", "", eventModString(e.Modifiers), "-", "", d, true

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
func eventModString(m core.KeyModifiers) string {
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

func eventButtonName(b core.MouseButton) string {
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
func quoteEventText(s string) string {
	if s == "" {
		return "-"
	}
	return fmt.Sprintf("%q", s)
}

func abbreviateEventText(s string) string {
	const limit = 24
	if len([]rune(s)) <= limit {
		return s
	}
	return string([]rune(s)[:limit]) + "…"
}
