package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// The child terminal's grid must be a pure function of its rectangle and font —
// never of its own content. Reserving a grid row for the horizontal scrollbar
// (which appears only when a visible line overflows the grid) coupled the row
// count to the content: shrinking the grid changed which lines were visible,
// which flipped the bar, which regrew the grid — a self-sustaining
// resize/redraw loop at a fixed window size. So a horizontal scrollbar being
// active must NOT change the fitted row count; the bar overlays the bottom row
// instead.
func TestGridRowsIndependentOfHorizontalScrollbar(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.New(640, 400)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)

	// A graphical-frame parent so gfxInputActive() (FindGraphicalFrames) is true
	// and the scrollbar-lane reservations run at all.
	stub := &graphicalFrameStub{Panel: NewPanel()}
	term := NewPurfecTerm()
	term.Init(term)
	if term.Terminal() == nil {
		t.Skip("terminal unavailable")
	}
	term.SetEditorMode(false)
	term.SetParent(stub)
	term.SetBounds(core.UnitRect{Width: 640, Height: 400})

	p := core.NewPainter(b)

	// Baseline: no horizontal scroll, so no horizontal bar.
	term.Paint(p)
	_, baseRows := term.Terminal().GetSize()
	if baseRows <= 0 {
		t.Skip("no usable grid in this environment")
	}

	// Force the horizontal scrollbar on (a nonzero horizontal offset makes
	// hScrollActive report true), then repaint at the SAME bounds.
	term.Terminal().Buffer().SetHorizOffset(1)
	if term.Terminal().Buffer().GetHorizOffset() == 0 {
		t.Skip("could not force a horizontal offset")
	}
	term.Paint(p)
	_, rows2 := term.Terminal().GetSize()

	if rows2 != baseRows {
		t.Errorf("grid rows changed when the horizontal scrollbar became active: %d -> %d; "+
			"the fitted grid must not depend on content (see the churn note in paintGraphical)",
			baseRows, rows2)
	}
}
