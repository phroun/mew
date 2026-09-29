package trinkets

// A list with checkboxes ticks rows independently of its current row: the bar
// is where the keyboard is, the ticks are what is chosen, and neither moves the
// other. Space and a press on a box tick; Return still activates; a right-click
// offers Select All, Select None and Invert Selection, none of which names a
// row. Without checkboxes the list is what it was, and Space and Select All's
// key go on to whatever else wants them.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
	"github.com/phroun/serval"
)

func ticking(n int) *ListView {
	l := filled(n)
	l.SetCheckboxes(true)
	l.extent(0, n)
	return l
}

func ticked(l *ListView) string { return fmt.Sprint(l.SelectedIndexes()) }

func TestTickingAndTheBarAreSeparate(t *testing.T) {
	l := filled(5)
	l.SetCurrentIndex(1)
	l.SetCheckboxes(true)
	l.extent(0, 5)
	if got := ticked(l); got != "[]" {
		t.Fatalf("turning checkboxes on ticked %s, want nothing", got)
	}
	l.SetSelected(3, true)
	if l.CurrentIndex() != 1 || ticked(l) != "[3]" {
		t.Errorf("ticking row 3: current %d, ticked %s; want current 1, ticked [3]", l.CurrentIndex(), ticked(l))
	}
	// Saying so again changes nothing.
	l.SetCheckboxes(true)
	if ticked(l) != "[3]" {
		t.Errorf("turning checkboxes on again left %s ticked, want [3]", ticked(l))
	}
	l.SetCurrentIndex(4)
	if ticked(l) != "[3]" {
		t.Errorf("moving the bar changed the ticks to %s", ticked(l))
	}
	if !l.barOn(4) || l.barOn(3) {
		t.Error("the bar is not on the current row alone")
	}
	// Turned off, the ticks go and the current row is the chosen one again.
	l.SetCheckboxes(false)
	if got := ticked(l); got != "[4]" {
		t.Errorf("turning checkboxes off leaves %s chosen, want the current row [4]", got)
	}
	if !l.barOn(4) || l.barOn(3) {
		t.Error("without checkboxes the bar is not on the chosen row")
	}
}

func key(l *ListView, k string) bool { return l.HandleKeyPress(core.KeyPressEvent{Key: k}) }

func TestSpaceTicksAndReturnActivates(t *testing.T) {
	l := ticking(4)
	l.SetCurrentIndex(2)
	activated := -1
	l.SetOnItemActivated(func(i int) { activated = i })

	key(l, "Space")
	if ticked(l) != "[2]" || activated != -1 {
		t.Fatalf("Space ticked %s and activated %d; want [2] and nothing", ticked(l), activated)
	}
	key(l, "Space")
	if ticked(l) != "[]" {
		t.Errorf("a second Space leaves %s ticked", ticked(l))
	}
	key(l, "Return")
	if activated != 2 || ticked(l) != "[]" {
		t.Errorf("Return activated %d and ticked %s; want row 2 and nothing", activated, ticked(l))
	}

	// Without checkboxes Space is the activation it always was.
	l.SetCheckboxes(false)
	activated = -1
	key(l, "Space")
	if activated != 2 {
		t.Errorf("without checkboxes Space activated %d, want 2", activated)
	}
}

func TestSelectAllsKeyIsTheListsOnlyWithBoxes(t *testing.T) {
	l := filled(3)
	if key(l, "M-a") {
		t.Error("a list without checkboxes swallowed Select All's key")
	}
	l.SetCheckboxes(true)
	if !key(l, "M-a") || !l.SelectsEverything() {
		t.Error("a list with checkboxes did not take Select All's key and tick everything")
	}
}

