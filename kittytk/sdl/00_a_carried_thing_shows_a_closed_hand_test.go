package sdl

// The grab cursor is a picture, since SDL offers none: 32 by 32 at one pixel
// to the point, exactly as drawn, and each pixel doubled for a denser display.

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"testing"
)

func px(pix []byte, w, x, y int) [4]byte {
	i := (y*w + x) * 4
	return [4]byte{pix[i], pix[i+1], pix[i+2], pix[i+3]}
}

func TestTheGrabHandIsThePictureAsDrawn(t *testing.T) {
	pix, w, h, ok := grabHandPixels(1)
	if !ok || w != 32 || h != 32 || len(pix) != w*h*4 {
		t.Fatalf("scale 1 is %dx%d (ok %v), want the 32x32 picture", w, h, ok)
	}
	img, err := png.Decode(bytes.NewReader(grabHandPNG))
	if err != nil {
		t.Fatal(err)
	}
	want := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(want, want.Bounds(), img, img.Bounds().Min, draw.Src)
	if !bytes.Equal(pix, want.Pix) {
		t.Error("scale 1 is not the picture pixel for pixel")
	}
	if px(pix, w, 0, 0)[3] != 0 {
		t.Error("the corner of the picture is not clear")
	}
	if px(pix, w, grabHandHot, grabHandHot)[3] != 0xff {
		t.Error("the hot spot, the picture's center, is not on the hand")
	}
}

func TestTheGrabHandDoublesSharply(t *testing.T) {
	one, w1, h1, _ := grabHandPixels(1)
	two, w2, h2, ok := grabHandPixels(2)
	if !ok || w2 != 2*w1 || h2 != 2*h1 || len(two) != w2*h2*4 {
		t.Fatalf("scale 2 is %dx%d, want %dx%d", w2, h2, 2*w1, 2*h1)
	}
	for y := 0; y < h1; y++ {
		for x := 0; x < w1; x++ {
			want := px(one, w1, x, y)
			for _, d := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
				if got := px(two, w2, 2*x+d[0], 2*y+d[1]); got != want {
					t.Fatalf("scale 2 at (%d,%d) is %v, want %v", 2*x+d[0], 2*y+d[1], got, want)
				}
			}
		}
	}
}
