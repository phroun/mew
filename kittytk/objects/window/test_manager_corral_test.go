package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A window left off-screen by a desktop shrink is corralled into view
// for display only: its logical bounds are untouched, so growing the
// desktop back re-spreads it to where it was.
func TestProvisionalCorralRespread(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 608})
	win := NewWindow("corral")
	win.SetBounds(core.UnitRect{X: 600, Y: 100, Width: 160, Height: 120})
	m.AddWindow(win)

	// Fits comfortably: display == logical.
	if got := m.displayBounds(win); got.X != 600 {
		t.Fatalf("fits: display X = %d, want 600", got.X)
	}

	// Shrink so the window overhangs the right edge.
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 300, Height: 608})
	disp := m.displayBounds(win)
	if disp.X >= 600 {
		t.Errorf("shrunk: window not corralled (display X=%d)", disp.X)
	}
	metrics := m.ScreenCellMetrics()
	if maxX := 300 - metrics.CellWidth*minVisibleColumns; disp.X > maxX {
		t.Errorf("shrunk: display X %d leaves < %d columns visible", disp.X, minVisibleColumns)
	}
	// Provisional: logical bounds must NOT have moved.
	if win.Bounds().X != 600 {
		t.Errorf("shrunk: logical X mutated to %d, want 600 (provisional)", win.Bounds().X)
	}

	// Grow back: the window re-spreads to its original position.
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 608})
	if got := m.displayBounds(win); got.X != 600 {
		t.Errorf("regrown: display X = %d, want re-spread to 600", got.X)
	}
}

// A deliberate interaction (mouse press landing on the window) commits
// the provisional corral: the displayed position becomes permanent and
// no longer re-spreads when the desktop grows.
func TestCorralCommitsOnPress(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 608})
	win := NewWindow("corral")
	win.SetBounds(core.UnitRect{X: 600, Y: 100, Width: 160, Height: 120})
	m.AddWindow(win)

	// Shrink so the window is corralled but not yet committed.
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 300, Height: 608})
	disp := m.displayBounds(win)
	if win.Bounds().X == disp.X {
		t.Fatalf("precondition: corral already committed (logical X=%d)", win.Bounds().X)
	}

	// Press on the corralled window's title bar, then release in place.
	m.HandleMousePress(core.MousePressEvent{X: disp.X + 130, Y: disp.Y + 4, Button: core.LeftButton})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: disp.X + 130, Y: disp.Y + 4, Button: core.LeftButton})

	// Logical bounds are now committed to the corralled position.
	if win.Bounds().X != disp.X {
		t.Errorf("after press: logical X = %d, want committed %d", win.Bounds().X, disp.X)
	}

	// Growing the desktop back no longer re-spreads it.
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 608})
	if got := m.displayBounds(win); got.X != disp.X {
		t.Errorf("after commit+regrow: display X = %d, want %d (committed)", got.X, disp.X)
	}
}
