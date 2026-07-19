package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Starting an edge resize must anchor to the OS window's true pixel size,
// not re-derive it from the surface's unit size through px(). At a fractional
// pixels-per-unit the unit size snaps to whole cells at a rate slightly above
// ppu, so px(units) undershoots the real pixel width - and a zero-delta first
// move would then jump the window smaller by roughly the frame width.
func TestTearOffResizeAnchorsToPixelSize(t *testing.T) {
	// The OS window is 214x108 px, but its unit size floored to 100x50 (as
	// happens at a fractional ppu). px(units) at ppu=2 would give 200x100 -
	// short of the real pixels.
	surf := &nativeFakeSurface{
		size: core.UnitSize{Width: 100, Height: 50},
		pxW:  214, pxH: 108,
		x: 500, y: 300,
	}
	gx, gy := 700, 380
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, 2, func() (int, int) { return gx, gy }, nil)

	// Press within the right-edge grip, then a zero-delta move (pointer
	// unmoved). The window must not change size.
	h.Event(core.MousePressEvent{X: 98, Y: 25, Button: core.LeftButton})
	h.Event(core.MouseMoveEvent{X: 98, Y: 25, Buttons: core.LeftButton})

	if surf.pxW != 214 || surf.pxH != 108 {
		t.Errorf("starting a resize changed the window from 214x108 to %dx%d px (should be unchanged)",
			surf.pxW, surf.pxH)
	}
	h.Event(core.MouseReleaseEvent{X: 98, Y: 25, Button: core.LeftButton})
}
