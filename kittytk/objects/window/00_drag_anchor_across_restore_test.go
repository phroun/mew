package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Dragging a maximized window's title bar restores it under the pointer. The
// grab has to be re-expressed against the frame the person is holding, which
// on a capped window sits in the middle of the room it holds -- measured from
// the room's corner instead, it carries the frame's own inset and the window
// lands with the pointer nowhere near the title bar it grabbed.
func TestTheGrabSurvivesARestoreUnderThePointer(t *testing.T) {
	m := NewWindowManager()
	m.SetSmoothPositioning(true)
	m.SetScreenBounds(core.UnitRect{Width: 1200, Height: 800})

	win := NewWindow("bounded")
	win.SetMaximumSize(core.UnitSize{Width: 480, Height: 320})
	win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 280, Height: 160})
	m.AddWindow(win)
	m.MaximizeWindow(win)

	room := m.ClientArea()
	fr := win.FrameRect()
	if fr.X == 0 && fr.Y == 0 {
		t.Fatal("the frame is not inset; this test proves nothing")
	}

	// Grab the middle of the frame's title bar and drag down into the room.
	grab := core.UnitPoint{X: room.X + fr.X + fr.Width/2, Y: room.Y + fr.Y + 4}
	m.HandleMousePress(core.MousePressEvent{X: grab.X, Y: grab.Y, Button: core.LeftButton})
	drop := core.UnitPoint{X: grab.X, Y: grab.Y + 200}
	m.HandleMouseMove(core.MouseMoveEvent{X: drop.X, Y: drop.Y, Buttons: core.LeftButton})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: drop.X, Y: drop.Y, Button: core.LeftButton})

	if win.IsMaximized() {
		t.Fatal("dragging the title bar down did not restore the window")
	}
	b := win.Bounds()
	if b.Width != 280 || b.Height != 160 {
		t.Fatalf("it restored to %v, want its own 280x160", b.Size())
	}

	// The pointer is still on the title bar it grabbed...
	local := core.UnitPoint{X: drop.X - b.X, Y: drop.Y - b.Y}
	if local.Y < 0 || local.Y >= win.titleBarMetrics().RowH+8 {
		t.Errorf("the pointer is %d down the restored window, past its title bar", local.Y)
	}
	// ...and proportionally across it: the middle stays the middle.
	if want := b.Width / 2; local.X < want-8 || local.X > want+8 {
		t.Errorf("the pointer is %d across a %d-wide window, want about the middle (%d)",
			local.X, b.Width, want)
	}
}

// The same gesture on a window hosted as its own OS window, where zooming is
// what maximizing means.
func TestTheGrabSurvivesAZoomedTornWindowsRestore(t *testing.T) {
	// The fake's work area is 1600x970 at 0,30.
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 400, Height: 240}, x: 100, y: 100}
	win := NewWindow("bounded")
	win.SetMaximumSize(core.UnitSize{Width: 480, Height: 320})
	gx, gy := 0, 0
	h := NewTearOffHost(win, surf, ppu1, func() (int, int) { return gx, gy }, nil)

	h.zoomToWorkArea()
	if !h.zoomed {
		t.Fatal("the host did not zoom")
	}
	fr := win.FrameRect()
	if fr.X == 0 && fr.Y == 0 {
		t.Fatal("the zoomed frame is not inset; this test proves nothing")
	}

	// Grab the middle of the frame's title bar, in window units.
	h.grabX, h.grabY = fr.X+fr.Width/2, fr.Y+4
	h.dragging = true
	// Drag the pointer well below the work area's top, which restores it.
	gx, gy = 800, 600
	h.dragMove()

	if h.zoomed || win.IsMaximized() {
		t.Fatal("dragging the title down did not restore the zoomed window")
	}
	if h.grabY < 0 || h.grabY >= win.titleBarMetrics().RowH+8 {
		t.Errorf("the grab is %d down the restored window, past its title bar", h.grabY)
	}
	b := win.Bounds()
	if want := b.Width / 2; h.grabX < want-16 || h.grabX > want+16 {
		t.Errorf("the grab is %d across a %d-wide window, want about the middle (%d)",
			h.grabX, b.Width, want)
	}
}
