package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/objects/window"
)

type recPC struct {
	metrics core.CellMetrics
	screen  core.UnitRect
	popup   *core.PopupRequest
}

func (r *recPC) RegisterPopup(req *core.PopupRequest) { r.popup = req }
func (r *recPC) UnregisterPopup(string)               {}
func (r *recPC) ScreenBounds() core.UnitRect          { return r.screen }
func (r *recPC) ScreenCellMetrics() core.CellMetrics  { return r.metrics }
func (r *recPC) MapToScreen(t core.Trinket, p core.UnitPoint) core.UnitPoint {
	return window.MapTrinketToScreen(t, p)
}

// A combobox dropdown must open at the combobox's on-screen bottom edge.
// font_size (here 18pt) scales pixels-per-unit but leaves the root
// denomination at 8x16, so the popup mapping must land the dropdown flush
// against its control - in units - regardless of font_size.
func TestComboPopupOpensAtControlBottom(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.New(1280, 900)
	if err != nil {
		t.Fatal(err)
	}
	b.SetFontSize(18)
	m := b.Metrics() // stays 8x16 under font_size
	d := NewDesktop()
	d.SetBackend(b)
	d.SetFont(&core.Font{Name: "ui-text", Size: 12})
	d.SetBounds(core.UnitRect{Width: 1280, Height: 900})
	d.WindowManager().SetScreenBounds(core.UnitRect{Width: 1280, Height: 900})

	win := window.NewWindow("W")
	panel := NewPanel()
	panel.SetLayoutManager(layout.NewBoxLayout(core.Vertical))
	combo := NewComboBox()
	combo.AddItems([]string{"First item", "Second item", "Third item", "Fourth item"})
	panel.AddChild(combo)
	win.SetContent(panel)
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 4 * m.CellWidth, Y: 2 * m.CellHeight, Width: 50 * m.CellWidth, Height: 20 * m.CellHeight})
	win.Layout()

	rec := &recPC{metrics: m, screen: core.UnitRect{Width: 1280, Height: 900}}
	combo.SetPopupController(rec)
	combo.ShowPopup()

	if rec.popup == nil {
		t.Fatal("no popup registered")
	}
	// The combobox's true on-screen top and bottom.
	top := window.MapTrinketToScreen(combo, core.UnitPoint{X: 0, Y: 0})
	wantBottom := window.MapTrinketToScreen(combo, core.UnitPoint{X: 0, Y: m.CellHeight})

	// It must map inside the window, not rescaled toward the origin: the
	// combobox sits at the window's left + the client-area offset (frame
	// border) + its own indent, one titlebar row down.
	wantX := win.Bounds().X + win.ClientAreaOffset().X + combo.Bounds().X
	wantY := win.Bounds().Y + win.ClientAreaOffset().Y + combo.Bounds().Y
	if top.X != wantX || top.Y != wantY {
		t.Errorf("combo mapped to %+v, want {%d %d} (window+offset+indent)", top, wantX, wantY)
	}
	// The popup opens flush against the control's bottom edge.
	if got := rec.popup.Bounds; got.X != wantBottom.X || got.Y != wantBottom.Y {
		t.Errorf("popup at %+v, want top-left %+v (control bottom)", got, wantBottom)
	}
	// Four items, one cell row each - not doubled.
	if want := 4 * m.CellHeight; rec.popup.Bounds.Height != want {
		t.Errorf("popup height = %d, want %d (4 rows)", rec.popup.Bounds.Height, want)
	}
}
