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
		got := ApplyResize(orig, c.edge, c.dx, c.dy, m, false, ca, unlimited)
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
	got := ApplyResize(orig, ResizeEdgeBottom, 0, 100, m, false, ca, unlimited)
	if got.Height != 200 {
		t.Errorf("height = %d, want capped at client area height 200", got.Height)
	}
}

// unlimited is a window that says nothing about how far it grows.
var unlimited = ResizeLimits{Maximum: core.UnitSize{Width: core.Unbounded, Height: core.Unbounded}}

// A window that states a maximum cannot be dragged past it, and the edge
// opposite the one under the pointer stays where the gesture found it.
func TestApplyResizeStopsAtTheStatedMaximum(t *testing.T) {
	m := core.DefaultCellMetrics()
	ca := core.UnitRect{X: 0, Y: 0, Width: 1000, Height: 1000}
	orig := core.UnitRect{X: 100, Y: 100, Width: 200, Height: 120}
	lim := ResizeLimits{Maximum: core.UnitSize{Width: 280, Height: 160}}

	cases := []struct {
		name string
		edge int
		dx   core.Unit
		dy   core.Unit
		want core.UnitRect
	}{
		{"right stops at the maximum width", ResizeEdgeRight, 400, 0,
			core.UnitRect{X: 100, Y: 100, Width: 280, Height: 120}},
		{"left stops with the right edge anchored", ResizeEdgeLeft, -400, 0,
			core.UnitRect{X: 20, Y: 100, Width: 280, Height: 120}},
		{"bottom stops at the maximum height", ResizeEdgeBottom, 0, 400,
			core.UnitRect{X: 100, Y: 100, Width: 200, Height: 160}},
		{"top stops with the bottom edge anchored", ResizeEdgeTop, 0, -400,
			core.UnitRect{X: 100, Y: 60, Width: 200, Height: 160}},
		{"a corner stops on both axes at once", ResizeEdgeRight | ResizeEdgeBottom, 400, 400,
			core.UnitRect{X: 100, Y: 100, Width: 280, Height: 160}},
		{"short of the maximum nothing is clamped", ResizeEdgeRight, 40, 0,
			core.UnitRect{X: 100, Y: 100, Width: 240, Height: 120}},
	}
	for _, c := range cases {
		if got := ApplyResize(orig, c.edge, c.dx, c.dy, m, false, ca, lim); got != c.want {
			t.Errorf("%s: ApplyResize = %+v, want %+v", c.name, got, c.want)
		}
	}
}

// A stated minimum raises the shared 3x2-cell floor, and where the two
// limits meet the minimum is the one that holds.
func TestApplyResizeHoldsTheStatedMinimum(t *testing.T) {
	m := core.DefaultCellMetrics()
	ca := core.UnitRect{X: 0, Y: 0, Width: 1000, Height: 1000}
	orig := core.UnitRect{X: 100, Y: 100, Width: 200, Height: 120}

	lim := ResizeLimits{
		Minimum: core.UnitSize{Width: 150, Height: 90},
		Maximum: core.UnitSize{Width: core.Unbounded, Height: core.Unbounded},
	}
	if got := ApplyResize(orig, ResizeEdgeRight, -400, 0, m, false, ca, lim); got.Width != 150 {
		t.Errorf("width = %d, want the stated minimum 150", got.Width)
	}

	// Minimum over maximum: a band capped below its own floor keeps the floor.
	crossed := ResizeLimits{
		Minimum: core.UnitSize{Width: 150, Height: 90},
		Maximum: core.UnitSize{Width: 100, Height: 60},
	}
	got := ApplyResize(orig, ResizeEdgeRight|ResizeEdgeBottom, 400, 400, m, false, ca, crossed)
	if got.Width != 150 || got.Height != 90 {
		t.Errorf("size = %v, want the minimum 150x90 to win over the maximum", got.Size())
	}
}
