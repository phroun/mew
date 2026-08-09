package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// ResizeEdgeAt is the shared resize-edge detector for desktop and MDI
// windows. When a window is small enough (or its grip wide enough — a wide
// scaled frame border does exactly this) that opposite grips overlap, a
// pointer in the overlap must not let one side always win. The pointer's
// half decides: before the window's 50% line the near edge (left/top) takes
// it, at or past it the far edge (right/bottom) does — so every handle stays
// reachable at any size. Coordinates are in the window's space, so this also
// exercises a docked window at a non-zero origin.
func TestResizeEdgeAtSplitsOverlappingGrips(t *testing.T) {
	m := core.DefaultCellMetrics()

	// Horizontal overlap: a narrow window (W=40) with a grip (30) wider than
	// half of it, at a non-zero origin. Tall enough (H=100) that the vertical
	// middle is clear of the top/bottom grips, isolating the horizontal split.
	// Local midpoint is lx=20 (x=120).
	hb := core.UnitRect{X: 100, Y: 50, Width: 40, Height: 100}
	const grip = core.Unit(30)
	if e := ResizeEdgeAt(hb, 115, 100, m, grip); e&ResizeEdgeLeft == 0 || e&ResizeEdgeRight != 0 {
		t.Errorf("left half: edge=%b, want Left set, Right clear", e)
	}
	if e := ResizeEdgeAt(hb, 125, 100, m, grip); e&ResizeEdgeRight == 0 || e&ResizeEdgeLeft != 0 {
		t.Errorf("right half: edge=%b, want Right set, Left clear", e)
	}

	// Vertical overlap: a short window (H=40) with the same grip, wide enough
	// (W=100) to isolate the vertical split. Local midpoint is ly=20 (y=70).
	vb := core.UnitRect{X: 100, Y: 50, Width: 100, Height: 40}
	if e := ResizeEdgeAt(vb, 150, 65, m, grip); e&ResizeEdgeTop == 0 || e&ResizeEdgeBottom != 0 {
		t.Errorf("top half: edge=%b, want Top set, Bottom clear", e)
	}
	if e := ResizeEdgeAt(vb, 150, 75, m, grip); e&ResizeEdgeBottom == 0 || e&ResizeEdgeTop != 0 {
		t.Errorf("bottom half: edge=%b, want Bottom set, Top clear", e)
	}

	// A far corner of a tiny window resolves to exactly one horizontal and one
	// vertical edge (a clean diagonal), never both lefts-and-rights.
	tiny := core.UnitRect{Width: 30, Height: 30}
	if e := ResizeEdgeAt(tiny, 25, 25, m, grip); e != (ResizeEdgeRight | ResizeEdgeBottom) {
		t.Errorf("bottom-right of tiny window: edge=%b, want Right|Bottom exactly", e)
	}
	if e := ResizeEdgeAt(tiny, 4, 4, m, grip); e != (ResizeEdgeLeft | ResizeEdgeTop) {
		t.Errorf("top-left of tiny window: edge=%b, want Left|Top exactly", e)
	}
}

// A normal, roomy window is unaffected: edges only near the borders, nothing
// in the interior, and the top row stays a titlebar (not grabbable) under the
// cell frame (grip 0).
func TestResizeEdgeAtNormalWindowUnchanged(t *testing.T) {
	m := core.DefaultCellMetrics()
	b := core.UnitRect{Width: 200, Height: 200}

	if e := ResizeEdgeAt(b, 3, 100, m, 8); e != ResizeEdgeLeft {
		t.Errorf("left edge: edge=%b, want Left only", e)
	}
	if e := ResizeEdgeAt(b, 197, 100, m, 8); e != ResizeEdgeRight {
		t.Errorf("right edge: edge=%b, want Right only", e)
	}
	if e := ResizeEdgeAt(b, 100, 100, m, 8); e != ResizeEdgeNone {
		t.Errorf("interior: edge=%b, want None", e)
	}
	if e := ResizeEdgeAt(b, 3, 3, m, 8); e != (ResizeEdgeLeft | ResizeEdgeTop) {
		t.Errorf("top-left corner: edge=%b, want Left|Top", e)
	}
	// grip 0 (cell frame): the top row is the titlebar, not a resize edge.
	if e := ResizeEdgeAt(b, 100, 0, m, 0); e&ResizeEdgeTop != 0 {
		t.Errorf("cell-frame top row should not be grabbable: edge=%b", e)
	}
}
