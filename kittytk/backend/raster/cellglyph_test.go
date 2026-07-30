package raster

import (
	"testing"

	"github.com/phroun/kittytk/style"
)

// DrawCell (the cell primitive) shapes through the engine, so chrome
// glyphs get the full per-rune fallback chain - the checkmark and
// info glyphs used to rasterize from a bare Go Mono face and come
// out as .notdef boxes.
func TestDrawCellUsesFallbackChain(t *testing.T) {
	b, err := New(120, 20)
	if err != nil {
		t.Fatal(err)
	}
	white := style.DefaultStyle().WithFg(style.Color(256 + 0xFFFFFF))
	for i, ch := range []rune{'✓', 'ℹ', '⚠', '✖', '▸'} {
		b.Clear(style.DefaultStyle())
		b.DrawCell(4, 2, ch, white)
		ink := 0
		img := b.Image()
		for y := 0; y < 20; y++ {
			for x := 0; x < 120; x++ {
				c := img.RGBAAt(x, y)
				if c.R > 150 && c.G > 150 && c.B > 150 {
					ink++
				}
			}
		}
		if ink < 4 {
			t.Errorf("glyph %q (#%d) produced almost no ink (%d px) - fallback not applied?", string(ch), i, ink)
		}
	}
}