func TestTheBulkChoicesNameNoRow(t *testing.T) {
	l := ticking(5)
	var told []string
	l.SetOnCheck(func(c CheckChange) {
		if c.What == CheckRow {
			told = append(told, fmt.Sprintf("row %d %v %v", c.Row, c.ID, c.Checked))
		} else {
			told = append(told, fmt.Sprintf("%s %d named %v", c.What, c.Row, c.ID != nil))
		}
	})
	l.SetSelected(1, true)
	l.InvertSelection()
	if got := ticked(l); got != "[0 2 3 4]" {
		t.Errorf("inverting [1] ticks %s, want [0 2 3 4]", got)
	}
	if len(l.SelectedIDs()) != 1 {
		t.Errorf("inverting named %d rows, want only the one it named before", len(l.SelectedIDs()))
	}
	l.InvertSelection()
	if got := ticked(l); got != "[1]" {
		t.Errorf("inverting twice ticks %s, want [1] back", got)
	}
	l.SelectAll()
	l.ClearSelection()
	want := "row 1 1 true | invert -1 named false | invert -1 named false | all -1 named false | none -1 named false"
	if got := strings.Join(told, " | "); got != want {
		t.Errorf("told:\n  %s\nwant:\n  %s", got, want)
	}
	// Nothing is told without checkboxes, and nothing bulk is done.
	l.SetCheckboxes(false)
	told = nil
	l.SelectAll()
	l.InvertSelection()
	l.SetSelected(2, true)
	l.ClearSelection()
	if len(told) != 0 || l.SelectsEverything() {
		t.Errorf("without checkboxes: told %v, everything chosen %v", told, l.SelectsEverything())
	}
}

func TestAListThatChoosesNothingTicksNothing(t *testing.T) {
	l := ticking(3)
	l.SetSelectionMode(NoSelection)
	l.SelectAll()
	l.InvertSelection()
	l.SetSelected(1, true)
	if got := ticked(l); got != "[]" || l.SelectAllEnabled() {
		t.Errorf("a list that chooses nothing ticked %s (Select All enabled %v)", got, l.SelectAllEnabled())
	}
	l.SetCurrentIndex(1)
	if l.barOn(1) {
		t.Error("a list that chooses nothing draws its bar")
	}
}

// paintList draws a list into a cell grid, so what each cell holds can be read.
func paintList(t *testing.T, l *ListView, cols, rows int) *closeGrid {
	t.Helper()
	core.SetTextMeasurer(nil)
	px, err := raster.New(800, 400)
	if err != nil {
		t.Fatal(err)
	}
	m := l.EffectiveCellMetrics()
	l.SetBounds(core.UnitRect{Width: core.Unit(cols) * m.UnitsPerCellWidth, Height: core.Unit(rows) * m.UnitsPerCellHeight})
	g := &closeGrid{RenderBackend: px, cw: m.UnitsPerCellWidth, ch: m.UnitsPerCellHeight,
		focusFg: style.ColorRed, styles: map[[2]int]style.CellStyle{}}
	for i := 0; i < rows; i++ {
		g.rows = append(g.rows, []rune(strings.Repeat(" ", cols)))
		g.focus = append(g.focus, make([]bool, cols))
	}
	l.Paint(core.NewPainter(g))
	return g
}

func TestEachRowDrawsItsBox(t *testing.T) {
	l := ticking(3)
	l.SetSelected(1, true)
	g := paintList(t, l, 20, 3)
	for row, want := range []string{" [ ] item 0", " [x] item 1", " [ ] item 2"} {
		if got := string(g.rows[row][:len(want)]); got != want {
			t.Errorf("row %d reads %q, want %q", row, got, want)
		}
	}
	// Read the other way, the box stands at the right, just inside the
	// arrow's cell, with the gap after it on its left -- and its brackets keep
	// their order.
	l.SetDirection(core.DirRTL)
	g = paintList(t, l, 20, 3)
	if got := string(g.rows[1][15:20]); got != " [x] " {
		t.Errorf("right to left, row 1 ends %q, want %q", got, " [x] ")
	}
	// Without checkboxes, no box.
	l.SetDirection(core.DirLTR)
	l.SetCheckboxes(false)
	g = paintList(t, l, 20, 3)
	if got := string(g.rows[0][:7]); got != " item 0" {
		t.Errorf("without checkboxes row 0 reads %q", got)
	}
}

