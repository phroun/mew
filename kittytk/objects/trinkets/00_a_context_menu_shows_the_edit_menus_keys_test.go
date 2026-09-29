package trinkets

// A text field's right-click menu leads with Undo and Redo, as the Edit menu
// does, and shows against every item the key the Edit menu shows for the same
// act. PurfecTerm's popup -- which mew's editor opens too -- draws its items
// the same way and refuses a disabled one.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

func TestAFieldsContextMenuLeadsWithUndoAndRedo(t *testing.T) {
	ti, _ := newClippedInput("")
	var got []string
	for _, it := range ti.contextMenuItems() {
		if it.separator {
			got = append(got, "---")
		} else {
			got = append(got, it.label)
		}
	}
	want := "Undo Redo --- Cut Copy Paste --- Select All"
	if strings.Join(got, " ") != want {
		t.Errorf("the menu is %v, want %s", got, want)
	}
	enabled := func(label string) bool {
		for _, it := range ti.contextMenuItems() {
			if it.label == label {
				return !it.disabled
			}
		}
		t.Fatalf("no %s item", label)
		return false
	}
	if enabled("Undo") || enabled("Redo") {
		t.Error("Undo or Redo is offered with nothing typed")
	}
	typeWord(ti, "ab")
	if !enabled("Undo") || enabled("Redo") {
		t.Error("after typing, Undo should be offered and Redo not")
	}
	ti.Undo()
	if !enabled("Redo") {
		t.Error("after undoing, Redo is not offered")
	}
}

// Each item shows exactly the key the Edit menu shows for its command --
// including Undo, which shows simple undo's key where the keymap ranks it
// higher.
func TestAContextMenuShowsTheEditMenusKeys(t *testing.T) {
	ti := NewTextInput()
	d, menu := editMenuOver(t, ti)
	menu.SetKeyResolver(d.keyForCommandInFocus)
	for _, it := range withShortcuts(ti.Self(), ti.contextMenuItems()) {
		if it.separator {
			continue
		}
		want := itemNamed(t, menu, it.command).ShortcutDisplay()
		if want == "" {
			t.Errorf("the Edit menu shows no key for %s", it.label)
		}
		if it.shortcut != want {
			t.Errorf("%s shows %q in the context menu and %q in the Edit menu", it.label, it.shortcut, want)
		}
	}
}

// On a cell surface the key is drawn dimmed, ending the indent in from the
// right edge, and the menu is wide enough for label and key together.
func TestAContextMenuDrawsTheKeyAtTheRight(t *testing.T) {
	m := core.DefaultCellMetrics()
	items := []termMenuItem{
		{label: "Copy", shortcut: "^C"},
		{label: "Select All", shortcut: "^A", disabled: true},
	}
	lay := termMenuLayoutFrom(false, nil, m, items)
	if cols := int(lay.width / m.UnitsPerCellWidth); cols < len("Select All")+6+termMenuShortcutGap+2 {
		t.Errorf("the menu is %d columns, too narrow for its label and key", cols)
	}
	px, err := raster.New(400, 64)
	if err != nil {
		t.Fatal(err)
	}
	g := &closeGrid{RenderBackend: px, cw: m.UnitsPerCellWidth, ch: m.UnitsPerCellHeight, styles: map[[2]int]style.CellStyle{}}
	cols := int(lay.width / m.UnitsPerCellWidth)
	for i := 0; i < 2; i++ {
		g.rows = append(g.rows, []rune(strings.Repeat(" ", cols)))
		g.focus = append(g.focus, make([]bool, cols))
	}
	bounds := core.UnitRect{Width: lay.width, Height: 2 * m.UnitsPerCellHeight}
	bg := style.DefaultStyle().WithFg(style.ColorBlack).WithBg(style.ColorWhite)
	hover := style.DefaultStyle().WithFg(style.ColorWhite).WithBg(style.ColorBlue)
	p := core.NewPainter(g)
	paintTermMenuItem(p, bounds, 0, lay, items[0], true, bg, hover)
	paintTermMenuItem(p, bounds, m.UnitsPerCellHeight, lay, items[1], true, bg, hover)

	row := string(g.rows[0])
	if !strings.HasPrefix(strings.TrimLeft(row, " "), "Copy") || !strings.HasSuffix(strings.TrimRight(row, " "), "^C") {
		t.Errorf("row 0 reads %q, want Copy at the left and ^C at the right", row)
	}
	keyEnd := strings.LastIndex(row, "^C") + 2
	if keyEnd != cols-1 {
		t.Errorf("the key ends at column %d, want %d (one cell in from the edge)", keyEnd, cols-1)
	}
	if s := g.styles[[2]int{keyEnd - 1, 0}]; s.Attrs&style.StyleDim == 0 || s.Bg != hover.Bg {
		t.Errorf("the key is drawn %+v, want dimmed on the highlight", s)
	}
	// A disabled item is never highlighted, and its key is not dimmed twice.
	if s := g.styles[[2]int{1, 1}]; s.Bg == hover.Bg {
		t.Error("a disabled item was drawn highlighted")
	}
}

