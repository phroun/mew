package sdl

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"image/png"
)

// The hands SDL has no system cursors for, as pictures: 32 by 32 at one pixel
// to the point, each the one file to replace to change it. The open hand is
// over something a press would pick up (core.CursorGrab), the closed one while
// it is carried (core.CursorGrabbing). The two share a wrist, so the fingers
// are all that move between them.
var (
	//go:embed hand_cursor.png
	openHandPNG []byte
	//go:embed grab_hand.png
	grabHandPNG []byte
)

// handHot is the pointer's position in either picture at one pixel to the
// point: its exact center. Twice that at two.
const handHot = 16

// cursorPixels is a picture as RGBA bytes at scale pixels to the point, with
// its width and height; ok is false if it will not decode. Every pixel of the
// drawing becomes a scale-by-scale block of exactly its colour: nothing is
// smoothed, so the drawing stays as sharp as it was drawn.
func cursorPixels(picture []byte, scale int) (pix []byte, w, h int, ok bool) {
	img, err := png.Decode(bytes.NewReader(picture))
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
