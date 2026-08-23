package raster

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// core.SetWindowFrameBorderPx sets the rounded window-frame border width
// in device pixels; 0 restores the default (2px for a double frame).
// Measured here as the thickness of the top stroke.
func TestFrameStrokePxOverride(t *testing.T) {
	defer core.SetWindowFrameBorderPx(0)

	white := style.DefaultStyle().WithBg(style.RGB(255, 255, 255)).WithFg(style.RGB(255, 255, 255))

	topStrokePx := func(widthPx int) int {
		core.SetWindowFrameBorderPx(widthPx)
		b, err := New(100, 100)
		if err != nil {
			t.Fatal(err)
		}
		b.Clear(style.DefaultStyle().WithBg(style.RGB(0, 0, 0)))
		// A rounded frame filling most of the surface.
		b.StrokeRoundedRect(core.UnitRect{X: 10, Y: 10, Width: 80, Height: 80}, 6, style.BorderDouble, white)
		img := b.Image()
		// Count consecutive white pixels down the top edge at x=50 (past
		// the corner radius, on the straight top band).
		n := 0
		for y := 10; y < 90; y++ {
			c := img.RGBAAt(50, y)
			if c.R > 200 && c.G > 200 && c.B > 200 {
				n++
			} else if n > 0 {
				break
			}
		}
		return n
	}

	if got := topStrokePx(0); got != 2 {
		t.Errorf("default frame stroke = %d px, want 2", got)
	}
	if got := topStrokePx(6); got != 6 {
		t.Errorf("override frame stroke = %d px, want 6", got)
	}
	if got := topStrokePx(1); got != 1 {
		t.Errorf("thin frame stroke = %d px, want 1", got)
	}
}
