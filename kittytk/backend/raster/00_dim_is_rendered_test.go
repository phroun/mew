package raster

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// StyleDim reaches the glass on a pixel surface.
//
// It is an instruction a terminal carries out for itself, sent as SGR 2.
// There is no attribute to send here, only colours to choose, and nothing
// read the bit -- so everything that asked for dim on this surface drew at
// full strength: the desktop's fill, an inactive window title, a button's
// shadow, a menu's shortcut column.
func TestDimIsRenderedRatherThanIgnored(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := NewScaled(200, 60, 1)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)
	f := &core.Font{Name: "ui-text", Size: 12}

	// The darkest ink a run lays, which for a solid colour on white is the
	// colour itself.
	inkOf := func(st style.CellStyle) int {
		b.Clear(style.DefaultStyle().WithBg(style.RGB(255, 255, 255)))
		core.NewPainter(b).DrawText(0, 0, "Shortcut", st, f)
		img := b.Image()
		d := 255 * 3
		for x := 0; x < 120; x++ {
			for y := 0; y < 20; y++ {
				c := img.RGBAAt(x, y)
				if v := int(c.R) + int(c.G) + int(c.B); v < d {
					d = v
				}
			}
		}
		return d
	}

	plain := style.DefaultStyle().WithFg(style.RGB(0, 0, 0)).WithBg(style.RGB(255, 255, 255))
	full := inkOf(plain)
	dim := inkOf(plain.WithAttrs(style.StyleDim))

	if dim <= full {
		t.Errorf("dim ink is %d against plain %d; the attribute is being ignored", dim, full)
	}
	// Three fifths of the ink over the background: 102 a channel for black on
	// white. Written out rather than read from style.DimIntensity, so the
	// expectation does not move with the code.
	if want := 102 * 3; dim < want-30 || dim > want+30 {
		t.Errorf("dim ink is %d, want about %d (three fifths of black over white)", dim, want)
	}

	// And it dims the INK, not whatever ends up behind it: reversed, the
	// dimming still lands on the colour that was the foreground.
	rev := inkOf(plain.WithAttrs(style.StyleReverse))
	revDim := inkOf(plain.WithAttrs(style.StyleReverse | style.StyleDim))
	if rev == revDim {
		t.Error("dim has no effect under reverse; it should still let the ink down")
	}
}
