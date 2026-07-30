package raster_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/style"
)

// A coverage mask tinted with a color must produce the standard over-composite
// out = tint*cov + dst*(255-cov), matching a pre-colored glyph - so recoloring
// a cached grayscale mask is pixel-identical to baking the color in.
func TestCompositeMaskTint(t *testing.T) {
	b, err := raster.New(4, 1)
	if err != nil {
		t.Fatal(err)
	}
	b.Clear(style.DefaultStyle().WithBg(style.Color(256 + 0x0000FF))) // blue bg

	// White premultiplied coverage mask: alpha = coverage.
	mask := image.NewRGBA(image.Rect(0, 0, 4, 1))
	covs := []uint8{0, 128, 255, 64}
	for i, c := range covs {
		mask.SetRGBA(i, 0, color.RGBA{c, c, c, c})
	}
	b.DrawImageMaskTintPx(0, 0, mask, 255, 0, 0) // tint red

	over := func(fg, dst uint8, cov uint32) uint8 {
		return uint8((uint32(fg)*cov + uint32(dst)*(255-cov)) / 255)
	}
	for i, c := range covs {
		cov := uint32(c)
		want := color.RGBA{over(255, 0, cov), over(0, 0, cov), over(0, 255, cov), 255}
		if got := b.Image().RGBAAt(i, 0); got != want {
			t.Errorf("cov=%d: got %v, want %v", c, got, want)
		}
	}
}
