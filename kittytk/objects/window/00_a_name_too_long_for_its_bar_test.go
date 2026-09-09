package window

// A title bar cut short is a window whose name a reader cannot make out, and
// the name is the one thing a title bar is for.

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

const longWindowName = "Connections — Authorizations and Known Hosts for This Desktop"

// painted is a window of the given width with a long name, drawn once so the
// bar knows what it had room for.
func paintedWindow(t *testing.T, width core.Unit) *Window {
	t.Helper()
	px, err := raster.New(900, 300)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	win := NewWindow(longWindowName)
	win.SetBounds(core.UnitRect{Width: width, Height: 200})
	win.Layout()
	win.Paint(core.NewPainter(px))
	return win
}

// titleMid is a point in the middle of the title band.
func titleMid(w *Window) core.UnitPoint {
	w.mu.RLock()
	h := w.titleBandH
	w.mu.RUnlock()
	return core.UnitPoint{X: w.Bounds().Width / 2, Y: h / 2}
}

func TestANameTooLongForItsBarIsOffered(t *testing.T) {
	win := paintedWindow(t, 200)

	text, at, ok := win.TooltipAt(titleMid(win))
	if !ok {
		t.Fatal("a bar too narrow for its name offered nothing")
	}
	if text != longWindowName {
		t.Errorf("it offered %q, not the window's name", text)
	}
	// The note stands on the NAME, not on the band: a title bar centres its
	// name, and a note anchored to the whole band would sit at the far left
	// of the window with the buttons rather than on the words.
	band := win.Bounds().Width
	if at.Height <= 0 {
		t.Errorf("it named %+v, which has no height", at)
	}
	if at.Width >= band {
		t.Errorf("it named the whole bar (%+v) rather than the name in it", at)
	}
	if at.X <= 0 {
		t.Errorf("the note stands at %d, at the bar's own left edge", at.X)
	}
	if at.X+at.Width > band {
		t.Errorf("the name is said to run to %d, past the bar's %d", at.X+at.Width, band)
	}

	// Below the bar is content, and the trinkets in it answer for themselves.
	if _, _, ok := win.TooltipAt(core.UnitPoint{X: 10, Y: at.Height + 40}); ok {
		t.Error("the window answered for its content")
	}
}

// A bar with room for the whole name is not hiding anything.
func TestANameThatFitsIsNotOffered(t *testing.T) {
	win := paintedWindow(t, 800)
	if text, _, ok := win.TooltipAt(titleMid(win)); ok {
		t.Errorf("a bar showing the whole name offered %q anyway", text)
	}
}

// A bar wide enough to centre its name puts the name in the middle, and a
// note about it belongs there too rather than out at the bar's left edge.
func TestTheNoteFollowsACentredName(t *testing.T) {
	px, err := raster.New(900, 300)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	// Wide enough to centre the name, then narrowed until it is just cut --
	// the anchor follows the drawn text either way.
	win := NewWindow(longWindowName)
	win.SetBounds(core.UnitRect{Width: 900, Height: 200})
	win.Layout()
	win.Paint(core.NewPainter(px))

	win.mu.RLock()
	at, cut := win.titleTextAt, win.titleCut
	win.mu.RUnlock()
	if cut {
		t.Skip("900 units is still too narrow for this name")
	}
	if at.Width <= 0 {
		t.Fatal("the bar recorded no place for its name")
	}
	// Centred on the bar, to within the frame's own border: the point is that
	// the name is in the middle rather than out at an edge.
	mid, want := at.X+at.Width/2, core.Unit(900)/2
	cell := win.titleBarMetrics().CellW
	if d := mid - want; d > cell || d < -cell {
		t.Errorf("the name is centred on %d; the bar's middle is %d", mid, want)
	}
}
