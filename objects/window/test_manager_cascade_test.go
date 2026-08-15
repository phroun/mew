package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Cascade repositions every window but only resizes the ones that can be
// resized: a fixed-size (NoResize) window keeps its own dimensions and is
// merely moved, while a normal window adopts the standard 3/4 cascade size.
func TestCascadeKeepsNonResizableSize(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 600})

	fixed := NewWindow("fixed")
	fixed.SetFlags(WindowFlagNoResize)
	m.AddWindow(fixed)
	fixed.SetBounds(core.UnitRect{X: 10, Y: 10, Width: 120, Height: 80})

	flex := NewWindow("flex")
	m.AddWindow(flex)
	flex.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 100, Height: 100})

	m.CascadeWindows()

	if fb := fixed.Bounds(); fb.Width != 120 || fb.Height != 80 {
		t.Errorf("cascade resized a NoResize window to %dx%d, want 120x80", fb.Width, fb.Height)
	}

	metrics := core.DefaultCellMetrics()
	wantW := metrics.RoundDownToCellX(800 * 3 / 4)
	wantH := metrics.RoundDownToCellY(600 * 3 / 4)
	if xb := flex.Bounds(); xb.Width != wantW || xb.Height != wantH {
		t.Errorf("cascade sized the resizable window to %dx%d, want %dx%d", xb.Width, xb.Height, wantW, wantH)
	}
}
