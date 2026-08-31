package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// The outer denomination every render below is displayed through: 8 units
// across a cell, 16 down. A device pixel is therefore one unit at 8x16, and
// the interior denomination is what varies.
var denomOuter = core.CellMetrics{CellWidth: 8, CellHeight: 16}

const (
	denomStripCells = 50
	denomSurfaceW   = 460
	denomSurfaceH   = 40
)

// paintDenomStrip renders a three-tab strip whose interior counts units at the
// given denomination onto a fixed 8x16 surface, and returns the backend.
func paintDenomStrip(t *testing.T, interior core.CellMetrics) *raster.Backend {
	t.Helper()
	b, err := raster.New(denomSurfaceW, denomSurfaceH)
	if err != nil {
		t.Fatal(err)
	}
	b.SetCellMetrics(denomOuter)
	b.Clear(style.DefaultStyle())

	tabs := newDenomStrip(interior)
	tabs.Paint(core.NewPainter(b).WithDenomination(denomOuter, interior))
	return b
}

func newDenomStrip(interior core.CellMetrics) *TabTrinket {
	tabs := NewTabTrinket()
	for _, title := range []string{"Alpha", "Beta", "Gamma"} {
		tabs.AddTab(title, nil)
	}
	tabs.SetCurrentIndex(1)
	tabs.SetCellMetrics(&interior)
	tabs.SetBounds(core.UnitRect{
		Width:  denomStripCells * interior.CellWidth,
		Height: interior.CellHeight,
	})
	return tabs
}

// A denomination says how many units make up a cell. A cell is a fixed
// physical size at a given zoom, so changing the denomination changes only the
// currency the layout is counted in. A tab strip states no margin, padding,
// width or height of its own: its prefixes and separators are counted in
// cells, its labels are measured. Nothing it paints should move or resize.
//
// It moved. The cell-counted parts were multiplied by the strip's own
// CellWidth and so followed the denomination, while the labels between them
// went through Font.MeasureText, which answers in default-denomination units.
// At an interior denomination of 16 the labels came back at half the width the
// gaps around them were placed at, and the tabs drifted out of step.
//
// Painted pixels are compared rather than the width arithmetic: the arithmetic
// agreed with itself throughout, which is why clicks kept landing on the tab
// the picture said was elsewhere.
func TestTabStripPaintsTheSameAtEveryDenomination(t *testing.T) {
	base := paintDenomStrip(t, denomOuter).Image()

	for _, interior := range []core.CellMetrics{
		{CellWidth: 16, CellHeight: 32},
		{CellWidth: 32, CellHeight: 64},
		{CellWidth: 4, CellHeight: 8},
	} {
		img := paintDenomStrip(t, interior).Image()

		// The strip's bottom edge is one unit thick, and a coarser
		// denomination spends more pixels on a unit, so the edge is the one
		// thing entitled to a different thickness. Everything above it --
		// every label, every tab shape -- has to land on the same pixels.
		edgePx := 1
		if px := int(denomOuter.CellHeight / interior.CellHeight); px > edgePx {
			edgePx = px
		}
		rows := int(denomOuter.CellHeight) - edgePx

		for y := 0; y < rows; y++ {
			for x := 0; x < denomSurfaceW; x++ {
				if img.RGBAAt(x, y) != base.RGBAAt(x, y) {
					t.Errorf("interior %dx%d: pixel (%d,%d) is %v, want %v (the 8x16 picture)",
						interior.CellWidth, interior.CellHeight, x, y,
						img.RGBAAt(x, y), base.RGBAAt(x, y))
					y = rows // one report per denomination is enough
					break
				}
			}
		}
	}
}

// The other half of the same measurement: a click arrives in the strip's own
// units, and the strip decides which tab it landed on by walking the same
// widths it paints with. Pressing a given physical pixel has to pick the same
// tab whatever denomination the strip counts in.
func TestTabStripPicksTheSameTabAtEveryDenomination(t *testing.T) {
	// Device columns spread across the three tabs, read off the 8x16 render.
	for _, px := range []core.Unit{20, 45, 70, 100, 130, 160, 185} {
		want := -1
		for _, interior := range []core.CellMetrics{
			denomOuter,
			{CellWidth: 16, CellHeight: 32},
			{CellWidth: 32, CellHeight: 64},
			{CellWidth: 4, CellHeight: 8},
		} {
			tabs := newDenomStrip(interior)
			x := core.ExchangeX(px, denomOuter, interior)
			tabs.HandleMousePress(core.MousePressEvent{
				X: x, Y: interior.CellHeight / 2, Button: core.LeftButton,
			})
			got := tabs.CurrentIndex()
			if want == -1 {
				want = got
				continue
			}
			if got != want {
				t.Errorf("px %d: interior %dx%d selected tab %d, want %d (what 8x16 selects)",
					px, interior.CellWidth, interior.CellHeight, got, want)
			}
		}
	}
}
