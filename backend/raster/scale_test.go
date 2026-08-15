package raster_test

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// At scale 2 the same unit-space drawing covers twice the pixels: one
// 8x16-unit cell paints a 16x32 pixel block, and glyphs rasterize at
// the doubled point size rather than being upsampled.
func TestScaledBackendDoublesPixelCoverage(t *testing.T) {
	b, err := raster.NewScaled(64, 64, 2)
	if err != nil {
		t.Fatal(err)
	}
	if b.Scale() != 2 {
		t.Fatalf("Scale() = %d", b.Scale())
	}
	// Size reports units, not pixels.
	if s := b.Size(); s.Width != 32 || s.Height != 32 {
		t.Fatalf("Size() = %+v, want 32x32 units", s)
	}

	b.Clear(style.DefaultStyle())
	st := style.DefaultStyle()
	st.Bg = style.Color(256 + 0xFF0000) // pure red background
	b.DrawCell(0, 0, ' ', st)

	img := b.Image()
	red := func(x, y int) bool {
		c := img.RGBAAt(x, y)
		return c.R == 255 && c.B == 0
	}
	// The cell's background must reach past the scale-1 extent (8x16 px)
	// out to the scale-2 extent (16x32 px)...
	if !red(12, 24) {
		t.Errorf("pixel (12,24) not cell background; cell did not scale")
	}
	if !red(15, 31) {
		t.Errorf("pixel (15,31) not cell background; cell did not scale")
	}
	// ...and stop there.
	if red(17, 2) || red(2, 33) {
		t.Errorf("cell background leaked past 16x32 px")
	}

	// The glyph itself must have been drawn (some non-background pixels
	// inside the cell).
	b.DrawCell(0, 0, 'X', st)
	glyph := 0
	for y := 0; y < 32; y++ {
		for x := 0; x < 16; x++ {
			if !red(x, y) {
				glyph++
			}
		}
	}
	if glyph < 20 {
		t.Errorf("glyph coverage %d px; 'X' at 20pt should be larger", glyph)
	}
}

// Scale 1 behavior is unchanged: New == NewScaled(w, h, 1).
func TestScaleOneIsIdentity(t *testing.T) {
	b, err := raster.New(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	if b.Scale() != 1 {
		t.Fatalf("Scale() = %d", b.Scale())
	}
	if s := b.Size(); s.Width != 64 || s.Height != 64 {
		t.Fatalf("Size() = %+v, want 64x64 units", s)
	}

	b.Clear(style.DefaultStyle())
	st := style.DefaultStyle()
	st.Bg = style.Color(256 + 0xFF0000)
	b.FillRect(core.UnitRect{X: 0, Y: 0, Width: 8, Height: 16}, ' ', st)

	img := b.Image()
	if c := img.RGBAAt(7, 15); c.R != 255 {
		t.Errorf("fill missing inside 8x16 rect")
	}
	if c := img.RGBAAt(9, 2); c.R == 255 {
		t.Errorf("fill leaked past 8 px at scale 1")
	}
}
