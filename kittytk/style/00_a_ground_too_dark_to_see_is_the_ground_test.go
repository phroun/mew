package style

import "testing"

// A theme's black is rarely the pure one, and it reaches a cell by whichever
// spelling the theme happened to use: an index, a palette entry, or channels.
// What settles whether a blank should draw it as its own ground is not the
// spelling but whether there is enough light in it to see -- below 14% of full,
// weighted for the eye, there is not, and a shade drawn there stands texture
// over the ground the cell already has.
func TestNearBlackReadsEverySpelling(t *testing.T) {
	for _, c := range []struct {
		name  string
		color Color
		dark  bool
	}{
		{"nothing painted", ColorDefault, true},
		{"nor by a transparent", ColorTransparent, true},
		{"the named black", ColorBlack, true},
		{"pure black in channels", RGB(0, 0, 0), true},
		{"a near-black a theme would write", RGB(30, 30, 46), true},
		{"the cube's own black", Color256(16), true},
		{"the greyscale ramp's foot", Color256(232), true},
		{"and its fourth step", Color256(234), true},
		{"its fifth step is over the line", Color256(235), false},
		{"grey 34 is under", RGB(34, 34, 34), true},
		{"grey 36 is over", RGB(36, 36, 36), false},
		{"a mid colour is nowhere near", RGB(10, 120, 200), false},
		{"nor is white", ColorWhite, false},
		{"nor a cube colour", Color256(27), false},
	} {
		if got := c.color.NearBlack(); got != c.dark {
			t.Errorf("%s: %v.NearBlack() = %v, want %v",
				c.name, c.color, got, c.dark)
		}
	}
}

// Bright black is a grey, not the ground: a cell wearing it has something to
// draw, and a blank that gave it up would lose a region the reader can see.
func TestBrightBlackIsAColour(t *testing.T) {
	if ColorBrightBlack.NearBlack() {
		t.Error("bright black was taken for the ground it stands on")
	}
}
