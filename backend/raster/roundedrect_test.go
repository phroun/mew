package raster_test

import (
	"image/color"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
)

func TestDrawRoundedRect(t *testing.T) {
	b, err := raster.New(100, 80)
	if err != nil {
		t.Fatal(err)
	}
	clear := style.DefaultStyle().WithBg(style.Color(256 + 0x000000))
	b.Clear(clear)

	// Red stroke, green fill, double border = 2px stroke, radius 6.
	s := style.DefaultStyle().
		WithFg(style.Color(256 + 0xFF0000)).
		WithBg(style.Color(256 + 0x00FF00))
	rd, ok := interface{}(b).(core.RoundedRectDrawer)
	if !ok {
		t.Fatal("raster backend must implement RoundedRectDrawer")
	}
	rd.DrawRoundedRect(core.UnitRect{X: 10, Y: 10, Width: 80, Height: 60}, 6, style.BorderDouble, s)

	img := b.Image()
	is := func(x, y int, want color.RGBA) bool { return img.RGBAAt(x, y) == want }
	red := color.RGBA{255, 0, 0, 255}
	green := color.RGBA{0, 255, 0, 255}
	black := color.RGBA{0, 0, 0, 255}

	// Straight edges: 2px stroke at top/bottom/left/right midpoints.
	for _, pt := range [][2]int{{50, 10}, {50, 11}, {50, 68}, {50, 69}, {10, 40}, {11, 40}, {88, 40}, {89, 40}} {
		if !is(pt[0], pt[1], red) {
			t.Errorf("stroke missing at (%d,%d): %v", pt[0], pt[1], img.RGBAAt(pt[0], pt[1]))
		}
	}
	// Just inside the stroke: fill.
	for _, pt := range [][2]int{{50, 12}, {50, 67}, {12, 40}, {87, 40}, {50, 40}} {
		if !is(pt[0], pt[1], green) {
			t.Errorf("fill missing at (%d,%d): %v", pt[0], pt[1], img.RGBAAt(pt[0], pt[1]))
		}
	}
	// Corner cut: the pixel at the rect's corner is far outside the
	// radius-6 arc and must remain untouched canvas.
	for _, pt := range [][2]int{{10, 10}, {89, 10}, {10, 69}, {89, 69}} {
		if !is(pt[0], pt[1], black) {
			t.Errorf("corner not cut at (%d,%d): %v", pt[0], pt[1], img.RGBAAt(pt[0], pt[1]))
		}
	}
	// Single border = 1px: second row in is already fill.
	b.Clear(clear)
	rd.DrawRoundedRect(core.UnitRect{X: 10, Y: 10, Width: 80, Height: 60}, 6, style.BorderSingle, s)
	if !is(50, 10, red) || !is(50, 11, green) {
		t.Errorf("single border should be 1px: row10=%v row11=%v", img.RGBAAt(50, 10), img.RGBAAt(50, 11))
	}
}

// The window frame on a pixel surface is one rounded rect: its
// corners are cut (the canvas shows through) instead of square
// box-drawing corners.
func TestWindowFrameHasRoundedCorners(t *testing.T) {
	b, err := raster.New(400, 240)
	if err != nil {
		t.Fatal(err)
	}
	clear := style.DefaultStyle().WithBg(style.Color(256 + 0x101018))
	b.Clear(clear)
	canvas := b.Image().RGBAAt(2, 2)

	win := window.NewWindow("Frame")
	win.SetBounds(core.UnitRect{X: 24, Y: 24, Width: 320, Height: 160})
	win.Layout()
	win.Paint(core.NewPainter(b).WithOffset(24, 24))

	img := b.Image()
	// The extreme window corners lie outside the arc: canvas color.
	// (Bottom corners regression-guard the content-area fill, which
	// once painted square corners over the arcs.)
	for _, pt := range [][2]int{{24, 24}, {24 + 319, 24}, {24, 24 + 159}, {24 + 319, 24 + 159}} {
		if got := img.RGBAAt(pt[0], pt[1]); got != canvas {
			t.Errorf("corner (%d,%d) not rounded: %v", pt[0], pt[1], got)
		}
	}
	// The frame edge is stroked (not canvas, not interior). Sample at
	// x=+100: past the titlebar buttons (which end at 80 units) and
	// left of the centered title text.
	edge := img.RGBAAt(24+100, 24)
	interior := img.RGBAAt(24+100, 24+80)
	if edge == canvas {
		t.Error("top edge not painted")
	}
	if edge == interior {
		t.Error("top edge should be stroke, not fill")
	}
}

// StrokeRoundedRectWeight strokes at an explicit device-pixel weight,
// independent of the border-style weight - used for a single-border
// window's thin inner line.
func TestStrokeRoundedRectWeight(t *testing.T) {
	b, err := raster.New(100, 80)
	if err != nil {
		t.Fatal(err)
	}
	clear := style.DefaultStyle().WithBg(style.Color(256 + 0x000000))
	b.Clear(clear)

	ws, ok := interface{}(b).(core.RoundedRectWeightStroker)
	if !ok {
		t.Fatal("raster backend must implement RoundedRectWeightStroker")
	}
	// 3px red stroke, radius 6, no fill (interior stays black canvas).
	s := style.DefaultStyle().WithFg(style.Color(256 + 0xFF0000))
	ws.StrokeRoundedRectWeight(core.UnitRect{X: 10, Y: 10, Width: 80, Height: 60}, 6, 3, s)

	img := b.Image()
	red := color.RGBA{255, 0, 0, 255}
	black := color.RGBA{0, 0, 0, 255}
	// Top edge midpoint: the 3 outermost rows are stroke, the 4th is canvas.
	for _, y := range []int{10, 11, 12} {
		if img.RGBAAt(50, y) != red {
			t.Errorf("row %d should be stroke: %v", y, img.RGBAAt(50, y))
		}
	}
	if img.RGBAAt(50, 13) != black {
		t.Errorf("row 13 should be interior canvas: %v", img.RGBAAt(50, 13))
	}
	// Center is untouched (stroke-only leaves the interior).
	if img.RGBAAt(50, 40) != black {
		t.Errorf("interior should be untouched: %v", img.RGBAAt(50, 40))
	}
}
