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
	w.SetBounds(core.UnitRect{Width: 100, Height: 60})
	h := &TearOffHost{win: w, resizeGrip: 4}

	h.resizing = false
	h.resizeEdges = resizeRight
	w.SetResizeHoverRects(nil)
	h.refreshResizeHover()
	if len(w.ResizeHoverRects()) != 0 {
		t.Fatal("no highlight should be set when no resize is in flight")
	}

	h.resizing = true
	h.refreshResizeHover()
	got := w.ResizeHoverRects()
	if len(got) != 1 {
		t.Fatalf("the armed edge should be highlighted; got %d bands", len(got))
	}
	// Grow the window: the band must follow on the next refresh.
	w.SetBounds(core.UnitRect{Width: 200, Height: 60})
	h.refreshResizeHover()
	if got = w.ResizeHoverRects(); len(got) != 1 || got[0].X != 200-4 {
		t.Fatalf("band did not follow the resize: %+v", got)
	}
}
