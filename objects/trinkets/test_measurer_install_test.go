package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// Measurement comes from the render target: SetBackend with a pixel
// backend installs its shaping engine as the process measurer, and a
// cell backend restores the text-mode cell arithmetic.
func TestSetBackendInstallsTextMeasurer(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	f := core.FontUIText12
	cellWidth := f.MeasureText("iii")
	if cellWidth != 3*8 {
		t.Fatalf("text-mode measurement = %d, want 24", cellWidth)
	}

	pixel, err := raster.New(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(pixel)

	if f.MeasureText("iii") >= f.MeasureText("MMM") {
		t.Error("pixel backend: ui-text should measure proportionally")
	}

	// A cell backend restores the cell arithmetic.
	d2 := NewDesktop()
	d2.SetBackend(&nullBackend{})
	if got := f.MeasureText("iii"); got != cellWidth {
		t.Errorf("cell backend: measurement = %d, want %d", got, cellWidth)
	}
}