func TestAPressOnABoxTicksItsRow(t *testing.T) {
	l := ticking(4)
	paintList(t, l, 20, 4)
	m := l.EffectiveCellMetrics()
	row := func(i int) core.Unit { return core.Unit(i)*m.UnitsPerCellHeight + m.UnitsPerCellHeight/2 }
	press := func(x core.Unit, i int) {
		l.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: x, Y: row(i)})
		l.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton, X: x, Y: row(i)})
	}
	press(m.UnitsPerCellWidth, 0) // the box's very first unit
	if ticked(l) != "[0]" {
		t.Fatalf("a press on the first unit of row 0's box ticks %s, want [0]", ticked(l))
	}
	press(m.UnitsPerCellWidth, 0)
	press(2*m.UnitsPerCellWidth, 2) // the box's middle cell
	if l.CurrentIndex() != 2 || ticked(l) != "[2]" {
		t.Errorf("a press on row 2's box: current %d, ticked %s; want 2 and [2]", l.CurrentIndex(), ticked(l))
	}
	press(10*m.UnitsPerCellWidth, 3) // the row's text
	if l.CurrentIndex() != 3 || ticked(l) != "[2]" {
		t.Errorf("a press on row 3's text: current %d, ticked %s; want 3 and [2]", l.CurrentIndex(), ticked(l))
	}
	press(0, 1) // the arrow's cell, before the box
	if ticked(l) != "[2]" {
		t.Errorf("a press on the arrow's cell ticked %s", ticked(l))
	}
	press(4*m.UnitsPerCellWidth, 1) // the space after the box
	if ticked(l) != "[2]" {
		t.Errorf("a press just after the box ticked %s", ticked(l))
	}
	// Right to left the box is at the other end, three cells in from it.
	l.SetDirection(core.DirRTL)
	paintList(t, l, 20, 4)
	press(17*m.UnitsPerCellWidth, 0)
	if ticked(l) != "[0 2]" {
		t.Errorf("right to left, a press on row 0's box ticks %s, want [0 2]", ticked(l))
	}
	press(2*m.UnitsPerCellWidth, 1)
	if ticked(l) != "[0 2]" {
		t.Errorf("right to left, a press where the box stands left to right ticked %s", ticked(l))
	}
}

func TestARightClickOffersTheBulkChoices(t *testing.T) {
	l := ticking(4)
	paintList(t, l, 20, 4)
	pc := &popupsCaught{}
	l.SetPopupController(pc)
	m := l.EffectiveCellMetrics()
	right := core.MousePressEvent{Button: core.RightButton, X: 8 * m.UnitsPerCellWidth, Y: m.UnitsPerCellHeight / 2}
	l.HandleMousePress(right)
	if len(pc.registered) != 1 {
		t.Fatalf("a right-click opened %d menus, want 1", len(pc.registered))
	}
	var labels []string
	for _, it := range l.contextMenuItems() {
		labels = append(labels, it.label)
	}
	if got := strings.Join(labels, ", "); got != "Select All, Select None, Invert Selection" {
		t.Errorf("the menu offers %s", got)
	}
	// Choosing the last item inverts: its row is the popup's last.
	menu := pc.registered[0]
	b := menu.Bounds
	menu.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: b.X + 1, Y: b.Y + b.Height - m.UnitsPerCellHeight})
	if !l.SelectsEverything() {
		t.Error("choosing Invert Selection from the menu did not turn the boxes over")
	}
	// Without checkboxes a right-click opens nothing.
	l.SetCheckboxes(false)
	pc.registered = nil
	l.HandleMousePress(right)
	if len(pc.registered) != 0 {
		t.Error("a list without checkboxes opened a menu")
	}
}

