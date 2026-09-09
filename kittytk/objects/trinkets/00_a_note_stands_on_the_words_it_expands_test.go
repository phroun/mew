package trinkets

// Where a note is put against the text it expands.

import (
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
)

// A note reads on IN PLACE. What has to line up is the note's TEXT and the
// text it expands -- not the note's box and the text's corner, which puts the
// words a padding down and to the right of where they were.
func TestANoteLinesUpWithTheWordsItExpands(t *testing.T) {
	mm := MenuMetricsFor(core.DefaultCellMetrics(), core.DefaultFont(), true)
	padX, padY := tooltipPadding(mm)
	metrics := core.DefaultCellMetrics()
	screen := core.UnitRect{Width: 2000, Height: 2000}

	// One row of text, somewhere with room all around it.
	anchor := core.UnitRect{X: 400, Y: 400, Width: 200, Height: metrics.UnitsPerCellHeight}
	box := core.UnitRect{Width: 300, Height: mm.RowH + padY*2}

	for _, side := range []struct {
		name string
		side core.TooltipSide
	}{
		{"auto", core.TooltipAuto},
		{"over", core.TooltipOver},
	} {
		x, y := tooltipOrigin(side.side, anchor, box, screen, metrics, padX, padY)

		// The note's own text begins one padding in from its box.
		if textX := x + padX; textX != anchor.X {
			t.Errorf("%s: the note's text begins at %d, the text it expands at %d",
				side.name, textX, anchor.X)
		}
		// And sits on the same row: the box is centred on the anchor, so the
		// two rows share a middle however tall the note is.
		if mid, want := y+box.Height/2, anchor.Y+anchor.Height/2; mid != want {
			t.Errorf("%s: the note's middle is at %d, the text's at %d", side.name, mid, want)
		}
	}
}

// Below is still below, for a trinket that asks for it.
func TestBelowIsStillUnderTheText(t *testing.T) {
	mm := MenuMetricsFor(core.DefaultCellMetrics(), core.DefaultFont(), true)
	padX, padY := tooltipPadding(mm)
	metrics := core.DefaultCellMetrics()
	screen := core.UnitRect{Width: 2000, Height: 2000}
	anchor := core.UnitRect{X: 400, Y: 400, Width: 200, Height: metrics.UnitsPerCellHeight}
	box := core.UnitRect{Width: 300, Height: mm.RowH + padY*2}

	_, y := tooltipOrigin(core.TooltipBelow, anchor, box, screen, metrics, padX, padY)
	if y <= anchor.Y+anchor.Height {
		t.Errorf("a note asked for below sits at %d, not under the text ending at %d",
			y, anchor.Y+anchor.Height)
	}
}

// Near an edge a note gives up its preference rather than half of itself --
// the same compromise a context menu makes.
func TestANoteNearAnEdgeComesBackOnScreen(t *testing.T) {
	mm := MenuMetricsFor(core.DefaultCellMetrics(), core.DefaultFont(), true)
	padX, padY := tooltipPadding(mm)
	metrics := core.DefaultCellMetrics()
	screen := core.UnitRect{Width: 400, Height: 400}
	box := core.UnitRect{Width: 300, Height: mm.RowH + padY*2}

	// Hard against the right edge, and against the left.
	anchor := core.UnitRect{X: 380, Y: 200, Width: 20, Height: metrics.UnitsPerCellHeight}
	x, _ := tooltipOrigin(core.TooltipAuto, anchor, box, screen, metrics, padX, padY)
	if x+box.Width > screen.Width {
		t.Errorf("the note runs to %d, past the screen's %d", x+box.Width, screen.Width)
	}
	anchor.X = 0
	x, _ = tooltipOrigin(core.TooltipAuto, anchor, box, screen, metrics, padX, padY)
	if x < 0 {
		t.Errorf("the note starts at %d, off the left of the screen", x)
	}

	// And against the bottom.
	anchor = core.UnitRect{X: 100, Y: 396, Width: 20, Height: metrics.UnitsPerCellHeight}
	_, y := tooltipOrigin(core.TooltipAuto, anchor, box, screen, metrics, padX, padY)
	if y+box.Height > screen.Height {
		t.Errorf("the note reaches %d, past the screen's %d", y+box.Height, screen.Height)
	}
}

// A note wide enough to wrap fits the surface it will be DRAWN on, which is
// not always the desktop: a window torn out onto a surface of its own carries
// its own popup layer, and a note wrapped to the whole screen runs off the
// edge of that window.
func TestAWrappedNoteFitsTheSurfaceItIsDrawnOn(t *testing.T) {
	d, _, label := onScreen(t)
	d.tooltipDwell = time.Nanosecond
	d.tooltipFade = time.Nanosecond
	d.tooltipShownAt = time.Now()

	// The desktop stays wide; the layer the note goes to is narrow.
	narrow := &ownLayer{screen: core.UnitRect{Width: 240, Height: 600}}
	label.SetPopupController(narrow)

	long := "Denomination is how many units make one character cell, eight wide " +
		"by sixteen tall to begin with, and setting it on this window re-expresses " +
		"every unit inside it."
	d.ShowTooltip(core.TooltipRequest{Text: long, From: label})
	d.ProcessTimers()

	if narrow.got == nil {
		t.Fatal("no note reached the window's own layer")
	}
	box := narrow.got.Bounds
	if box.Width > narrow.screen.Width {
		t.Errorf("the note is %d wide on a surface %d wide", box.Width, narrow.screen.Width)
	}
	if box.X < 0 || box.X+box.Width > narrow.screen.Width {
		t.Errorf("the note occupies %d..%d, past the surface's %d",
			box.X, box.X+box.Width, narrow.screen.Width)
	}
	if len(d.tooltip.lines) < 2 {
		t.Fatalf("the note did not wrap at all: %q", d.tooltip.lines)
	}
}

// The note's box is its widest line plus its padding, so a line allowed to
// fill the whole surface makes a box wider than the surface. What a line may
// take is the room LESS the padding -- whatever the words happen to do.
func TestTheWrapWidthLeavesRoomForTheNotesOwnPadding(t *testing.T) {
	d := NewDesktop()
	mm := d.tooltipFace()
	padX, _ := tooltipPadding(mm)

	for _, w := range []core.Unit{80, 160, 240, 500, 900} {
		surface := core.UnitRect{Width: w, Height: 600}
		line := d.tooltipWrapWidth(mm, surface)
		if box := line + padX*2; box > w {
			t.Errorf("on a surface %d wide a full line makes a box %d wide", w, box)
		}
	}
}
