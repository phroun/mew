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
	if at.Height <= 0 || at.Width != win.Bounds().Width {
		t.Errorf("it named %+v rather than the title band", at)
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