// The Edit menu's Select All ticks every box of a focused list with them, and
// is greyed for one without.
func TestTheEditMenusSelectAllTicksEveryBox(t *testing.T) {
	l := filled(4)
	d, menu := editMenuOver(t, l)
	item := itemNamed(t, menu, core.CmdTrinketSelectAll)
	menu.OnAboutToShow()()
	if item.Enabled {
		t.Error("Select All is offered for a list without checkboxes")
	}
	l.SetCheckboxes(true)
	menu.OnAboutToShow()()
	if !item.Enabled {
		t.Fatal("Select All is greyed for a list with checkboxes")
	}
	if !d.PerformEdit(ItemIDSelectAll) || !l.SelectsEverything() {
		t.Error("the Edit menu's Select All did not tick every box")
	}
}

// Each item of the right-click menu does what it says, and Select All's shows
// the key that does the same.
func TestEachMenuItemDoesWhatItSays(t *testing.T) {
	l := ticking(4)
	items := withShortcuts(l, l.contextMenuItems())
	if items[0].shortcut == "" {
		t.Error("Select All shows no key")
	}
	l.SetSelected(1, true)
	items[1].action() // Select None
	if got := ticked(l); got != "[]" {
		t.Errorf("Select None leaves %s ticked", got)
	}
	items[0].action() // Select All
	if !l.SelectsEverything() {
		t.Error("Select All did not tick everything")
	}
	items[2].action() // Invert Selection
	if got := ticked(l); got != "[]" {
		t.Errorf("inverting everything leaves %s ticked", got)
	}
}

// The bar marks the current row, and a ticked row is drawn in the plain row's
// colours with its tick.
func TestTheBarIsTheCurrentRowNotTheTicked(t *testing.T) {
	l := ticking(3)
	l.SetCurrentIndex(0)
	l.SetSelected(2, true)
	g := paintList(t, l, 20, 3)
	bg := func(row int) style.Color { return g.styles[[2]int{10, row}].Bg }
	if bg(0) == bg(1) {
		t.Error("the current row is drawn like a plain one")
	}
	if bg(2) != bg(1) {
		t.Error("a ticked row is drawn with the bar")
	}
}

// A screen reader is told the list can have more than one row chosen exactly
// while it has boxes.
func TestAScreenReaderIsToldTheListTicks(t *testing.T) {
	l := filled(3)
	if l.AccessibleInfo().State&core.StateMultiSelectable != 0 {
		t.Error("a list without checkboxes says more than one row can be chosen")
	}
	l.SetCheckboxes(true)
	if l.AccessibleInfo().State&core.StateMultiSelectable == 0 {
		t.Error("a list with checkboxes does not say more than one row can be chosen")
	}
}

// A current row set by identity before its record arrives is not ticked when
// it does: with checkboxes the bar and the ticks stay apart even then.
func TestAPendingCurrentRowIsNotTickedWhenItArrives(t *testing.T) {
	rows := make([]serval.Row, 50)
	for i := range rows {
		rows[i] = serval.NewRow(serval.NewText(fmt.Sprintf("k%02d", i)),
			serval.Record{serval.Named(rowDisplay, fmt.Sprintf("row %d", i))})
	}
	l := NewListView()
	l.SetSource(serval.NewListSource(rows))
	l.SetCheckboxes(true)
	l.extent(0, 5)
	l.SetSelectedID(serval.NewText("k40"))
	l.extent(38, 5)
	if l.CurrentIndex() != 40 {
		t.Fatalf("setup: the pending row resolved to %d, want 40", l.CurrentIndex())
	}
	if l.IsSelected(40) {
		t.Error("the current row was ticked when its record arrived")
	}
}

// The Edit menu's other items are not Select All: Copy on a list with
// checkboxes does nothing and says it did nothing.
func TestOnlySelectAllReachesAList(t *testing.T) {
	l := ticking(3)
	d, _ := editMenuOver(t, l)
	if d.PerformEdit(ItemIDCopy) || l.SelectsEverything() {
		t.Error("Copy on a list with checkboxes did something")
	}
}
