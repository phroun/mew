package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The grab rule, stated as the number you actually feel: how far in from a
// window's outer edge a press resizes instead of reaching content — the frame
// border plus a quarter column, floored at 3 device pixels.
//
// Nothing asserted this before, which is how three separate errors stacked
// unnoticed into a zone of five-eighths of a cell — the device scale counted
// twice (units already carry it through ppu), a "3 device pixel" floor that
// was really 3 UNITS and so beat the quarter cell outright, and the frame
// border added on top of a zone measured from the outer edge, where the
// border already sat.
func TestResizeHitGripIsTheBorderPlusAQuarterCell(t *testing.T) {
	cell := core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}

	for _, c := range []struct {
		name   string
		ppu    float64
		border core.Unit
		want   core.Unit
	}{
		// The border plus a quarter column: a constant margin of CONTENT
		// however thick the frame is.
		{"border plus a quarter cell", 1, 2, 4},
		{"no border: the quarter cell alone", 2, 0, 2},
		// The quarter column is unchanged in UNITS as the zoom rises, because
		// units already grow with the zoom. Multiplying by the device scale as
		// well is what squared it into half a cell and then three quarters.
		{"ppu 2: unchanged by zoom", 2, 2, 4},
		{"ppu 4: still unchanged", 4, 2, 4},
		// A fat frame carries the quarter column out with it.
		{"fat border", 1, 20, 22},
		// Zoomed OUT far enough that border plus a quarter cell is under three
		// device pixels, the pixel floor takes over — real device pixels,
		// converted through ppu rather than through the integer scale.
		{"tiny ppu: 3 device px floor", 0.25, 0, 12},
	} {
		// At the default cell a quarter column and an eighth of a row are the
		// same two units, so both axes answer alike here.
		got := ResizeHitGrip(true, cell, c.ppu, c.border, c.border)
		if got.X != c.want || got.Y != c.want {
			t.Errorf("%s: ResizeHitGrip(ppu=%v, border=%v) = %v units, want %v across and down",
				c.name, c.ppu, c.border, got, c.want)
		}
	}
}

// The cell frame is untouched by any of this: there the whole border
// row/column IS the grip, and ResizeEdgeAt's metrics defaults apply.
func TestResizeHitGripLeavesTheCellFrameAlone(t *testing.T) {
	cell := core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}
	if got := ResizeHitGrip(false, cell, 1, 2, 2); !got.Zero() {
		t.Errorf("cell frame: ResizeHitGrip = %v, want zero (metrics defaults apply)", got)
	}
}

// End to end: with the rule in force, content is clickable from the first
// pixel past the border rather than most of a cell inside the window.
func TestContentIsClickableJustPastTheBorder(t *testing.T) {
	cell := core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}
	bounds := core.UnitRect{X: 100, Y: 100, Width: 400, Height: 300}
	// ppu 2, border 2: the 2-unit border plus a quarter cell (2 units).
	grip := ResizeHitGrip(true, cell, 2, 2, 2)
	if grip.X != 4 {
		t.Fatalf("grip = %v, want 4 across (border + a quarter cell)", grip.X)
	}

	// Inside the zone: resizes.
	for _, dx := range []core.Unit{0, 1, 2, 3} {
		if edge := ResizeEdgeAt(bounds, bounds.X+dx, bounds.Y+150, cell, grip, ResizeGrip{}); edge == ResizeEdgeNone {
			t.Errorf("x+%v: no resize edge, want the left grip", dx)
		}
	}
	// Past it: content, a quarter cell beyond the border — not the
	// five-eighths of a cell that three stacked errors used to produce.
	if edge := ResizeEdgeAt(bounds, bounds.X+4, bounds.Y+150, cell, grip, ResizeGrip{}); edge != ResizeEdgeNone {
		t.Errorf("x+4 (border + a quarter cell): edge bits %d, want content", edge)
	}
}

