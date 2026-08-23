package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// On pixel surfaces the tab strip's edge is one continuous hairline in
// the bar's text color: along the bar's content edge, around the
// selected tab's arcs, and across the tab's outer edge (paintTabShape).
func TestTabStripGraphicalEdgeLine(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(px)

	tw := NewTabTrinket()
	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		tw.AddTab(name, NewLabel(name))
	}
	tw.SetCurrentIndex(1)
	tw.SetBounds(core.UnitRect{Width: 400, Height: 96})
	tw.Paint(core.NewPainter(px))

	img := px.Image()
	// The bar's content edge line runs along the strip's bottom row,
	// starting at the far left; the bar background sits above it.
	lineC := img.At(2, 15)
	bgC := img.At(2, 4)
	if lineC == bgC {
		t.Fatalf("no edge line at strip bottom-left: %v matches the bar background", lineC)
	}
	// The same line crosses the strip's top row over the selected tab.
	found := false
	for x := 0; x < 400; x++ {
		if img.At(x, 0) == lineC {
			found = true
			break
		}
	}
	if !found {
		t.Error("no edge line along the strip's top row over the selected tab")
	}
}
