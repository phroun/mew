package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// resizeEdgeRects produces one band per edge (two for a corner), sized to
// the resize grip and spanning the window.
func TestResizeEdgeRects(t *testing.T) {
	m := NewWindowManager()
	w := NewWindow("w")
	w.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 200, Height: 120})
	m.AddWindow(w)

	metrics := core.DefaultCellMetrics()

	// A single edge -> one band.
	if got := m.resizeEdgeRects(w, ResizeEdgeRight); len(got) != 1 {
		t.Fatalf("right edge: want 1 rect, got %d", len(got))
	} else if want := (core.UnitRect{X: 200 - metrics.CellWidth, Width: metrics.CellWidth, Height: 120}); got[0] != want {
		t.Errorf("right band = %v, want %v", got[0], want)
	}

	// A bottom-left corner -> two bands (left + bottom).
	got := m.resizeEdgeRects(w, ResizeEdgeLeft|ResizeEdgeBottom)
	if len(got) != 2 {
		t.Fatalf("corner: want 2 rects, got %d", len(got))
	}
	wantLeft := core.UnitRect{Width: metrics.CellWidth, Height: 120}
	wantBottom := core.UnitRect{Y: 120 - metrics.CellHeight, Width: 200, Height: metrics.CellHeight}
	if got[0] != wantLeft {
		t.Errorf("left band = %v, want %v", got[0], wantLeft)
	}
	if got[1] != wantBottom {
		t.Errorf("bottom band = %v, want %v", got[1], wantBottom)
	}
}

// SetResizeHoverRects reports a change only when the highlight set differs.
func TestSetResizeHoverRectsChangeDetection(t *testing.T) {
	w := NewWindow("w")
	rects := []core.UnitRect{{Width: 8, Height: 100}}

	if !w.SetResizeHoverRects(rects) {
		t.Error("first non-empty set should report a change")
	}
	if w.SetResizeHoverRects(rects) {
		t.Error("setting the same rects should not report a change")
	}
	if !w.SetResizeHoverRects(nil) {
		t.Error("clearing should report a change")
	}
	if w.SetResizeHoverRects(nil) {
		t.Error("clearing when already clear should not report a change")
	}
}

// ClearResizeHover removes the highlight from every window (used on
// mouse-leave, when no move event arrives to clear it).
func TestClearResizeHover(t *testing.T) {
	m := NewWindowManager()
	w := NewWindow("w")
	w.SetBounds(core.UnitRect{Width: 200, Height: 120})
	m.AddWindow(w)

	w.SetResizeHoverRects([]core.UnitRect{{Width: 8, Height: 120}})
	m.ClearResizeHover()

	// Already clear -> setting nil again reports no change.
	if w.SetResizeHoverRects(nil) {
		t.Error("ClearResizeHover left a stale highlight")
	}
}
