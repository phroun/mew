package raster_test

import (
	"image/color"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// An empty clip clips EVERYTHING: a window whose client area shrinks
// to nothing must not spill its content across the surface.
func TestEmptyClipPaintsNothing(t *testing.T) {
	b, err := raster.New(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	b.Clear(style.DefaultStyle().WithBg(style.Color(256 + 0x000000)))

	b.SetClip(core.UnitRect{X: 10, Y: 10, Width: 0, Height: 0})
	red := style.DefaultStyle().WithBg(style.Color(256 + 0xFF0000))
	b.FillRect(core.UnitRect{X: 0, Y: 0, Width: 64, Height: 64}, ' ', red)
	b.DrawText(4, 4, "spill", style.DefaultStyle().WithFg(style.Color(256+0xFF0000)), nil)

	img := b.Image()
	for y := 0; y < 64; y += 4 {
		for x := 0; x < 64; x += 4 {
			if c := img.RGBAAt(x, y); c.R != 0 {
				t.Fatalf("painted through an empty clip at (%d,%d): %v", x, y, c)
			}
		}
	}
}

// The rounded clip confines painting to the frame's rounded outline:
// fills reach the edges but never the cut corners.
func TestRoundedClipCutsCorners(t *testing.T) {
	b, err := raster.New(100, 80)
	if err != nil {
		t.Fatal(err)
	}
	b.Clear(style.DefaultStyle().WithBg(style.Color(256 + 0x000000)))

	p := core.NewPainter(b).
		WithRoundedClipRegion(core.UnitRect{X: 10, Y: 10, Width: 80, Height: 60}, 8)
	green := style.DefaultStyle().WithBg(style.Color(256 + 0x00FF00))
	// Content deliberately larger than the clip region.
	p.FillRect(core.UnitRect{X: 0, Y: 0, Width: 100, Height: 80}, ' ', green)

	img := b.Image()
	g := color.RGBA{0, 255, 0, 255}
	// Inside the region: painted, right up to the straight edges.
	for _, pt := range [][2]int{{50, 40}, {10, 40}, {89, 40}, {50, 10}, {50, 69}} {
		if img.RGBAAt(pt[0], pt[1]) != g {
			t.Errorf("(%d,%d) should be painted: %v", pt[0], pt[1], img.RGBAAt(pt[0], pt[1]))
		}
	}
	// The corner pixels are outside the radius-8 arc: untouched.
	for _, pt := range [][2]int{{10, 10}, {89, 10}, {10, 69}, {89, 69}, {0, 0}, {99, 79}} {
		if img.RGBAAt(pt[0], pt[1]) == g {
			t.Errorf("(%d,%d) leaked past the rounded clip", pt[0], pt[1])
		}
	}

	// Clearing the region restores unconstrained painting.
	b.SetRoundedClip(core.UnitRect{}, 0)
	b.SetClip(core.UnitRect{X: 0, Y: 0, Width: 100, Height: 80})
	b.FillRect(core.UnitRect{X: 0, Y: 0, Width: 4, Height: 4}, ' ', green)
	if img.RGBAAt(0, 0) != g {
		t.Error("rounded clip not cleared")
	}
}
