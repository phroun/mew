package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A window that states how far it grows states it for every way of growing
// it. Maximizing already asked -- an edge drag has to ask the same question,
// or the one size the window said it would never take is the one a pointer
// reaches first.
func TestTornEdgeDragStopsAtTheStatedMaximum(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 200, Height: 100}, x: 500, y: 300}
	gx, gy := 700, 380
	win := NewWindow("bounded")
	win.SetMaximumSize(core.UnitSize{Width: 320, Height: 200})
	h := NewTearOffHost(win, surf, ppu1, func() (int, int) { return gx, gy }, nil)

	// Right edge, dragged far past the maximum.
	h.Event(core.MousePressEvent{X: 197, Y: 50, Button: core.LeftButton})
	gx = 1100
	h.Event(core.MouseMoveEvent{X: 597, Y: 50, Buttons: core.LeftButton})
	if surf.size.Width != 320 {
		t.Errorf("width %d, want it held at the stated maximum 320", surf.size.Width)
	}
	h.Event(core.MouseReleaseEvent{X: 597, Y: 50, Button: core.LeftButton})

	// Left edge: the width stops too, and the right edge stays where it was.
	right := surf.x + int(surf.size.Width)
	gx, gy = 500, 380
	h.Event(core.MousePressEvent{X: 1, Y: 50, Button: core.LeftButton})
	gx = 100
	h.Event(core.MouseMoveEvent{X: -399, Y: 50, Buttons: core.LeftButton})
	if surf.size.Width != 320 {
		t.Errorf("width %d dragging the left edge, want 320", surf.size.Width)
	}
	if got := surf.x + int(surf.size.Width); got != right {
		t.Errorf("right edge moved to %d, want it anchored at %d", got, right)
	}
	h.Event(core.MouseReleaseEvent{X: -399, Y: 50, Button: core.LeftButton})

	// Bottom edge, against the maximum height.
	gx, gy = 660, 400
	h.Event(core.MousePressEvent{X: 160, Y: 99, Button: core.LeftButton})
	gy = 800
	h.Event(core.MouseMoveEvent{X: 160, Y: 499, Buttons: core.LeftButton})
	if surf.size.Height != 200 {
		t.Errorf("height %d, want it held at the stated maximum 200", surf.size.Height)
	}
}

// The floor still wins where a window's two limits cross, and a stated
// minimum raises the host's own 3x2-cell floor.
func TestTornEdgeDragHoldsTheStatedMinimum(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 300, Height: 200}, x: 500, y: 300}
	gx, gy := 800, 400
	win := NewWindow("bounded")
	win.SetMinimumSize(core.UnitSize{Width: 240, Height: 160})
	h := NewTearOffHost(win, surf, ppu1, func() (int, int) { return gx, gy }, nil)

	h.Event(core.MousePressEvent{X: 297, Y: 100, Button: core.LeftButton})
	gx = 500
	h.Event(core.MouseMoveEvent{X: -3, Y: 100, Buttons: core.LeftButton})
	if surf.size.Width != 240 {
		t.Errorf("width %d, want it held at the stated minimum 240", surf.size.Width)
	}
}
