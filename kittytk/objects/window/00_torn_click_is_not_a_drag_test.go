package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A click on a zoomed torn-off window's title bar leaves it zoomed.
//
// The window's top edge is the work area's top already, so where it would sit
// if the grab were honoured is at or below that from the moment it is grabbed.
// Anything that reads as motion then brings it down, however little the
// pointer went -- and a click carries motion.
func TestAClickOnAZoomedTornTitleBarIsNotADrag(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}, x: 500, y: 300}
	gx, gy := 0, 0
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, ppu1, func() (int, int) { return gx, gy }, nil)

	h.ToggleZoom()
	if !win.IsMaximized() {
		t.Fatal("the window did not zoom")
	}

	gx, gy = 800, 200
	h.Event(core.MousePressEvent{X: 800, Y: 8, Button: core.LeftButton})
	// The pointer where it was pressed, and then a pixel of hand.
	h.Event(core.MouseMoveEvent{X: 800, Y: 8, Buttons: core.LeftButton})
	if !win.IsMaximized() {
		t.Fatal("a motion of nothing brought the window down")
	}
	gy = 201
	h.Event(core.MouseMoveEvent{X: 800, Y: 9, Buttons: core.LeftButton})
	if !win.IsMaximized() {
		t.Error("a pixel of travel brought the window down")
	}
	h.Event(core.MouseReleaseEvent{X: 800, Y: 9, Button: core.LeftButton})
}
