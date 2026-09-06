package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// A popup stands on the cell grid, wherever the thing that opened it stands.
//
// Drawing divides units by the cell size and hit-testing does not, so a menu
// whose rect is a fraction of a cell out paints its rows in one cell and
// answers the pointer in the next: the item under the pointer is the one above
// the one that lights up. Mapping a trinket's own coordinates to the screen
// crosses whatever denominations lie between, so the rect that comes back is
// not on the grid by itself.
func TestAContextMenuStandsOnTheCellGrid(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	d.windowManager.SetScreenBounds(core.UnitRect{Width: 800, Height: 592})
	d.setupTearOff(nil, nil)

	win := window.NewWindow("w")
	win.SetBounds(core.UnitRect{X: 80, Y: 96, Width: 400, Height: 300})
	d.windowManager.AddWindow(win)

	input := NewTextInput()
	win.AddChild(input)
	// Half a cell across and half a row down inside its window, which is what
	// a mapping through a denominated container can land on.
	input.SetBounds(core.UnitRect{X: 4, Y: 8, Width: 160, Height: 16})
	input.SetText("hello there")

	b, client := win.Bounds(), win.ClientArea()
	at := core.UnitPoint{X: b.X + client.X + 24, Y: b.Y + client.Y + 8}
	d.dispatchEvent(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.RightButton})

	m := core.FindEffectiveCellMetrics(input.Self())
	found := false
	for _, p := range d.windowManager.GetPopups() {
		overlay, ok := p.(*window.PopupOverlay)
		if !ok {
			continue
		}
		found = true
		r := overlay.Bounds
		if r.X%m.UnitsPerCellWidth != 0 || r.Y%m.UnitsPerCellHeight != 0 {
			t.Errorf("the menu opened at %d,%d, %d columns and %d rows into a cell",
				r.X, r.Y, r.X%m.UnitsPerCellWidth, r.Y%m.UnitsPerCellHeight)
		}
		if r.Width%m.UnitsPerCellWidth != 0 || r.Height%m.UnitsPerCellHeight != 0 {
			t.Errorf("the menu is %dx%d, not a whole number of cells", r.Width, r.Height)
		}
	}
	if !found {
		t.Fatal("the right-click opened no menu")
	}
}

// And a combo box's drop-down does the same.
func TestADropDownStandsOnTheCellGrid(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	d.windowManager.SetScreenBounds(core.UnitRect{Width: 800, Height: 592})
	d.setupTearOff(nil, nil)

	win := window.NewWindow("w")
	win.SetBounds(core.UnitRect{X: 80, Y: 96, Width: 400, Height: 300})
	d.windowManager.AddWindow(win)

	cb := NewComboBox()
	for _, item := range []string{"one", "two", "three", "four"} {
		cb.AddItem(item)
	}
	win.AddChild(cb)
	cb.SetBounds(core.UnitRect{X: 4, Y: 8, Width: 158, Height: 16})

	b, client := win.Bounds(), win.ClientArea()
	at := core.UnitPoint{X: b.X + client.X + 8, Y: b.Y + client.Y + 8}
	d.dispatchEvent(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	if !cb.IsOpen() {
		t.Fatal("the press did not open the drop-down")
	}

	m := core.FindEffectiveCellMetrics(cb.Self())
	for _, p := range d.windowManager.GetPopups() {
		overlay, ok := p.(*window.PopupOverlay)
		if !ok {
			continue
		}
		r := overlay.Bounds
		if r.X%m.UnitsPerCellWidth != 0 || r.Y%m.UnitsPerCellHeight != 0 {
			t.Errorf("the list opened at %d,%d, %d columns and %d rows into a cell",
				r.X, r.Y, r.X%m.UnitsPerCellWidth, r.Y%m.UnitsPerCellHeight)
		}
		if r.Width%m.UnitsPerCellWidth != 0 || r.Height%m.UnitsPerCellHeight != 0 {
			t.Errorf("the list is %dx%d, not a whole number of cells", r.Width, r.Height)
		}
	}
}
