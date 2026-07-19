package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// The desktop derives the graphical resize grip: a quarter of a
// layout column, floored at 4 device pixels; zero on cell frames.
func TestDesktopResizeGripDerivation(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	// Scale 2: quarter-column (2 units) x scale 2 = 4 units = 8 px.
	px2, err := raster.NewScaled(640, 480, 2)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px2)
	if got := d.GraphicalResizeGrip(); got != 4 {
		t.Errorf("scale 2 grip = %d units, want 4 (8 device px)", got)
	}

	// Scale 1: quarter-column x 1 = 2 units = 2 px < 4 px floor -> 4 units.
	px1, err := raster.New(640, 480)
	if err != nil {
		t.Fatal(err)
	}
	d1 := NewDesktop()
	d1.SetBackend(px1)
	if got := d1.GraphicalResizeGrip(); got != 4 {
		t.Errorf("scale 1 grip = %d units, want 4 (4px floor)", got)
	}

	// Cell backend: zero (the whole border cell is the grip there).
	dc := NewDesktop()
	dc.SetBackend(&nullBackend{})
	if got := dc.GraphicalResizeGrip(); got != 0 {
		t.Errorf("cell frame grip = %d, want 0", got)
	}
}

// MDI panes inherit the grip through their ancestry.
func TestMDIPaneInheritsResizeGrip(t *testing.T) {
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
	if got := core.FindResizeGrip(pane.Self()); got != 4 {
		t.Errorf("MDI pane grip = %d, want 4 (inherited from desktop)", got)
	}
}
