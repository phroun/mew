package trinkets

import (
	"image"
	"image/color"
	"testing"
)

// A face whose presentation forms carry side bearing ends its ink before the
// advance edge. The no-tatweel fill path (k=nil) must still bridge from the
// letter's REAL ink to the cell edge — not from the advance box — so the join
// never floats detached. Synthesize such a glyph and check both edges fill.
func TestJoinFillClosesSideBearingGap(t *testing.T) {
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

	term := &PurfecTerm{}

	// Right side: fill from the letter's rightmost band ink (x=13) to the edge.
	edge := -1
	for x := boxW - 1; x >= 0; x-- {
		if colInkedInBand(out, x, top, bot) {
			edge = x
			break
		}
	}
	if edge != 13 {
		t.Fatalf("right ink edge = %d, want 13", edge)
	}
	term.fillJoin(out, nil, nil, boxH, 1.0, edge, boxW, top, bot)
	if !colInkedInBand(out, boxW-1, top, bot) {
		t.Errorf("right cell edge not filled after join")
	}

	// Left side: fill from 0 to the leftmost band ink (x=6).
	edgeL := -1
	for x := 0; x < boxW; x++ {
		if colInkedInBand(out, x, top, bot) {
			edgeL = x
			break
		}
	}
	if edgeL != 6 {
		t.Fatalf("left ink edge = %d, want 6", edgeL)
	}
	term.fillJoin(out, nil, nil, boxH, 1.0, 0, edgeL+1, top, bot)
	if !colInkedInBand(out, 0, top, bot) {
		t.Errorf("left cell edge not filled after join")
	}

	// The whole baseline band is now continuous edge to edge.
	for x := 0; x < boxW; x++ {
		if !colInkedInBand(out, x, top, bot) {
			t.Errorf("baseline band has a gap at column %d", x)
		}
	}
}

// arabicJoinBand falls back to the letter's lowest ink rows when no tatweel mask
// is available, so the smear path targets the baseline rather than an ascender.
func TestArabicJoinBandFallback(t *testing.T) {
	const boxW, boxH = 20, 30
	white := color.RGBA{255, 255, 255, 255}
	out := image.NewRGBA(image.Rect(0, 0, boxW, boxH))
	// A tall stroke up high (rows 4-6) plus a baseline nub (rows 25-26).
	for x := 8; x < 12; x++ {
		for _, y := range []int{4, 5, 6, 25, 26} {
			out.SetRGBA(x, y, white)
		}
	}
	top, bot := arabicJoinBand(nil, out, boxH)
	if bot != 26 {
		t.Errorf("band bottom = %d, want 26 (baseline, not ascender)", bot)
	}
	if top > 25 || top < 20 {
		t.Errorf("band top = %d, want it near the baseline (~20-25)", top)
	}
}
