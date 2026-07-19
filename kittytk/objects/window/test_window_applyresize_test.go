package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// ApplyResize is the shared resize-geometry rule for both the desktop
// WindowManager and the embedded MDIPane; these cases pin the edge math,
// minimum-size enforcement, and client-area clamping.
func TestApplyResize(t *testing.T) {
	m := core.DefaultCellMetrics() // 8x16; min window = 24x32
	ca := core.UnitRect{X: 0, Y: 0, Width: 1000, Height: 1000}
	orig := core.UnitRect{X: 10, Y: 10, Width: 100, Height: 50}

	cases := []struct {
		name string
		edge int
		dx   core.Unit
		dy   core.Unit
		want core.UnitRect
	}{
		{"right grows width", ResizeEdgeRight, 20, 0, core.UnitRect{X: 10, Y: 10, Width: 120, Height: 50}},
		{"left moves x and grows width", ResizeEdgeLeft, -10, 0, core.UnitRect{X: 0, Y: 10, Width: 110, Height: 50}},
		{"bottom grows height", ResizeEdgeBottom, 0, 30, core.UnitRect{X: 10, Y: 10, Width: 100, Height: 80}},
		{"right clamps to min width, x stays", ResizeEdgeRight, -90, 0, core.UnitRect{X: 10, Y: 10, Width: 24, Height: 50}},
		{"left clamps to min width, x anchored to right", ResizeEdgeLeft, 90, 0, core.UnitRect{X: 86, Y: 10, Width: 24, Height: 50}},
		{"top clamps at client top, height absorbs it", ResizeEdgeTop, 0, -100, core.UnitRect{X: 10, Y: 0, Width: 100, Height: 60}},
	}
	for _, c := range cases {
		got := ApplyResize(orig, c.edge, c.dx, c.dy, m, false, ca)
		if got != c.want {
			t.Errorf("%s: ApplyResize = %+v, want %+v", c.name, got, c.want)
		}
	}
}

// The height is capped at the client area height (windows may be wider
// than the area but not taller).
func TestApplyResizeHeightCappedToClientArea(t *testing.T) {
	m := core.DefaultCellMetrics()
	ca := core.UnitRect{X: 0, Y: 0, Width: 1000, Height: 200}
	orig := core.UnitRect{X: 0, Y: 0, Width: 100, Height: 180}
	got := ApplyResize(orig, ResizeEdgeBottom, 0, 100, m, false, ca)
	if got.Height != 200 {
		t.Errorf("height = %d, want capped at client area height 200", got.Height)
	}
}
