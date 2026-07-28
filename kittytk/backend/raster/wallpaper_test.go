package raster_test

import (
	"image/color"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

func TestFillPatternChunksAndAnchors(t *testing.T) {
	b, err := raster.New(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	st := style.DefaultStyle().
		WithFg(style.Color(256 + 0xFF0000)).
		WithBg(style.Color(256 + 0x0000FF))

	// Checkerboard rows, 4px chunks, painted over a sub-rect NOT at
	// the origin: the pattern must stay origin-anchored.
	pattern := [8]uint8{0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55}
	b.FillPattern(core.UnitRect{X: 6, Y: 6, Width: 40, Height: 40}, pattern, 4, st)

	img := b.Image()
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}

	// Row 0 of the pattern is 10101010 (bit 7 leftmost = set = fg).
	// With 4px chunks, block (x/4, y/4); at y=6 -> block row 1 ->
	// pattern row 1 = 0x55 = 01010101: block col 1 (x 4..7) is SET.
	if got := img.RGBAAt(6, 6); got != red {
		t.Errorf("(6,6) = %v, want fg (origin-anchored block col 1, row 1)", got)
	}
	if got := img.RGBAAt(9, 6); got != blue {
		t.Errorf("(9,6) = %v, want bg (block col 2, row 1)", got)
	}
	// Outside the rect: untouched (black canvas).
	if got := img.RGBAAt(2, 2); got == red || got == blue {
		t.Errorf("pattern leaked outside rect at (2,2): %v", got)
	}
	// Chunk size respected: within one 4px block the color is uniform.
	if img.RGBAAt(8, 8) != img.RGBAAt(11, 11) {
		t.Error("colors vary inside one chunk block")
	}
}

// Transparent-background text composites over what is beneath instead
// of filling an opaque line box.
func TestTransparentTextPreservesBackground(t *testing.T) {
	b, err := raster.New(200, 40)
	if err != nil {
		t.Fatal(err)
	}
	green := style.DefaultStyle().WithBg(style.Color(256 + 0x00FF00))
	b.Clear(green)

	st := style.DefaultStyle().
		WithFg(style.Color(256 + 0xFFFFFF)).
		WithBg(style.ColorTransparent)
	b.DrawText(4, 4, "ghost", st, core.FontUIText12)

	img := b.Image()
	// Between glyphs and in the line box's empty areas the green
	// survives; an opaque draw would have painted the default bg.
	greenPx := 0
	for y := 4; y < 20; y++ {
		for x := 4; x < 44; x++ {
			c := img.RGBAAt(x, y)
			if c.G == 255 && c.R == 0 {
				greenPx++
			}
		}
	}
	if greenPx < 100 {
		t.Errorf("transparent text erased the background beneath (only %d green px left)", greenPx)
	}
	// And the glyphs themselves drew something non-green.
	ink := 0
	for y := 4; y < 20; y++ {
		for x := 4; x < 44; x++ {
			if c := img.RGBAAt(x, y); c.R > 128 {
				ink++
			}
		}
	}
	if ink < 20 {
		t.Errorf("no glyph ink composited (%d px)", ink)
	}
}
