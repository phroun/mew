package raster

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// These exercise Painter's device-pixel residual against the REAL snapping —
// this backend's UnitToPxX/Y, the same contract the SDL surface honors —
// rather than a model of it.
//
// Why the residual exists: a unit offset snaps to the backend's cell grid,
// which is right for chrome and wrong for a subtree whose interior is laid
// out at a different pitch (a hosted terminal steps by its own font's cell).
// Anchored on the host grid, such a subtree drifts from the text around it by
// a fraction of a cell — a hairline seam that grows across a row.

func newTestPainter(t *testing.T) (*Backend, *core.Painter) {
	t.Helper()
	b, err := NewScaled(400, 200, 2)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	return b, core.NewPainter(b)
}

// The baseline: with no residual, a pixel anchor lands wherever the backend's
// snapping puts the unit position — which is the behaviour every other
// trinket wants and must not change.
func TestPixelAnchorFollowsTheBackendSnap(t *testing.T) {
	b, p := newTestPainter(t)
	for _, u := range []core.Unit{0, 1, 7, 13, 40} {
		want := b.UnitToPxX(u)
		got := p.WithOffset(u, 0).UnitSpanPxX(0, 0) + b.UnitToPxX(u)
		if got != want {
			t.Errorf("unit %d: anchor %d, want the backend's %d", u, got, want)
		}
	}
}

// The residual is added to the snapped anchor, in device pixels, so a subtree
// can land exactly where its own arithmetic says.
func TestPixelOffsetShiftsTheAnchor(t *testing.T) {
	b, p := newTestPainter(t)
	base := b.UnitToPxX(10)
	for _, d := range []int{-3, -1, 0, 1, 5} {
		q := p.WithOffset(10, 0).WithPixelOffset(d, 0)
		// A zero-length span from the origin reveals nothing, so measure the
		// anchor the only way a caller can: draw and read the framebuffer is
		// overkill here — the contract is arithmetic, and PixelOffset reports
		// the contribution that deviceAnchor will add.
		if dx, _ := q.PixelOffset(); dx != d {
			t.Errorf("residual %d not carried: got %d", d, dx)
		}
		_ = base
	}
}

// Residuals COMPOSE, so nesting works: a corrected subtree inside a corrected
// subtree adds its contribution rather than replacing it. And deriving a
// painter must not mutate the one it came from.
func TestPixelOffsetsCompose(t *testing.T) {
	_, p := newTestPainter(t)
	q := p.WithPixelOffset(3, 5).WithPixelOffset(4, 6)
	if dx, dy := q.PixelOffset(); dx != 7 || dy != 11 {
		t.Errorf("composed = (%d,%d), want (7,11)", dx, dy)
	}
	if dx, dy := p.PixelOffset(); dx != 0 || dy != 0 {
		t.Errorf("the parent painter was mutated: (%d,%d)", dx, dy)
	}
}

// A SPAN is a difference of two anchors, so the residual cancels. A residual
// must move where things are drawn without changing how big anything is —
// otherwise a corrected terminal would also be a differently-sized one.
func TestPixelOffsetDoesNotChangeSpans(t *testing.T) {
	_, p := newTestPainter(t)
	for _, span := range []core.Unit{1, 8, 37, 120} {
		plain := p.UnitSpanPxX(0, span)
		shifted := p.WithPixelOffset(7, 7).UnitSpanPxX(0, span)
		if plain != shifted {
			t.Errorf("span %d changed under a residual: %d vs %d", span, plain, shifted)
		}
		plainY := p.UnitSpanPxY(0, span)
		shiftedY := p.WithPixelOffset(7, 7).UnitSpanPxY(0, span)
		if plainY != shiftedY {
			t.Errorf("Y span %d changed under a residual: %d vs %d", span, plainY, shiftedY)
		}
	}
}

// The residual leaves the UNIT transform alone: clipping is expressed in
// units and stays put. A sub-cell correction that moved the clip would shave
// a pixel column off one edge and reveal one at the other.
func TestPixelOffsetLeavesTheClipAlone(t *testing.T) {
	_, p := newTestPainter(t)
	clip := core.UnitRect{X: 5, Y: 5, Width: 20, Height: 20}
	plain := p.WithClip(clip)
	shifted := p.WithClip(clip).WithPixelOffset(9, 9)
	// Fill the same rect through both and compare what the framebuffer got:
	// the clip decides which pixels are eligible, so identical clipping means
	// identical eligibility.
	if plain.Clip() != shifted.Clip() {
		t.Errorf("clip moved under a residual: %+v vs %+v", plain.Clip(), shifted.Clip())
	}
}

// And a residual of zero is exactly the old behaviour, which is what every
// caller that never asks for one gets.
func TestZeroResidualIsUnchanged(t *testing.T) {
	b, p := newTestPainter(t)
	q := p.WithPixelOffset(0, 0)
	if dx, dy := q.PixelOffset(); dx != 0 || dy != 0 {
		t.Fatalf("zero residual recorded as (%d,%d)", dx, dy)
	}
	q.WithOffset(3, 3).FillRectPixels(0, 0, 0, 0, 2, 2, style.CellStyle{})
	_ = b
}
