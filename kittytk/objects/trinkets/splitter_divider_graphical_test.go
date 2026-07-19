package trinkets

import (
	"fmt"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// On pixel surfaces the horizontal divider band of a vertical
// splitter is one column thick (the scrollbar dimension); on cell
// surfaces it stays a full row.
func TestVerticalSplitterDividerThinOnPixelSurfaces(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	build := func(d *Desktop) *Splitter {
		sp := NewSplitter(core.Vertical)
		sp.AddChild(NewPanel())
		sp.AddChild(NewPanel())
		win := window.NewWindow("host")
		win.SetContent(sp)
		d.WindowManager().AddWindow(win)
		win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 400, Height: 200})
		win.Layout()
		return sp
	}

	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)
	if got := build(d).dividerBounds().Height; got != 8 {
		t.Errorf("pixel surface divider band %d units thick, want 8", got)
	}

	dc := NewDesktop()
	dc.SetBackend(&nullBackend{})
	if got := build(dc).dividerBounds().Height; got != 16 {
		t.Errorf("cell surface divider band %d units thick, want 16", got)
	}
}

// The combobox popup scrollbar thumb follows the pointer at unit
// granularity while scrollOffset snaps to whole items.
func TestComboBoxPopupSmoothScrollbarDrag(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	cb := NewComboBox()
	for i := 0; i < 30; i++ {
		cb.AddItem(fmt.Sprintf("item %d", i))
	}
	win := window.NewWindow("host")
	win.SetContent(cb)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 300, Height: 200})
	win.Layout()

	cb.clickMode = true
	visible := cb.effectiveMaxVisible()
	if visible >= len(cb.items) {
		t.Fatalf("harness: %d visible of %d items - no scrollbar", visible, len(cb.items))
	}
	metrics := cb.screenMetrics()
	popupBounds := core.UnitRect{
		X:      0,
		Y:      0,
		Width:  200,
		Height: core.Unit(visible) * metrics.CellHeight,
	}
	trackU, thumbU, posU := cb.popupScrollbarUnits(visible)
	scrollable := trackU - thumbU
	if scrollable <= 0 {
		t.Fatal("harness: no scrollable track")
	}

	// Grab 3 units into the thumb.
	grabY := popupBounds.Y + core.Unit(posU) + 3
	if !cb.handlePopupMousePress(core.MousePressEvent{
		X: popupBounds.X + popupBounds.Width - 2, Y: grabY, Button: core.LeftButton,
	}, popupBounds) {
		t.Fatal("press on popup thumb not consumed")
	}
	if !cb.scrollbarDragging || !cb.sbSmoothDrag {
		t.Fatal("press on popup thumb did not start a smooth drag")
	}

	// Drag 25 units down: the thumb tracks exactly, the offset snaps.
	cb.handlePopupMouseMove(core.MouseMoveEvent{
		X: popupBounds.X + popupBounds.Width - 2, Y: grabY + 25,
	}, popupBounds)
	wantPos := posU + 25
	if wantPos > scrollable {
		wantPos = scrollable
	}
	if cb.sbThumbPos != wantPos {
		t.Errorf("smooth thumb pos = %v, want %v", cb.sbThumbPos, wantPos)
	}
	maxScroll := len(cb.items) - visible
	wantOffset := int(wantPos*float64(maxScroll)/scrollable + 0.5)
	if cb.scrollOffset != wantOffset {
		t.Errorf("scroll offset = %d, want snapped %d", cb.scrollOffset, wantOffset)
	}

	cb.handlePopupMouseRelease(core.MouseReleaseEvent{
		X: popupBounds.X + popupBounds.Width - 2, Y: grabY + 25, Button: core.LeftButton,
	}, popupBounds)
	if cb.scrollbarDragging || cb.sbSmoothDrag {
		t.Error("release did not end the popup scrollbar drag")
	}
}

// The divider band's thickness is expressed in the Y denomination:
// re-denominating a window's interior (32-unit rows) scales it with
// the rows instead of leaving an X-denominated constant behind.
func TestDividerThicknessFollowsRowDenomination(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	sp := NewSplitter(core.Vertical)
	sp.AddChild(NewPanel())
	sp.AddChild(NewPanel())
	win := window.NewWindow("host")
	win.SetContent(sp)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 400, Height: 200})
	win.Layout()

	if got := sp.dividerBounds().Height; got != 8 {
		t.Errorf("standard metrics: divider %d units, want 8", got)
	}
	win.SetCellMetrics(&core.CellMetrics{CellWidth: 8, CellHeight: 32})
	win.Layout()
	if got := sp.dividerBounds().Height; got != 16 {
		t.Errorf("32-unit rows: divider %d units, want 16 (half a row)", got)
	}
	win.SetCellMetrics(nil)
	win.Layout()
	if got := sp.dividerBounds().Height; got != 8 {
		t.Errorf("after round-trip: divider %d units, want 8", got)
	}
}
