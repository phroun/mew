package raster_test

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// The draw-call clip early-reject must be exact: a glyph run fully outside the
// clip paints nothing, but one inside the clip still renders. (Skipping only
// zero-work draws - output is unchanged.)
func TestClippedTextEarlyRejectIsExact(t *testing.T) {
	b, err := raster.New(200, 60)
	if err != nil {
		t.Fatal(err)
	}
	bg := style.DefaultStyle().WithBg(style.Color(256 + 0x000000))
	fg := style.DefaultStyle().WithFg(style.Color(256 + 0xFFFFFF))

	changed := func() bool {
		img := b.Image()
		for y := 0; y < 60; y++ {
			for x := 0; x < 200; x++ {
				if c := img.RGBAAt(x, y); c.R != 0 || c.G != 0 || c.B != 0 {
					return true
				}
			}
		}
		return false
	}

	// Text drawn far outside a small left-edge clip: nothing lands.
	b.Clear(bg)
	b.SetClip(core.UnitRect{X: 0, Y: 0, Width: 20, Height: 60})
	b.DrawText(120, 20, "far away", fg, nil)
	if changed() {
		t.Error("text outside the clip painted through the early-reject")
	}

	// Same text inside a clip that covers it: it renders.
	b.SetClip(core.UnitRect{X: 100, Y: 0, Width: 100, Height: 60})
	b.Clear(bg)
	b.DrawText(120, 20, "far away", fg, nil)
	if !changed() {
		t.Error("text inside the clip did not render")
	}
}
