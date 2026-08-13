package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// What decides a window's resize-edge geometry is which KIND of frame the
// surface paints, and nothing else. The desktop used to derive a grip WIDTH
// here — a quarter column times the device scale, floored — and pass it down;
// every consumer then compared it to zero and computed its own width from the
// metrics, so the arithmetic was doing nothing but carrying one bit. The bit
// already had a name.
func TestDesktopReportsGraphicalFrames(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	px, err := raster.NewScaled(640, 480, 2)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)
	if !d.GraphicalWindowFrames() {
		t.Error("a pixel surface reports cell frames")
	}

	dc := NewDesktop()
	dc.SetBackend(&nullBackend{})
	if dc.GraphicalWindowFrames() {
		t.Error("a cell surface reports graphical frames")
	}
}

// MDI panes inherit the answer through their ancestry, which is how an
// embedded pane's children get the same edges as the desktop's own windows.
func TestMDIPaneInheritsGraphicalFrames(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.NewScaled(640, 480, 2)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	pane := NewMDIPane()
	win := window.NewWindow("host")
	win.SetContent(pane)
	d.WindowManager().AddWindow(win)
	if !core.FindGraphicalFrames(pane.Self()) {
		t.Error("MDI pane did not inherit graphical frames from the desktop")
	}
}
