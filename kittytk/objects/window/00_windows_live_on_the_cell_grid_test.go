package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// On a cell surface a window's origin floors onto the cell grid and its
// extent ceils onto it. Drawing rounds and hit-testing does not, so a window
// standing off the grid draws in one cell and answers the mouse in another --
// a title bar that looks dead to a click that lands on it.
//
// Origin and extent round in OPPOSITE directions on purpose. A position never
// ceils: rounding it up moves the window away from where it was put. An
// extent never floors: rounding it down clips the far edge off something that
// asked to be that big.
func TestAWindowOnACellSurfaceLandsOnTheGrid(t *testing.T) {
	m := core.DefaultCellMetrics()
	for _, asked := range []core.UnitRect{
		{X: 100, Y: 100, Width: 300, Height: 200},
		{X: 1, Y: 1, Width: 1, Height: 1},
		{X: 56, Y: 40, Width: 280, Height: 160},
		{X: 0, Y: 0, Width: 800, Height: 600},
	} {
		w := NewWindow("w")
		w.SetBounds(asked)
		got := w.Bounds()

		if got.X%m.UnitsPerCellWidth != 0 || got.Y%m.UnitsPerCellHeight != 0 {
			t.Errorf("asked for %v, got origin %d,%d off the grid", asked, got.X, got.Y)
		}
		if got.Width%m.UnitsPerCellWidth != 0 || got.Height%m.UnitsPerCellHeight != 0 {
			t.Errorf("asked for %v, got size %dx%d off the grid", asked, got.Width, got.Height)
		}
		if got.X > asked.X || got.Y > asked.Y {
			t.Errorf("asked for %v, origin %d,%d moved forward -- a position floors", asked, got.X, got.Y)
		}
		if got.Width < asked.Width || got.Height < asked.Height {
			t.Errorf("asked for %v, got %dx%d -- an extent ceils, it does not clip",
				asked, got.Width, got.Height)
		}
		// And never further than one cell from what was asked.
		if asked.X-got.X >= m.UnitsPerCellWidth || got.Width-asked.Width >= m.UnitsPerCellWidth {
			t.Errorf("asked for %v, got %v -- more than a cell away", asked, got)
		}
	}
}

// A smooth surface has no grid to stand off, and is left exactly as asked.
func TestAWindowOnASmoothSurfaceIsLeftAlone(t *testing.T) {
	asked := core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200}
	w := NewWindow("w")
	w.SetSmoothPositioning(true)
	w.SetBounds(asked)
	if got := w.Bounds(); got != asked {
		t.Errorf("a smooth surface gave %v, want the exact %v it was asked for", got, asked)
	}
}

// The granularity is stamped on a window when it joins a manager and again
// when it is torn onto an OS surface, which is after some windows have
// already been placed. The answer is re-derived from what was ASKED for, so a
// window placed before it knew where it was does not keep an answer rounded
// by the safe default.
func TestGeometryIsReDerivedWhenTheSurfaceChanges(t *testing.T) {
	asked := core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200}
	w := NewWindow("w")
	w.SetBounds(asked)
	if got := w.Bounds(); got == asked {
		t.Fatal("the default did not put the window on the grid; this test proves nothing")
	}
	w.SetSmoothPositioning(true)
	if got := w.Bounds(); got != asked {
		t.Errorf("after the surface said it was smooth the window is %v, want the %v "+
			"it asked for", got, asked)
	}
	w.SetSmoothPositioning(false)
	if got := w.Bounds(); got.X != 96 || got.Y != 96 || got.Width != 304 || got.Height != 208 {
		t.Errorf("back on a cell surface it is %v, want it re-gridded from the ask", got)
	}
}

// A room may only offer whole cells: half a row at the bottom is a row
// nothing can be drawn in. Without this a window fitted exactly to its room
// -- a maximized one -- is handed a size the grid cannot express, and ceils
// out past the room it was told to fill.
func TestARoomOffersWholeCellsOnly(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 803, Height: 617})
	room := m.ClientArea()

	metrics := core.DefaultCellMetrics()
	if room.Width%metrics.UnitsPerCellWidth != 0 || room.Height%metrics.UnitsPerCellHeight != 0 {
		t.Errorf("the room is %v, which is not whole cells", room)
	}

	win := NewWindow("w")
	m.AddWindow(win)
	m.MaximizeWindow(win)
	if got := win.Bounds(); got != room {
		t.Errorf("maximized it is %v, want exactly the room %v it was told to fill", got, room)
	}
}