// The affordance is a different quantity from the grab zone, with the
// opposite structure: the band covers the WHOLE border plus half a column
// beyond it, where the grab zone counts the border toward its width. Stated
// here because the asymmetry looks like an inconsistency to anyone tidying
// up, and collapsing it either blinds the affordance or swallows the content.
func TestOverlayGripCoversTheBorderPlusHalfACell(t *testing.T) {
	cell := core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}
	for _, border := range []core.Unit{0, 2, 20} {
		got := ResizeOverlayGrip(true, cell, border, border)
		if want := border + 4; got.X != want || got.Y != want {
			t.Errorf("border %v: ResizeOverlayGrip = %v, want %v across and down", border, got, want)
		}
	}

	// The cell frame keeps its own affordance: the whole border row/column,
	// which ResizeEdgeRects derives from the metrics when the grip is zero.
	if got := ResizeOverlayGrip(false, cell, 2, 2); !got.Zero() {
		t.Errorf("cell frame: ResizeOverlayGrip = %v, want zero (metrics defaults)", got)
	}

	// ...and it is wider than what actually grabs, at every ordinary border.
	for _, border := range []core.Unit{0, 2} {
		overlay := ResizeOverlayGrip(true, cell, border, border)
		hit := ResizeHitGrip(true, cell, 1, border, border)
		if overlay.X <= hit.X || overlay.Y <= hit.Y {
			t.Errorf("border %v: affordance %v is not wider than the grab zone %v", border, overlay, hit)
		}
	}
}

// A resize band is a physical thickness, so the top and bottom bands are as
// deep as the left and right ones are wide — at every denomination, not only
// where a unit happens to be square.
//
// Both grips counted half a column out of the LOCAL denomination and spent
// that number on both axes, so the band along the top of an MDI child came
// out twice as thick as the one down its side wherever a cell stopped being
// twice as tall as it is wide: 12 device pixels against 6 at a square 16x16
// denomination, 24 against 6 at 16x8, and 3 against 6 the other way at 8x32.
//
// Stated as an exchange rather than as the formula: the two counts are the
// same distance when they buy the same number of units in the surface's own
// denomination. Recomputing half a column here would agree with any formula,
// including the one that was wrong.
func TestResizeGripsAreTheSameThicknessOnBothAxes(t *testing.T) {
	d := core.DefaultCellMetrics()
	// The frame border is already per-axis, so it arrives as a pair; these
	// are what core.FindFrameBorderUnitsIn answers for a 2-unit border at
	// the default denomination.
	for _, c := range []struct {
		m                core.CellMetrics
		borderX, borderY core.Unit
	}{
		{core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}, 2, 2},
		{core.CellMetrics{UnitsPerCellWidth: 16, UnitsPerCellHeight: 32}, 4, 4},
		{core.CellMetrics{UnitsPerCellWidth: 4, UnitsPerCellHeight: 8}, 1, 1},
		{core.CellMetrics{UnitsPerCellWidth: 16, UnitsPerCellHeight: 16}, 4, 2},
		{core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 8}, 2, 1},
		{core.CellMetrics{UnitsPerCellWidth: 16, UnitsPerCellHeight: 8}, 4, 1},
		{core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 32}, 2, 4},
	} {
		for _, g := range []struct {
			name string
			grip ResizeGrip
		}{
			{"affordance", ResizeOverlayGrip(true, c.m, c.borderX, c.borderY)},
			{"grab zone", ResizeHitGrip(true, c.m, 1, c.borderX, c.borderY)},
		} {
			across := core.ExchangeX(g.grip.X, c.m, d)
			down := core.ExchangeY(g.grip.Y, c.m, d)
			if across != down {
				t.Errorf("%dx%d %s: %v units across is %v default units, %v down is %v",
					c.m.UnitsPerCellWidth, c.m.UnitsPerCellHeight, g.name,
					g.grip.X, across, g.grip.Y, down)
			}
		}
	}
}
