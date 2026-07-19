package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// Splitter dividers snap to whole rows/columns on cell surfaces and
// track the ratio at unit granularity on pixel surfaces.
func TestSplitterSmoothOnPixelSurfaces(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	build := func(d *Desktop) *Splitter {
		sp := NewSplitter(core.Horizontal)
		sp.AddChild(NewPanel())
		sp.AddChild(NewPanel())
		win := window.NewWindow("host")
		win.SetContent(sp)
		d.WindowManager().AddWindow(win)
		win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 400, Height: 200})
		win.Layout()
		// A ratio that does not land on a cell boundary:
		// totalWidth = contentWidth - 8; first = total * 0.37.
		sp.SetPosition(0.37)
		return sp
	}

	// Cell surface: divider X is a multiple of the cell width.
	cellDesk := NewDesktop()
	cellDesk.SetBackend(&nullBackend{})
	cellSp := build(cellDesk)
	if x := cellSp.dividerBounds().X; x%8 != 0 {
		t.Errorf("cell surface divider at %d; want cell-aligned", x)
	}

	// Pixel surface: divider sits at the exact ratio position.
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	pixDesk := NewDesktop()
	pixDesk.SetBackend(px)
	pixSp := build(pixDesk)
	got := pixSp.dividerBounds().X
	want := core.Unit(float64(pixSp.Bounds().Width-8) * 0.37)
	if got != want {
		t.Errorf("pixel surface divider at %d; want unsnapped %d", got, want)
	}
	if got%8 == 0 {
		t.Errorf("pixel surface divider landed cell-aligned (%d) for a non-aligned ratio", got)
	}
}

// Splitters inside MDI child windows inherit smooth positioning: the
// pane stamps its children (the WindowManager only stamps its own).
func TestMDIChildWindowCarriesSmoothPositioning(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	pane := NewMDIPane()
	host := window.NewWindow("host")
	host.SetContent(pane)
	d.WindowManager().AddWindow(host)

	child := window.NewWindow("mdi child")
	pane.AddWindow(child)
	if !child.SmoothWindowPositioning() {
		t.Error("MDI child window not stamped with smooth positioning")
	}

	sp := NewSplitter(core.Horizontal)
	child.SetContent(sp)
	if !core.FindSmoothPositioning(sp.Self()) {
		t.Error("splitter inside MDI child does not see smooth positioning")
	}
}
