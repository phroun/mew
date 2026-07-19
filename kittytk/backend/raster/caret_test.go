package raster_test

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// The bar caret: one unit wide at the left edge of the glyph box,
// drawn in the color a block cursor would show (the style's
// background), honoring painter offsets and backend scale.
func TestDrawCaretBarPosition(t *testing.T) {
	b, err := raster.NewScaled(64, 64, 2)
	if err != nil {
		t.Fatal(err)
	}
	b.Clear(style.DefaultStyle())

	// Black-on-white block-cursor style: the bar must come out white.
	cur := style.DefaultStyle().WithFg(style.ColorBlack).WithBg(style.Color(256 + 0xFFFFFF))

	p := core.NewPainter(b).WithOffset(8, 4)
	if !p.DrawCaret(8, 0, 16, cur) {
		t.Fatal("raster backend must support DrawCaret")
	}

	// Screen position is offset+local = (16, 4) units = (32, 8) px at
	// scale 2; the bar is 1 unit (2 px) wide and 16 units (32 px) tall.
	img := b.Image()
	white := func(x, y int) bool {
		c := img.RGBAAt(x, y)
		return c.R == 255 && c.G == 255 && c.B == 255
	}
	for _, pt := range [][2]int{{32, 8}, {33, 8}, {32, 39}, {33, 39}} {
		if !white(pt[0], pt[1]) {
			t.Errorf("caret pixel (%d,%d) not drawn", pt[0], pt[1])
		}
	}
	// Left edge means nothing to the left of x=32, and one unit means
	// nothing at x=34; nothing above y=8 or below y=39 either.
	for _, pt := range [][2]int{{31, 8}, {34, 8}, {32, 7}, {32, 40}} {
		if white(pt[0], pt[1]) {
			t.Errorf("caret leaked to (%d,%d)", pt[0], pt[1])
		}
	}
}
