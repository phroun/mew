package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The grab rule, stated as the number you actually feel: how far in from a
// window's outer edge a press resizes instead of reaching content.
//
// Nothing asserted this before, which is how three separate errors stacked
// unnoticed into a zone of five-eighths of a cell — the device scale counted
// twice (units already carry it through ppu), a "3 device pixel" floor that
// was really 3 UNITS and so beat the quarter cell outright, and the frame
// border added on top of a zone measured from the outer edge, where the
// border already sat.
func TestResizeHitGripIsAQuarterCellIncludingTheBorder(t *testing.T) {
	cell := core.CellMetrics{CellWidth: 8, CellHeight: 16}
	const anyGraphicalGrip core.Unit = 1 // only says "graphical", not how wide

	for _, c := range []struct {
		name   string
		ppu    float64
		border core.Unit
		want   core.Unit
	}{
		// At ppu 1 the whole cell is only 8 device pixels, so a quarter of it
		// is 2px and the 3-pixel minimum takes over. The border is inside the
		// zone either way, never added to it.
		{"tiny cell: 3px minimum wins", 1, 2, 3},
		{"tiny cell, no border", 1, 0, 3},
		// Zoomed in, a quarter cell clears 3 device pixels and takes over. It
		// is unchanged in UNITS because units already grow with the zoom;
		// counting the device scale again is what made this half a cell and
		// then three quarters.
		{"ppu 2: quarter cell wins", 2, 2, 2},
		{"ppu 4: quarter cell, unchanged", 4, 2, 2},
		// A border wider than the rule stays grabbable along its whole width:
		// it is frame, not content, and a dead strip inside the frame would be
		// its own bug. Nothing is infringed beyond it.
		{"fat border swallows the rule", 1, 20, 20},
		// Zoomed OUT far enough that a quarter cell is under three device
		// pixels, the pixel floor takes over — and it is real device pixels,
		// converted through ppu.
		{"tiny ppu: 3 device px floor", 0.25, 0, 12},
	} {
		got := ResizeHitGrip(anyGraphicalGrip, cell, c.ppu, c.border)
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
	if got := ResizeHitGrip(0, cell, 1, 2); got != 0 {
		t.Errorf("cell frame: ResizeHitGrip = %v, want 0 (metrics defaults apply)", got)
	}
}

// End to end: with the rule in force, content is clickable from the first
// pixel past the border rather than most of a cell inside the window.
func TestContentIsClickableJustPastTheBorder(t *testing.T) {
	cell := core.CellMetrics{CellWidth: 8, CellHeight: 16}
	bounds := core.UnitRect{X: 100, Y: 100, Width: 400, Height: 300}
	// ppu 2: a quarter cell (2 units) clears the 3-device-pixel minimum, and
	// the 2-unit border sits inside it rather than adding to it.
	grip := ResizeHitGrip(1, cell, 2, 2)
	if grip != 2 {
		t.Fatalf("grip = %v, want 2 (a quarter cell, border included)", grip)
	}

	// Inside the zone: resizes.
	for _, dx := range []core.Unit{0, 1} {
		if edge := ResizeEdgeAt(bounds, bounds.X+dx, bounds.Y+150, cell, grip); edge == ResizeEdgeNone {
			t.Errorf("x+%v: no resize edge, want the left grip", dx)
		}
	}
	// Past it: content, from a quarter cell in — not the five-eighths that
	// three stacked errors used to produce.
	if edge := ResizeEdgeAt(bounds, bounds.X+2, bounds.Y+150, cell, grip); edge != ResizeEdgeNone {
		t.Errorf("x+2 (a quarter cell): edge bits %d, want content", edge)
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
		if got, want := ResizeOverlayGrip(1, cell, border), border+4; got != want {
			t.Errorf("border %v: ResizeOverlayGrip = %v, want %v", border, got, want)
		}
	}

	// The cell frame keeps its own affordance: the whole border row/column,
	// which ResizeEdgeRects derives from the metrics when the grip is zero.
	if got := ResizeOverlayGrip(0, cell, 2); got != 0 {
		t.Errorf("cell frame: ResizeOverlayGrip = %v, want 0 (metrics defaults)", got)
	}

	// ...and it is wider than what actually grabs, at every ordinary border.
	for _, border := range []core.Unit{0, 2} {
		overlay := ResizeOverlayGrip(1, cell, border)
		hit := ResizeHitGrip(1, cell, 1, border)
		if overlay <= hit {
			t.Errorf("border %v: affordance %v is not wider than the grab zone %v", border, overlay, hit)
		}
	}
}
