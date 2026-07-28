package raster_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

func TestDrawImageCompositesWithClip(t *testing.T) {
	b, err := raster.New(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	b.Clear(style.DefaultStyle().WithBg(style.Color(256 + 0x000000)))

	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			switch {
			case y < 3: // opaque red
				src.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
			case y < 6: // 50% green (premultiplied)
				src.SetRGBA(x, y, color.RGBA{0, 128, 0, 128})
			default: // transparent
				src.SetRGBA(x, y, color.RGBA{0, 0, 0, 0})
			}
		}
	}

	p := core.NewPainter(b).WithOffset(10, 10)
	if !p.DrawImage(2, 2, src) {
		t.Fatal("raster backend must support DrawImage")
	}

	img := b.Image()
	// Opaque rows land verbatim at screen (12,12).
	if got := img.RGBAAt(13, 12); got != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("opaque pixel = %v", got)
	}
	// Half-alpha green over black blends to ~half green.
	if got := img.RGBAAt(13, 16); got.G < 100 || got.G > 160 || got.R != 0 {
		t.Errorf("blended pixel = %v", got)
	}
	// Transparent rows leave the canvas untouched.
	if got := img.RGBAAt(13, 19); got.R != 0 || got.G != 0 {
		t.Errorf("transparent row painted: %v", got)
	}

	// Clip confines the blit.
	b.SetClip(core.UnitRect{X: 0, Y: 0, Width: 5, Height: 5})
	b.DrawImage(40, 40, src)
	if got := img.RGBAAt(41, 41); got.R != 0 {
		t.Errorf("DrawImage ignored clip: %v", got)
	}
}
