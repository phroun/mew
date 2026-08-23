package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// The client-area contract per frame mode: cell frames reserve a full
// cell on every side; graphical frames reserve the titlebar row at the
// top and the frame border (which rests OUTSIDE the content) on the left,
// right, and bottom. The default border is 2px; on this scale-1 backend
// that is 2 units.
func TestClientAreaContractPerFrameMode(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	newWin := func(d *Desktop) *window.Window {
		win := window.NewWindow("client")
		win.SetBounds(core.UnitRect{X: 32, Y: 32, Width: 240, Height: 160})
		d.WindowManager().AddWindow(win)
		return win
	}

	// Cell backend: one-cell border on every side.
	cellDesk := NewDesktop()
	cellDesk.SetBackend(&nullBackend{})
	cellWin := newWin(cellDesk)
	if off := cellWin.ClientAreaOffset(); off.X != 8 || off.Y != 16 {
		t.Errorf("cell frame client offset = %+v, want (8,16)", off)
	}

	// Pixel backend: titlebar reserved at top, frame border reserved on
	// the sides and bottom (2 units at scale 1).
	const border = 2
	pixel, err := raster.New(640, 480)
	if err != nil {
		t.Fatal(err)
	}
	pixDesk := NewDesktop()
	pixDesk.SetBackend(pixel)
	pixWin := newWin(pixDesk)
	// Top reserves the border AND the titlebar row; sides/bottom the border.
	if off := pixWin.ClientAreaOffset(); off.X != border || off.Y != border+16 {
		t.Errorf("graphical frame client offset = %+v, want (%d,%d)", off, border, border+16)
	}

	// Content spans the window minus the reserved border and titlebar.
	content := NewPanel()
	pixWin.SetContent(content)
	pixWin.Layout()
	if got, want := content.Bounds().Width, core.Unit(240-2*border); got != want {
		t.Errorf("graphical content width = %d, want %d (window - 2*border)", got, want)
	}
	if got, want := content.Bounds().Height, core.Unit(160-16-2*border); got != want {
		t.Errorf("graphical content height = %d, want %d (window - titlebar - 2*border)", got, want)
	}
}

// A window squeezed below its chrome still exposes a 1-unit client
// sliver: content paints clipped instead of spilling.
func TestClientAreaNeverEmpty(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	pixel, err := raster.New(640, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(pixel)

	win := window.NewWindow("tiny")
	content := NewPanel()
	win.SetContent(content)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 100, Height: 16}) // titlebar only
	win.Layout()

	if h := content.Bounds().Height; h < 1 {
		t.Errorf("client height = %d; must be clamped to >= 1", h)
	}
	if w := content.Bounds().Width; w < 1 {
		t.Errorf("client width = %d; must be clamped to >= 1", w)
	}
}
