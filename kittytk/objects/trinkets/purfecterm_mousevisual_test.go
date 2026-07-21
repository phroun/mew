package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Mouse reports forwarded to the hosted app must carry PHYSICAL viewport
// cells (visual columns), not screenToCellGfx's logical buffer cells: with
// standard-contract wide cells (purfecterm >= v0.2.23, CJK = one logical cell
// of width 2) the two diverge, and reporting the logical cell parks the
// hosted app's caret left of the click.
func TestMouseReportVisualColumns(t *testing.T) {
	tr := NewPurfecTerm()
	if tr.terminal == nil {
		t.Skip("no embedded terminal")
	}
	tr.terminal.FeedString("日abc")

	cw, ch := tr.cellDims()
	// A click in the middle of screen column 3 ('b': 日 spans columns 0-1,
	// 'a' is column 2).
	x := core.Unit(float64(cw) * 3.5)
	y := ch / 2

	vx, vy := tr.screenToVisualCellGfx(x, y)
	if vx != 3 || vy != 0 {
		t.Fatalf("visual mapping = (%d,%d), want (3,0)", vx, vy)
	}
	// The logical mapping lands one cell earlier — right for selection,
	// wrong for reports; this divergence is the regression this test guards.
	lx, _ := tr.screenToCellGfx(x, y)
	if lx != 2 {
		t.Fatalf("logical mapping = %d, want 2 (buffer cell of 'b')", lx)
	}
}
