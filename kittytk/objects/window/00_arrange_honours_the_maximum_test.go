package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Tiling hands each window a cell. A window that says how far it grows takes
// what it may of that cell and sits in the middle of it -- the same thing it
// does with a room too big for it when maximized. Filling the cell regardless
// is the whole of what a maximum is for, undone by an arrangement.
func TestTilingHoldsAWindowToItsMaximum(t *testing.T) {
	m := NewWindowManager()
	m.SetSmoothPositioning(true)
	m.SetScreenBounds(core.UnitRect{Width: 1200, Height: 800})

	capped := NewWindow("capped")
	capped.SetMaximumSize(core.UnitSize{Width: 200, Height: 120})
	m.AddWindow(capped)
	free := NewWindow("free")
	m.AddWindow(free)

	m.TileWindows()

	// Two windows tile into two cells side by side across the room.
	room := m.ClientArea()
	if got := free.Bounds(); got.Width != room.Width/2 {
		t.Errorf("the unbounded window is %d wide, want half the room (%d)",
			got.Width, room.Width/2)
	}
	got := capped.Bounds()
	if got.Width != 200 || got.Height != 120 {
		t.Errorf("the capped window tiled to %v, want its maximum of 200x120", got.Size())
	}
	// Centered in the cell it was given -- the left half, since it was added
	// first and the cells are handed out in order.
	cell := core.UnitRect{X: room.X, Y: room.Y, Width: room.Width / 2, Height: room.Height}
	if free.Bounds().X != cell.X+cell.Width {
		t.Fatalf("the other window is at x=%d, so the capped one did not take the left cell",
			free.Bounds().X)
	}
	if got.X != cell.X+(cell.Width-200)/2 || got.Y != cell.Y+(cell.Height-120)/2 {
		t.Errorf("the capped window sits at %d,%d, want it centered in its cell %v",
			got.X, got.Y, cell)
	}
}

// A minimum still beats a maximum here, and overflows the cell rather than
// letting the window be shrunk below what it says it needs.
func TestTilingHoldsAWindowToItsMinimum(t *testing.T) {
	m := NewWindowManager()
	m.SetSmoothPositioning(true)
	m.SetScreenBounds(core.UnitRect{Width: 400, Height: 400})

	win := NewWindow("floored")
	win.SetMaximumSize(core.UnitSize{Width: 50, Height: core.Unbounded})
	win.SetMinimumSize(core.UnitSize{Width: 300, Height: 0})
	m.AddWindow(win)
	m.AddWindow(NewWindow("other"))

	m.TileWindows()
	if got := win.Bounds().Width; got != 300 {
		t.Errorf("a minimum of 300 against a maximum of 50 tiled to %d, want the minimum", got)
	}
}

// Cascade gives every window the same standard size on a stepped corner. A
// capped window takes what it may of that size -- and stays on its step,
// because the stepped corner IS the arrangement.
func TestCascadeHoldsAWindowToItsMaximum(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})

	capped := NewWindow("capped")
	capped.SetMaximumSize(core.UnitSize{Width: 240, Height: core.Unbounded})
	m.AddWindow(capped)

	m.CascadeWindows()

	metrics := core.DefaultCellMetrics()
	room := m.ClientArea()
	got := capped.Bounds()
	if got.Width != 240 {
		t.Errorf("the capped window cascaded to %d wide, want its maximum of 240", got.Width)
	}
	if want := metrics.RoundDownToCellY(room.Height * 3 / 4); got.Height != want {
		t.Errorf("its height is %d, want the standard cascade height %d", got.Height, want)
	}
	if got.X != room.X || got.Y != room.Y {
		t.Errorf("the first cascaded window sits at %d,%d, want the room's corner %v",
			got.X, got.Y, room)
	}
}

// Tiling on a cell surface still lands every window on the cell grid, cap or
// no cap: a terminal can render a window nowhere else.
func TestTilingACappedWindowStaysOnTheCellGrid(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 803, Height: 617})

	for i := 0; i < 3; i++ {
		w := NewWindow("w")
		// A maximum that is not a whole number of cells, so centring it in
		// its cell lands the origin off the grid unless it is floored.
		w.SetMaximumSize(core.UnitSize{Width: 101, Height: 61})
		m.AddWindow(w)
	}
	m.TileWindows()

	met := core.DefaultCellMetrics()
	for i, w := range m.Windows() {
		b := w.Bounds()
		if b.X%met.UnitsPerCellWidth != 0 || b.Y%met.UnitsPerCellHeight != 0 {
			t.Errorf("window %d origin (%d,%d) is off the cell grid", i, b.X, b.Y)
		}
	}
}
