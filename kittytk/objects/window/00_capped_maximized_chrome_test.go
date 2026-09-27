package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A capped maximized window holds the whole room but draws its frame in the
// middle of it. Every hit test on that frame has to be measured from the
// frame: measured from the surface instead, the title bar answers along the
// top of the shade -- yards from the window -- and nowhere near the bar the
// person can see.
func TestACappedMaximizedWindowsChromeIsWhereItIsDrawn(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 1200, Height: 800})
	win := NewWindow("bounded")
	win.SetMaximumSize(core.UnitSize{Width: 480, Height: 320})
	m.AddWindow(win)
	m.MaximizeWindow(win)

	room := m.ClientArea()
	fr := win.FrameRect()
	if fr.X == 0 && fr.Y == 0 {
		t.Fatal("the frame is not inset; this test proves nothing")
	}

	// The title-bar buttons answer at the frame's own top-left corner.
	if got := win.buttonAtWindowPoint(fr.X+4, fr.Y+4); got != TitleButtonClose {
		t.Errorf("the close button at the frame's corner reads %v, want Close", got)
	}
	// And not at the surface's, which is shade.
	if got := win.buttonAtWindowPoint(4, 4); got != TitleButtonNone {
		t.Errorf("a point in the shade reads %v, want no button at all", got)
	}

	// A press on the frame's title bar begins a drag-to-restore gesture; a
	// press on the shade above it is the window's own surface and does not.
	m.HandleMousePress(core.MousePressEvent{
		X: room.X + fr.X + fr.Width/2, Y: room.Y + fr.Y + 4, Button: core.LeftButton})
	m.mu.RLock()
	draggingFromTitle := m.dragging == win
	m.mu.RUnlock()
	m.HandleMouseRelease(core.MouseReleaseEvent{
		X: room.X + fr.X + fr.Width/2, Y: room.Y + fr.Y + 4, Button: core.LeftButton})
	if !draggingFromTitle {
		t.Error("a press on the frame's title bar did not begin a title drag")
	}

	m.HandleMousePress(core.MousePressEvent{
		X: room.X + fr.X + fr.Width/2, Y: room.Y + 4, Button: core.LeftButton})
	m.mu.RLock()
	draggingFromShade := m.dragging == win
	m.mu.RUnlock()
	m.HandleMouseRelease(core.MouseReleaseEvent{
		X: room.X + fr.X + fr.Width/2, Y: room.Y + 4, Button: core.LeftButton})
	if draggingFromShade {
		t.Error("a press on the shade above the frame began a title drag")
	}
}

// The content is measured against the FRAME, not the room the window holds:
// a capped maximized window's interior is the interior of the window you can
// see, and it is reached in the frame's own coordinates -- the one space
// everything inside a window works in.
func TestACappedMaximizedWindowsContentIsSizedToItsFrame(t *testing.T) {
	win := NewWindow("bounded")
	win.SetMaximumSize(core.UnitSize{Width: 480, Height: 320})
	win.SetBounds(core.UnitRect{Width: 1200, Height: 768})
	win.Maximize()

	fr := win.FrameRect()
	if fr.Width != 480 || fr.Height != 320 {
		t.Fatalf("the frame is %v, want its maximum of 480x320", fr.Size())
	}
	cb := win.ContentBounds()
	if cb.X < 0 || cb.Y < 0 || cb.X+cb.Width > fr.Width || cb.Y+cb.Height > fr.Height {
		t.Errorf("the content is at %v, outside the %v frame it belongs to", cb, fr.Size())
	}
	// The title bar and border are reserved out of it, so it is not simply
	// the frame over again.
	if cb.Y == 0 || cb.Height >= fr.Height {
		t.Errorf("the content is %v with the frame %v; no chrome was reserved", cb, fr.Size())
	}
	// And it is measured against the frame rather than the room: sized to
	// the room it would be more than twice as wide.
	if cb.Width > fr.Width {
		t.Errorf("the content is %d wide inside a %d frame", cb.Width, fr.Width)
	}
}

// It is maximized, so it wears a maximized window's frame: the title bar and
// nothing else, flush to the frame's own edges. It merely wears it around a
// rect in the middle of the room rather than around the room itself.
//
// A normal frame there is the giveaway that the window has stopped believing
// it is maximized: side borders, a bottom border, rounded corners.
func TestACappedMaximizedWindowWearsTheMaximizedFrame(t *testing.T) {
	win := NewWindow("bounded")
	win.SetMaximumSize(core.UnitSize{Width: 480, Height: 320})
	win.SetBounds(core.UnitRect{Width: 1200, Height: 768})
	win.Maximize()

	fr := win.FrameRect()
	if fr.Width != 480 || fr.Height != 320 {
		t.Fatalf("the frame is %v, want its maximum of 480x320", fr.Size())
	}
	cb := win.ContentBounds()
	if cb.X != 0 {
		t.Errorf("the content starts %d in from the frame's left edge; a maximized "+
			"window reserves no side border", cb.X)
	}
	if cb.Width != fr.Width {
		t.Errorf("the content is %d wide in a %d frame; a maximized window reserves "+
			"no side borders", cb.Width, fr.Width)
	}
	if cb.Y+cb.Height != fr.Height {
		t.Errorf("the content ends %d short of the frame's bottom; a maximized window "+
			"reserves no bottom border", fr.Height-(cb.Y+cb.Height))
	}
	// The title row is still reserved -- that is the one thing a maximized
	// frame does draw.
	if cb.Y == 0 {
		t.Error("the content starts at the frame's top; the title bar was not reserved")
	}
}

// The maximized control inset lines a maximized window's buttons up with the
// host's own, which only means anything when the two start in the same place.
// A capped window's frame starts in the middle of the room, so its buttons
// belong at its own edge.
func TestACappedMaximizedWindowsControlsSitAtItsOwnEdge(t *testing.T) {
	win := NewWindow("bounded")
	win.SetMaximumSize(core.UnitSize{Width: 480, Height: 320})
	win.SetBounds(core.UnitRect{Width: 1200, Height: 768})
	win.Maximize()

	if got := win.TitleControlsInsetForTest(); got != 0 {
		t.Errorf("the controls are inset %d into a frame that is already inset %d",
			got, win.FrameRect().X)
	}
}
