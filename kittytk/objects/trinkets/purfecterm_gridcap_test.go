package trinkets

import "testing"

// A terminal grid dimension is capped. A degenerate fit — a near-zero cell size
// mid-resize, or a size-feedback runaway between a hosted terminal and its host
// — used to resize the grid to hundreds of thousands of cells, which the
// emulator allocated (one makeEmptyLine per row) into a multi-gigabyte buffer
// that OOM-killed the process. clampGridDim is the safety net both fit paths
// (updateTerminalSize and paintGraphical) pass their computed dimensions
// through.
func TestClampGridDim(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 0},
		{80, 80},
		{maxTermGridDim, maxTermGridDim},
		{maxTermGridDim + 1, maxTermGridDim},
		{600000, maxTermGridDim}, // the observed runaway row count
		{-1, 0},
		{-600000, 0},
	}
	for _, c := range cases {
		if got := clampGridDim(c.in); got != c.want {
			t.Errorf("clampGridDim(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
