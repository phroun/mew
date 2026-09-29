package trinkets

// Checkboxes: more than one row chosen at a time.
//
// A list with checkboxes draws a box at the front of every row, and the rows
// whose boxes are ticked are what is chosen (listchoice.go). The CURRENT row --
// the bar, what the arrows move and Return activates -- is a separate thing,
// and ticking a box does not move it any more than moving it ticks a box.
//
// Space ticks the current row's box, a press on a box ticks that row's, a
// double-click anywhere else on a row ticks it too, and a right-click offers
// Select All, Select None and Invert Selection. Return still activates. None of the
// three names a row: all of them are the "everything except" shape turned one
// way or the other, so they cost the same on a million rows as on three.

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/serval"
)

// CheckWhat says which kind of change a CheckChange reports.
type CheckWhat string

const (
	// CheckRow is one row's box ticked or unticked.
	CheckRow CheckWhat = "row"
	// CheckAll is every box ticked.
	CheckAll CheckWhat = "all"
	// CheckNone is every box unticked.
	CheckNone CheckWhat = "none"
	// CheckInvert is every box turned over.
	CheckInvert CheckWhat = "invert"
)

// CheckChange is one change to which boxes are ticked. Row, ID and Checked
// describe the row for CheckRow; the others are about every row, and Row is
// -1. A listener that applies each change in turn to its own record of the
// ticks keeps the same record the list does, without ever being handed a list
// of rows.
type CheckChange struct {
	What    CheckWhat
	Row     int
	ID      *serval.Value
	Checked bool
}

// SetCheckboxes draws a box at the front of every row, or takes the boxes
// away. Turned on, nothing is ticked; turned off, the ticks are forgotten and
// the current row is the chosen one again.
func (l *ListView) SetCheckboxes(on bool) {
	if l.checkboxes == on {
		return
	}
	l.checkboxes = on
	l.chosen.clear()
	if !on && l.mirrorsCurrent() && l.currentIndex >= 0 {
		if id, ok := l.bones.idAt(l.currentIndex); ok {
			l.chosen.only(id)
		}
	}
	l.offerCommands()
	l.Update()
	if l.onSelectionChanged != nil {
		l.onSelectionChanged()
	}
}

// Checkboxes reports whether the rows carry boxes.
func (l *ListView) Checkboxes() bool { return l.checkboxes }

// SetOnCheck sets what is told of each change to the ticked boxes.
func (l *ListView) SetOnCheck(fn func(CheckChange)) { l.onCheck = fn }

func (l *ListView) tellCheck(c CheckChange) {
	if l.onCheck != nil {
		l.onCheck(c)
	}
}

// mirrorsCurrent reports whether the chosen row simply follows the current
// one, which is what a list without checkboxes does.
func (l *ListView) mirrorsCurrent() bool {
	return l.selectionMode == SingleSelection && !l.checkboxes
}

// offerCommands says which commands the list answers to. Ticking a box and
// selecting every row are offered only while there are boxes, so without them
// the space bar still activates and the Select All key goes on to whatever
// else wants it rather than being swallowed here for nothing.
func (l *ListView) offerCommands() {
	cmds := []string{
		core.CmdTrinketItemPrior, core.CmdTrinketItemUp,
		core.CmdTrinketItemNext, core.CmdTrinketItemDown,
		core.CmdTrinketScrollUp, core.CmdTrinketScrollDown,
		core.CmdTrinketPagePrior, core.CmdTrinketPageNext,
		core.CmdTrinketBeg, core.CmdTrinketEnd,
	}
	if l.checkboxes {
		cmds = append(cmds, core.CmdTrinketCheck, core.CmdTrinketSelectAll)
	}
	l.SetCommands(append(cmds, core.CmdTrinketActivate)...)
}

// checkboxWidth is how much of a row its box takes: the three cells of
// "[x]" and a cell of space before what follows. None without checkboxes.
func (l *ListView) checkboxWidth() core.Unit {
	if !l.checkboxes {
		return 0
	}
	return 4 * l.EffectiveCellMetrics().UnitsPerCellWidth
}

// onCheckbox reports whether a local x is on a row's box, which stands after
// the current-row arrow's cell at the list's leading edge.
func (l *ListView) onCheckbox(x core.Unit) bool {
	if !l.checkboxes {
		return false
	}
	cw := l.EffectiveCellMetrics().UnitsPerCellWidth
	boxX := core.LeadingX(l, l.Bounds().Width, cw, 3*cw)
	return x >= boxX && x < boxX+3*cw
}

// contextMenuItems is what a right-click offers a list with checkboxes.
func (l *ListView) contextMenuItems() []termMenuItem {
	return []termMenuItem{
		{label: "Select All", command: core.CmdTrinketSelectAll, action: l.SelectAll},
		{label: "Select None", action: l.ClearSelection},
		{label: "Invert Selection", action: l.InvertSelection},
	}
}

// contextMenuID names this list's popup uniquely.
func (l *ListView) contextMenuID() string {
	return fmt.Sprintf("listview-menu-%d", l.ObjectID())
}

// showContextMenu opens the right-click menu at a local point.
func (l *ListView) showContextMenu(at core.UnitPoint) {
	var pc core.PopupController
	for t := core.Trinket(l.Self()); t != nil && pc == nil; t = t.Parent() {
		if getter, ok := t.(interface{ PopupController() core.PopupController }); ok {
			pc = getter.PopupController()
		}
	}
	if pc == nil {
		return
	}
	openTermMenu(l.Self(), l.EffectiveFont(), pc, l.contextMenuID(), l.Self(), at, l.contextMenuItems())
}

// barOn reports whether a row is drawn with the selection bar: the chosen row
// without checkboxes, and the current row with them, since the ticks then say
// what is chosen and the bar only where the keyboard is.
func (l *ListView) barOn(index int) bool {
	if l.checkboxes {
		return index == l.currentIndex && l.selectionMode != NoSelection
	}
	return l.IsSelected(index)
}

// SelectAllEnabled reports whether SelectAll would do anything: the Edit
// menu's Select All is offered for a list with checkboxes that chooses rows.
func (l *ListView) SelectAllEnabled() bool {
	return l.checkboxes && l.selectionMode != NoSelection
}
