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
	cell := core.CellMetrics{CellWidth: 8, CellHeight: 16}

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
		got := ResizeHitGrip(true, cell, c.ppu, c.border)
		if got != c.want {
			t.Errorf("%s: ResizeHitGrip(ppu=%v, border=%v) = %v units, want %v",
				c.name, c.ppu, c.border, got, c.want)
		}
	}
}

// The cell frame is untouched by any of this: there the whole border
// row/column IS the grip, and ResizeEdgeAt's metrics defaults apply.
func TestResizeHitGripLeavesTheCellFrameAlone(t *testing.T) {
	cell := core.CellMetrics{CellWidth: 8, CellHeight: 16}
	if got := ResizeHitGrip(false, cell, 1, 2); got != 0 {
		t.Errorf("cell frame: ResizeHitGrip = %v, want 0 (metrics defaults apply)", got)
	}
}

// End to end: with the rule in force, content is clickable from the first
// pixel past the border rather than most of a cell inside the window.
func TestContentIsClickableJustPastTheBorder(t *testing.T) {
	cell := core.CellMetrics{CellWidth: 8, CellHeight: 16}
	bounds := core.UnitRect{X: 100, Y: 100, Width: 400, Height: 300}
	// ppu 2, border 2: the 2-unit border plus a quarter cell (2 units).
	grip := ResizeHitGrip(true, cell, 2, 2)
	if grip != 4 {
		t.Fatalf("grip = %v, want 4 (border + a quarter cell)", grip)
	}

	// Inside the zone: resizes.
	for _, dx := range []core.Unit{0, 1, 2, 3} {
		if edge := ResizeEdgeAt(bounds, bounds.X+dx, bounds.Y+150, cell, grip, 0); edge == ResizeEdgeNone {
			t.Errorf("x+%v: no resize edge, want the left grip", dx)
		}
	}
	// Past it: content, a quarter cell beyond the border — not the
	// five-eighths of a cell that three stacked errors used to produce.
	if edge := ResizeEdgeAt(bounds, bounds.X+4, bounds.Y+150, cell, grip, 0); edge != ResizeEdgeNone {
		t.Errorf("x+4 (border + a quarter cell): edge bits %d, want content", edge)
	}
}

// The affordance is a different quantity from the grab zone, with the
// opposite structure: the band covers the WHOLE border plus half a column
// beyond it, where the grab zone counts the border toward its width. Stated
// here because the asymmetry looks like an inconsistency to anyone tidying
// up, and collapsing it either blinds the affordance or swallows the content.
func TestOverlayGripCoversTheBorderPlusHalfACell(t *testing.T) {
	cell := core.CellMetrics{CellWidth: 8, CellHeight: 16}
	for _, border := range []core.Unit{0, 2, 20} {
		if got, want := ResizeOverlayGrip(true, cell, border), border+4; got != want {
			t.Errorf("border %v: ResizeOverlayGrip = %v, want %v", border, got, want)
		}
	}

	// The cell frame keeps its own affordance: the whole border row/column,
	// which ResizeEdgeRects derives from the metrics when the grip is zero.
	if got := ResizeOverlayGrip(false, cell, 2); got != 0 {
		t.Errorf("cell frame: ResizeOverlayGrip = %v, want 0 (metrics defaults)", got)
	}

	// ...and it is wider than what actually grabs, at every ordinary border.
	for _, border := range []core.Unit{0, 2} {
		overlay := ResizeOverlayGrip(true, cell, border)
		hit := ResizeHitGrip(true, cell, 1, border)
		if overlay <= hit {
			t.Errorf("border %v: affordance %v is not wider than the grab zone %v", border, overlay, hit)
		}
	}
}
