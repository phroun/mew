package sdl

// The grab cursor is drawn from a mask, since SDL offers none: every row the
// same width, its hot spot on the palm, and the picture the same at each
// scale it is built at.

import "testing"

func TestTheGrabHandMaskIsSquare(t *testing.T) {
	for i, row := range grabHandMask {
		if len(row) != len(grabHandMask) {
			t.Errorf("row %d is %d wide, want %d", i, len(row), len(grabHandMask))
		}
	}
	if grabHandMask[grabHandHotY][grabHandHotX] != 'o' {
		t.Error("the hot spot is not on the palm")
	}
}

func TestTheGrabHandScales(t *testing.T) {
	one, w1, h1 := grabHandPixels(1)
	two, w2, h2 := grabHandPixels(2)
	if w2 != 2*w1 || h2 != 2*h1 || len(two) != w2*h2*4 || len(one) != w1*h1*4 {
		t.Fatalf("sizes %dx%d and %dx%d", w1, h1, w2, h2)
	}
	px := func(pix []byte, w, x, y int) [4]byte {
		i := (y*w + x) * 4
		return [4]byte{pix[i], pix[i+1], pix[i+2], pix[i+3]}
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
	// Outline opaque black, fill opaque white, the rest clear.
	for y, row := range grabHandMask {
		for x, c := range row {
			got := px(one, w1, x, y)
			var want [4]byte
			switch c {
			case 'X':
				want = [4]byte{0, 0, 0, 0xff}
			case 'o':
				want = [4]byte{0xff, 0xff, 0xff, 0xff}
			}
			if got != want {
				t.Fatalf("(%d,%d) %q is %v, want %v", x, y, c, got, want)
			}
		}
	}
}
