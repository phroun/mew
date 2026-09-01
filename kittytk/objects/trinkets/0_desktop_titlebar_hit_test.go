package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// The titlebar chrome sits inside the reserved frame border, so its band
// runs [0, border+UnitsPerCellHeight) in window-local coordinates. A press on the
// LOWER part of a titlebar button - the strip below UnitsPerCellHeight but still
// within the border-shifted titlebar - must still register as a button
// click, not leak through to content. Regression: the router gated on
// event.Y < UnitsPerCellHeight and missed the bottom `border` rows of the chrome.
func TestTitlebarButtonHitIncludesBorderOffset(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	pixel, err := raster.New(640, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(pixel)

	win := window.NewWindow("hit")
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 320, Height: 200})

	border := core.FindFrameBorderUnits(win)
	if border <= 0 {
		t.Fatalf("expected a reserved graphical border, got %d", border)
	}
	metrics := core.DefaultCellMetrics()

	// The maximize button is the third control ([x][.][^]); target its
	// horizontal centre, at a row inside the border-shifted band that the
	// old gate (< UnitsPerCellHeight) would have dropped.
	buttonWidth := metrics.TextWidth(3)
	maxCenterX := metrics.UnitsPerCellWidth + buttonWidth*2 + buttonWidth/2 + border
	y := metrics.UnitsPerCellHeight + border/2 // below UnitsPerCellHeight, inside the chrome
	if y <= metrics.UnitsPerCellHeight {
		y = metrics.UnitsPerCellHeight // border/2 rounded to 0; still exercises the boundary
	}

	if win.IsMaximized() {
		t.Fatal("precondition: window should not start maximized")
	}
	win.HandleMousePress(core.MousePressEvent{X: maxCenterX, Y: y, Button: core.LeftButton})
	win.HandleMouseRelease(core.MouseReleaseEvent{X: maxCenterX, Y: y, Button: core.LeftButton})

	if !win.IsMaximized() {
		t.Errorf("press+release on the lower titlebar band (y=%d, band=%d) did not hit the maximize button", y, metrics.UnitsPerCellHeight+border)
	}
}
