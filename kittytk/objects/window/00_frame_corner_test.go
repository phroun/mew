package window

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Nothing the window draws inside itself may damage its own frame. The
// window's own status bar is a rectangle that reaches the bottom edge, so
// at a ROUNDED corner it used to paint straight over the curve — the
// corner went square and the frame stroke vanished there — because the
// chrome built its painter from the raw painter (no rounded clip) and
// painted AFTER the border was re-stroked.
//
// Everything inside the outline now shares one rounded clip region, and
// the frame goes on last, the same order that makes the desktop's own
// corners survive its chrome.
func TestWindowChromeDoesNotDamageTheRoundedCorner(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(400, 300)
	if err != nil {
		t.Fatal(err)
	}
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 400, Height: 300})
	m.SetDesktop(&graphicalDesktop{})

	w := NewWindow("w")
	w.SetDetached(true) // a detached window carries its own status bar
	w.SetWindowStatusBar(&fillBar{fill: style.RGB(255, 0, 255)})
	m.AddWindow(w)
	w.SetBounds(core.UnitRect{X: 40, Y: 40, Width: 200, Height: 160})
	w.Layout()
	m.Paint(core.NewPainter(px))

	img := px.Image()
	isFill := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		// The status bar's magenta, allowing for edge antialiasing.
		return r>>8 > 200 && g>>8 < 60 && b>>8 > 200
	}
	// The outermost columns of the two rows closest to the bottom edge are
	// the corner's curve. The status bar must not have reached them.
	for _, dy := range []int{1, 2} {
		y := 40 + 160 - dy
		for dx := 0; dx < 3; dx++ {
			if isFill(40+dx, y) {
				t.Errorf("status bar painted into the rounded corner at (+%d, bottom-%d)", dx, dy)
			}
		}
	}
	// ...and it IS painted where it belongs, well inside the frame.
	if !isFill(40+20, 40+160-5) {
		t.Error("status bar did not paint inside the window at all — test is not measuring it")
	}
}

// fillBar is a status-bar stand-in that floods its whole rect, so any
// spill past the frame is unmistakable in the raster.
type fillBar struct {
	core.TrinketBase
	fill style.Color
}

func (b *fillBar) Paint(p *core.Painter) {
	bounds := b.Bounds()
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ',
		style.CellStyle{Bg: b.fill, Fg: style.RGB(255, 255, 255)})
}
