package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// A window laid out before joining the manager (the protocol builder
// sets bounds first) used cell-frame insets; AddWindow re-lays it out
// under its real ancestry so graphical edge-to-edge content applies
// without a resize wiggle.
func TestAddWindowRelayoutsUnderGraphicalFrames(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	win := window.NewWindow("built early")
	panel := NewPanel()
	win.SetContent(panel)
	// Bounds set before the window has any ancestry: laid out with
	// cell-frame insets (border column on each side).
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 400, Height: 200})
	if got := panel.Bounds().Width; got != 400-16 {
		t.Fatalf("harness: pre-add content width = %d, want 384 (cell frame)", got)
	}

	d.WindowManager().AddWindow(win)
	// After adoption the window re-lays out under graphical frames: the
	// titlebar reserves the top and the frame border (2 units at scale 1)
	// the sides, so content is the window width minus both side borders.
	if got, want := panel.Bounds().Width, core.Unit(400-2*2); got != want {
		t.Errorf("post-add content width = %d, want %d (graphical, border reserved)", got, want)
	}
}
