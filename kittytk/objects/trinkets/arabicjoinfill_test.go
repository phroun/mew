package trinkets

import (
	"image"
	"image/color"
	"testing"
)

// A face whose presentation forms carry side bearing ends its ink before the
// advance edge. extendArabicStroke must still bridge from the letter's REAL ink
// to the cell edge — not from the advance box — so the join never floats
// detached. Synthesize such a glyph and check both edges fill.
func TestExtendArabicStrokeClosesSideBearingGap(t *testing.T) {
	const boxW, boxH = 20, 30
	white := color.RGBA{255, 255, 255, 255}

	// Baseline nub inset 6px from each side (side bearing), rows 24-25.
	out := image.NewRGBA(image.Rect(0, 0, boxW, boxH))
	const top, bot = 24, 25
	for y := top; y <= bot; y++ {
		for x := 6; x < boxW-6; x++ {
			out.SetRGBA(x, y, white)
		}
	}

	extendArabicStroke(out, boxW, boxH, true)  // right
	extendArabicStroke(out, boxW, boxH, false) // left

	if !colInked(out, boxW-1) {
		t.Errorf("right cell edge not filled after join")
	}
	if !colInked(out, 0) {
		t.Errorf("left cell edge not filled after join")
	}
	// The baseline band is now continuous edge to edge; rows outside it stay clear.
	for x := 0; x < boxW; x++ {
		if out.RGBAAt(x, top).A == 0 {
			t.Errorf("baseline row %d has a gap at column %d", top, x)
		}
		if out.RGBAAt(x, 0).A != 0 {
			t.Errorf("non-baseline row 0 wrongly inked at column %d", x)
		}
	}
}

// An isolated form (ink already spanning to both cell edges, or none) gets no
// extension: extendArabicStroke only fills when there is an interior ink edge.
func TestExtendArabicStrokeNoStrayFill(t *testing.T) {
	const boxW, boxH = 20, 30
	// Empty cell: nothing to extend, must stay empty.
	empty := image.NewRGBA(image.Rect(0, 0, boxW, boxH))
	extendArabicStroke(empty, boxW, boxH, true)
	extendArabicStroke(empty, boxW, boxH, false)
	for x := 0; x < boxW; x++ {
		if colInked(empty, x) {
			t.Fatalf("empty cell should stay empty, column %d inked", x)
		}
	}
}
