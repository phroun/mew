package sdl

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"image/png"
)

// The closed hand shown while something is carried (core.CursorGrabbing).
// SDL has no system cursor for it, so it is this picture: 32 by 32 at one
// pixel to the point, and the one file to replace to change it.
//
//go:embed grab_hand.png
var grabHandPNG []byte

// grabHandHot is the pointer's position in the picture at one pixel to the
// point: its exact center. Twice that at two.
const grabHandHot = 16

// grabHandPixels is the hand as RGBA bytes at scale pixels to the point, with
// its width and height; ok is false if the picture will not decode. Every
// pixel of the drawing becomes a scale-by-scale block of exactly its colour:
// nothing is smoothed, so the drawing stays as sharp as it was drawn.
func grabHandPixels(scale int) (pix []byte, w, h int, ok bool) {
	img, err := png.Decode(bytes.NewReader(grabHandPNG))
	if err != nil || scale < 1 {
		return nil, 0, 0, false
	}
	b := img.Bounds()
	src := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	w, h = b.Dx()*scale, b.Dy()*scale
	pix = make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			copy(pix[(y*w+x)*4:], src.Pix[src.PixOffset(x/scale, y/scale):][:4])
		}
	}
	return pix, w, h, true
}
