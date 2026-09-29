package sdl

// The hand cursors are pictures, since SDL offers none: 32 by 32 at one pixel
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

var hands = map[string][]byte{"open": openHandPNG, "closed": grabHandPNG}

func TestEachHandIsItsPictureAsDrawn(t *testing.T) {
	for name, picture := range hands {
		pix, w, h, ok := cursorPixels(picture, 1)
		if !ok || w != 32 || h != 32 || len(pix) != w*h*4 {
			t.Fatalf("%s: scale 1 is %dx%d (ok %v), want the 32x32 picture", name, w, h, ok)
		}
		img, err := png.Decode(bytes.NewReader(picture))
		if err != nil {
			t.Fatal(err)
		}
		want := image.NewNRGBA(image.Rect(0, 0, 32, 32))
		draw.Draw(want, want.Bounds(), img, img.Bounds().Min, draw.Src)
		if !bytes.Equal(pix, want.Pix) {
			t.Errorf("%s: scale 1 is not the picture pixel for pixel", name)
		}
		if px(pix, w, 0, 0)[3] != 0 {
			t.Errorf("%s: the corner of the picture is not clear", name)
		}
		if px(pix, w, handHot, handHot)[3] != 0xff {
			t.Errorf("%s: the hot spot, the picture's center, is not on the hand", name)
		}
	}
	if bytes.Equal(openHandPNG, grabHandPNG) {
		t.Error("the open and closed hands are the same picture")
	}
}

func TestEachHandDoublesSharply(t *testing.T) {
	for name, picture := range hands {
		one, w1, h1, _ := cursorPixels(picture, 1)
		two, w2, h2, ok := cursorPixels(picture, 2)
		if !ok || w2 != 2*w1 || h2 != 2*h1 || len(two) != w2*h2*4 {
			t.Fatalf("%s: scale 2 is %dx%d, want %dx%d", name, w2, h2, 2*w1, 2*h1)
		}
		for y := 0; y < h1; y++ {
			for x := 0; x < w1; x++ {
				want := px(one, w1, x, y)
				for _, d := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
					if got := px(two, w2, 2*x+d[0], 2*y+d[1]); got != want {
						t.Fatalf("%s: scale 2 at (%d,%d) is %v, want %v", name, 2*x+d[0], 2*y+d[1], got, want)
					}
				}
			}
		}
	}
}