// popupsCaught keeps the popups a trinket registers, so a test can press in one.
type popupsCaught struct{ registered []*core.PopupRequest }

func (c *popupsCaught) RegisterPopup(r *core.PopupRequest) { c.registered = append(c.registered, r) }
func (c *popupsCaught) UnregisterPopup(string)             {}
func (c *popupsCaught) MapToScreen(_ core.Trinket, p core.UnitPoint) core.UnitPoint {
	return p
}
func (c *popupsCaught) ScreenBounds() core.UnitRect {
	return core.UnitRect{Width: 10000, Height: 10000}
}

// PurfecTerm's popup, which mew's editor opens as well, refuses a disabled
// item and fires an enabled one.
func TestTheTerminalsPopupRefusesADisabledItem(t *testing.T) {
	term := NewPurfecTerm()
	pc := &popupsCaught{}
	term.SetPopupController(pc)
	fired := ""
	term.showTermItemsMenu(core.UnitPoint{}, []termMenuItem{
		{label: "Off", disabled: true, action: func() { fired = "off" }},
		{label: "On", action: func() { fired = "on" }},
	})
	if len(pc.registered) != 1 {
		t.Fatalf("registered %d popups", len(pc.registered))
	}
	req := pc.registered[0]
	lay := termMenuLayoutFrom(core.FindGraphicalFrames(term), term.EffectiveFont(),
		termMenuScreenMetrics(pc), nil)
	rowY := func(i int) core.Unit { return req.Bounds.Y + lay.padTop + core.Unit(i)*lay.rowH + lay.rowH/2 }

	req.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: req.Bounds.X + 2, Y: rowY(0)})
	if fired != "" {
		t.Errorf("a disabled item fired %q", fired)
	}
	term.showTermItemsMenu(core.UnitPoint{}, []termMenuItem{
		{label: "Off", disabled: true, action: func() { fired = "off" }},
		{label: "On", action: func() { fired = "on" }},
	})
	req = pc.registered[len(pc.registered)-1]
	req.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: req.Bounds.X + 2, Y: rowY(1)})
	if fired != "on" {
		t.Errorf("the enabled item fired %q, want on", fired)
	}
}

// paintPopup draws a registered popup into a cell grid, for reading back.
func paintPopup(t *testing.T, req *core.PopupRequest) string {
	t.Helper()
	px, err := raster.New(800, 400)
	if err != nil {
		t.Fatal(err)
	}
	m := core.DefaultCellMetrics()
	g := &closeGrid{RenderBackend: px, cw: m.UnitsPerCellWidth, ch: m.UnitsPerCellHeight,
		styles: map[[2]int]style.CellStyle{}}
	for i := 0; i < 20; i++ {
		g.rows = append(g.rows, []rune(strings.Repeat(" ", 80)))
		g.focus = append(g.focus, make([]bool, 80))
	}
	req.Paint(core.NewPainter(g))
	var out []string
	for y := range g.rows {
		out = append(out, string(g.rows[y]))
	}
	return strings.Join(out, "\n")
}

// The popups a field and a terminal really open show the keys.
func TestTheOpenedPopupsShowTheKeys(t *testing.T) {
	copyKey := core.DisplayKey(core.DefaultKeyRegistry().KeyForCommand(core.CmdTrinketCopy))

	ti := NewTextInput()
	pc := &popupsCaught{}
	ti.SetPopupController(pc)
	ti.showContextMenu(core.MousePressEvent{Button: core.RightButton})
	if len(pc.registered) != 1 {
		t.Fatalf("the field registered %d popups", len(pc.registered))
	}
	if drawn := paintPopup(t, pc.registered[0]); !strings.Contains(drawn, copyKey) {
		t.Errorf("the field's menu does not show %q:\n%s", copyKey, drawn)
	}

	// A terminal offers no copy key of its own, and so shows none, as its Edit
	// menu does; given one, its popup shows it.
	term := NewPurfecTerm()
	term.SetCommands(core.CmdTrinketCopy)
	tpc := &popupsCaught{}
	term.SetPopupController(tpc)
	term.showTermItemsMenu(core.UnitPoint{}, []termMenuItem{
		{label: "Copy", action: func() {}, command: core.CmdTrinketCopy},
	})
	want := core.FindKeyForCommand(term, core.CmdTrinketCopy)
	if want == "" {
		t.Fatal("the terminal was given a copy command and binds no key for it")
	}
	if drawn := paintPopup(t, tpc.registered[0]); !strings.Contains(drawn, core.DisplayKey(want)) {
		t.Errorf("the terminal's menu does not show %q:\n%s", core.DisplayKey(want), drawn)
	}
}
