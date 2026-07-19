package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// An MDI child left off-screen by a pane shrink is corralled into view
// for display only; its logical bounds are untouched, so growing the
// pane back re-spreads it to where it was.
func TestMDIProvisionalCorralRespread(t *testing.T) {
	m := NewMDIPane()
	m.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 400, Height: 300})
	win := window.NewWindow("corral")
	win.SetBounds(core.UnitRect{X: 300, Y: 50, Width: 120, Height: 100})
	m.AddWindow(win)

	// Fits: display == logical.
	if got := m.displayBounds(win); got.X != 300 {
		t.Fatalf("fits: display X = %d, want 300", got.X)
	}

	// Shrink the pane so the window overhangs the right edge.
	m.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 200, Height: 300})
	disp := m.displayBounds(win)
	if disp.X >= 300 {
		t.Errorf("shrunk: window not corralled (display X=%d)", disp.X)
	}
	// Provisional: logical bounds unchanged.
	if win.Bounds().X != 300 {
		t.Errorf("shrunk: logical X mutated to %d, want 300 (provisional)", win.Bounds().X)
	}

	// Grow back: re-spread to the original spot.
	m.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 400, Height: 300})
	if got := m.displayBounds(win); got.X != 300 {
		t.Errorf("regrown: display X = %d, want re-spread to 300", got.X)
	}
}
