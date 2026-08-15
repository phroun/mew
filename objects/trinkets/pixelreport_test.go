package trinkets

import (
	"math"
	"testing"
)

// The synthetic ?1016 report must recover the SAME cell the paint draws, at
// every column, even when the cell's advance is fractional in device pixels —
// the bug where the caret drifted up to a full cell by the far edge of the
// screen. For each cell we drop the pointer at that cell's painted center and
// its left edge and confirm the report divides back to the same cell (no
// drift), with the fraction on the correct half.
func TestPixelReportNoDrift(t *testing.T) {
	// A deliberately awkward fractional advance: 9.4 device px per cell.
	const (
		adv = 9.4 // cell advance in units
		ppu = 1.0 // 1 px per unit → advance*ppu = 9.4 px, non-integer
		n   = 200 // wide screen, where uniform division would drift badly
	)
	for col := 0; col < n; col++ {
		left := cellBoundaryPx(float64(col)*adv, ppu)
		right := cellBoundaryPx(float64(col+1)*adv, ppu)
		center := (left + right) / 2

		for _, tc := range []struct {
			pt       float64
			wantHalf string // "left" (<500) or "right" (>=500)
		}{
			{left, "left"}, // painted left edge → left half
			{center - 0.01, "left"},
			{center + 0.01, "right"},
			{right - 0.01, "right"}, // just inside the painted right edge
		} {
			rep := pixelReportAxis(tc.pt, adv, ppu, n)
			gotCol := rep / gfxCellSubUnits
			if gotCol != col {
				t.Fatalf("col %d, pt %.2f: report %d → cell %d (drift of %d)",
					col, tc.pt, rep, gotCol, gotCol-col)
			}
			sub := rep % gfxCellSubUnits
			half := "left"
			if sub >= 500 {
				half = "right"
			}
			// The exact center can land on either side by a rounding hair; only
			// assert the halves that are a hair off center or at an edge.
			if math.Abs(tc.pt-center) > 0.005 && half != tc.wantHalf {
				t.Errorf("col %d, pt %.2f: sub %d is %s half, want %s",
					col, tc.pt, sub, half, tc.wantHalf)
			}
		}
	}
}

// A pointer past the last cell clamps to it (never a phantom extra column), and
// one before the first stays on cell 0 — the report is always a real cell.
func TestPixelReportClamps(t *testing.T) {
	const (
		adv = 8.0
		ppu = 2.0
		n   = 10
	)
	if got := pixelReportAxis(-50, adv, ppu, n) / gfxCellSubUnits; got != 0 {
		t.Errorf("far-left pointer → cell %d, want 0", got)
	}
	if got := pixelReportAxis(1e9, adv, ppu, n) / gfxCellSubUnits; got != n-1 {
		t.Errorf("far-right pointer → cell %d, want %d", got, n-1)
	}
}
