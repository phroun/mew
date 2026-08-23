package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The highlight bands are window-LOCAL and derived from the window's bounds,
// so a resize in flight invalidates them. Nothing else recomputes them
// mid-gesture — the hover path that built them is skipped while resizing — so
// a stale set leaves the bands hanging where the drag started.
//
// A single edge can hide this: the left and top bands are anchored at 0 and
// do not move. A CORNER always moves at least one band, which is where it
// shows.
func TestResizeHoverBandsFollowTheWindow(t *testing.T) {
	const grip = core.Unit(4)
	start := core.UnitRect{Width: 100, Height: 60}
	grown := core.UnitRect{Width: 160, Height: 90}

	// Bottom-right corner: both bands are positioned from the far edges.
	before := tornEdgeRects(start, resizeRight|resizeBottom, grip)
	after := tornEdgeRects(grown, resizeRight|resizeBottom, grip)
	if len(before) != 2 || len(after) != 2 {
		t.Fatalf("a corner arms two bands; got %d and %d", len(before), len(after))
	}
	if sameRects(before, after) {
		t.Fatal("the corner's bands must move with the window; they did not")
	}
	// The right band tracks the new width, the bottom band the new height.
	if after[0].X != grown.Width-grip {
		t.Errorf("right band at X=%v, want %v", after[0].X, grown.Width-grip)
	}
	if after[0].Height != grown.Height {
		t.Errorf("right band height %v, want the window's %v", after[0].Height, grown.Height)
	}
	if after[1].Y != grown.Height-grip {
		t.Errorf("bottom band at Y=%v, want %v", after[1].Y, grown.Height-grip)
	}
	if after[1].Width != grown.Width {
		t.Errorf("bottom band width %v, want the window's %v", after[1].Width, grown.Width)
	}

	// The top-left corner's bands are anchored at 0, but still SPAN the
	// window, so they resize even though they do not move.
	tl := tornEdgeRects(grown, resizeLeft|resizeTop, grip)
	if tl[0].Height != grown.Height || tl[1].Width != grown.Width {
		t.Errorf("top-left bands must span the window: %+v", tl)
	}
	if sameRects(tornEdgeRects(start, resizeLeft|resizeTop, grip), tl) {
		t.Fatal("top-left bands must still re-derive when the window resizes")
	}
}

// refreshResizeHover only acts during a resize, and keeps the ARMED edges —
// the pointer may have wandered off them, but the gesture still owns them.
func TestRefreshResizeHoverOnlyWhileResizing(t *testing.T) {
	w := NewWindow("t")
	small := core.UnitRect{Width: 100, Height: 60}
	w.SetBounds(small)
	h := &TearOffHost{win: w, graphicalFrames: true}

	h.resizing = false
	h.resizeEdges = resizeRight
	w.SetResizeHoverEdges(0, 0)
	h.refreshResizeHover()
	if len(w.resizeHoverBands(small)) != 0 {
		t.Fatal("no highlight should be set when no resize is in flight")
	}

	h.resizing = true
	h.refreshResizeHover()
	if got := w.resizeHoverBands(small); len(got) != 1 {
		t.Fatalf("the armed edge should be highlighted; got %d bands", len(got))
	}
}

// The band must be resolved against the bounds the PAINT is using. The OS
// reports a resize back asynchronously, so a rectangle measured when the
// pointer moved carries the size the window had a frame ago — which is how
// stretching a window to the right leaves its band stranded mid-frame.
func TestResizeBandFollowsThePaintBounds(t *testing.T) {
	w := NewWindow("t")
	w.SetBounds(core.UnitRect{Width: 100, Height: 60})
	h := &TearOffHost{win: w, graphicalFrames: true}
	h.resizing = true
	h.resizeEdges = resizeRight
	h.refreshResizeHover()

	// The window's own bounds are deliberately left STALE here: this is the
	// state a live resize is in when the frame is painted.
	grown := core.UnitRect{Width: 300, Height: 60}
	got := w.resizeHoverBands(grown)
	if len(got) != 1 {
		t.Fatalf("want one band, got %d", len(got))
	}
	// The band spans the effective grip (the resize sliver plus the painted
	// frame border), the same width the grab zone uses — so it is measured from
	// the painted width, not the stale bounds.
	wantX := grown.Width - h.effectiveGrip()
	if got[0].X != wantX {
		t.Errorf("band at X=%v, want %v — it is pinned to the old width, not the painted one",
			got[0].X, wantX)
	}
	if got[0].Height != grown.Height {
		t.Errorf("band height %v, want the painted %v", got[0].Height, grown.Height)
	}

	// Explicit rectangles (the older setter) are still honoured verbatim.
	w2 := NewWindow("t2")
	w2.SetResizeHoverRects([]core.UnitRect{{X: 1, Y: 2, Width: 3, Height: 4}})
	if got := w2.resizeHoverBands(grown); len(got) != 1 || got[0].X != 1 {
		t.Errorf("explicit rects should pass through unchanged: %+v", got)
	}
}

// When a torn window is small enough (or its border wide enough) that the
// opposite resize grips overlap, a pointer in the overlap must not let one
// side always win. The pointer's half decides: before the 50% line the near
// edge (left/top) takes it, at or past it the far edge (right/bottom) does —
// so every handle stays reachable.
func TestEdgeAtSplitsOverlappingGrips(t *testing.T) {
	w := NewWindow("t")
	// A window small enough that the grab zones overlap. The hit grip is the
	// rule's width (3 units here: 3 device pixels at ppu 1 beats a quarter of
	// an 8-unit cell), NOT the configured resizeGrip, which now only says
	// "graphical frame" — so the overlap comes from the window being 4 wide,
	// where the left zone (x<3) and the right zone (x>=1) cover each other.
	w.SetBounds(core.UnitRect{Width: 4, Height: 4})
	h := &TearOffHost{win: w, graphicalFrames: true}

	cases := []struct {
		name      string
		x, y      core.Unit
		wantHas   int
		wantHasnt int
	}{
		// Horizontal overlap: midpoint is x=2.
		{"left half -> left, not right", 1, 2, resizeLeft, resizeRight},
		{"right half -> right, not left", 2, 2, resizeRight, resizeLeft},
		// Vertical overlap: midpoint is y=2. Use x in the horizontal overlap
		// so only the vertical resolution is under test.
		{"top half -> top, not bottom", 2, 1, resizeTop, resizeBottom},
		{"bottom half -> bottom, not top", 2, 2, resizeBottom, resizeTop},
	}
	for _, c := range cases {
		e := h.edgeAt(c.x, c.y)
		if e&c.wantHas == 0 {
			t.Errorf("%s: edgeAt(%d,%d)=%b missing expected edge %b", c.name, c.x, c.y, e, c.wantHas)
		}
		if e&c.wantHasnt != 0 {
			t.Errorf("%s: edgeAt(%d,%d)=%b should not include %b", c.name, c.x, c.y, e, c.wantHasnt)
		}
	}
}
