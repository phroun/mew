package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A cell surface renders a frame nowhere but the cell grid, and a capped
// maximized window's frame is CENTRED in the room it holds -- an arithmetic
// that lands between rows as easily as on one.
//
// Drawing rounds and hit-testing does not, so a frame centred at unit 204
// draws at row 12 (unit 192) and answers the mouse from 204: a click on the
// title bar the person can see lands above the title bar the window believes
// in, and the window looks dead.
func TestACappedMaximizedFrameLandsOnTheCellGrid(t *testing.T) {
	m := core.DefaultCellMetrics()
	// Heights and widths chosen so the centring offset is off-grid: a room
	// 608 tall around a 200-tall frame centres at 204, which is 12.75 rows.
	for _, room := range []core.UnitSize{
		{Width: 800, Height: 608},
		{Width: 804, Height: 600},
		{Width: 999, Height: 641},
	} {
		win := NewWindow("bounded")
		win.SetMaximumSize(core.UnitSize{Width: 320, Height: 200})
		fr := MaximizedFrameRect(win, room)
		if fr.X%m.UnitsPerCellWidth != 0 {
			t.Errorf("in a %v room the frame starts at x=%d, %d units into a cell",
				room, fr.X, fr.X%m.UnitsPerCellWidth)
		}
		if fr.Y%m.UnitsPerCellHeight != 0 {
			t.Errorf("in a %v room the frame starts at y=%d, %d units into a row",
				room, fr.Y, fr.Y%m.UnitsPerCellHeight)
		}
		// Floored, never ceiled: the frame must not be pushed toward the far
		// edge of the room it is centred in.
		if want := m.RoundDownToCellY((room.Height - 200) / 2); fr.Y != want {
			t.Errorf("in a %v room the frame is at y=%d, want %d -- the centre floored",
				room, fr.Y, want)
		}
	}
}

// The title bar of such a frame answers a press on the row it is drawn in.
func TestACappedMaximizedWindowsTitleBarAnswersWhereItIsDrawn(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 800, Height: 608})
	win := NewWindow("bounded")
	win.SetMaximumSize(core.UnitSize{Width: 320, Height: 200})
	m.AddWindow(win)
	m.MaximizeWindow(win)

	metrics := core.DefaultCellMetrics()
	b := win.Bounds()
	fr := win.FrameRect()

	// The middle of the row the frame's top is drawn in.
	press := core.MousePressEvent{
		X:      b.X + fr.X + fr.Width/2,
		Y:      metrics.RoundDownToCellY(b.Y+fr.Y) + metrics.UnitsPerCellHeight/2,
		Button: core.LeftButton,
	}
	m.HandleMousePress(press)
	m.mu.RLock()
	dragging := m.dragging == win
	m.mu.RUnlock()
	if !dragging {
		t.Errorf("a press at %d,%d -- the row the title bar is drawn in -- did not "+
			"take hold of the title bar", press.X, press.Y)
	}
}
