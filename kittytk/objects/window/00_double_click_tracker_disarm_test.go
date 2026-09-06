package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The title bar's double-click is two plain clicks on the title bar and
// nothing else. A press the WINDOW took -- a caption button -- is not half of
// one, and leaving it in the tracker lets the next single title click pair
// with it and toggle maximize a second time, which is what "a single click
// counted as a double click" looks like from the outside.
func TestATornWindowsTrackerIsDisarmedByAPressTheWindowTook(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 400, Height: 240}, x: 100, y: 100}
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, ppu1, func() (int, int) { return 0, 0 }, nil)

	click := func(x, y core.Unit) {
		h.Event(core.MousePressEvent{X: x, Y: y, Button: core.LeftButton})
		h.Event(core.MouseReleaseEvent{X: x, Y: y, Button: core.LeftButton})
	}
	fr := win.FrameRect()
	titleX, titleY := fr.Width-20, core.Unit(7) // clear of the buttons on the left
	bx, by := buttonPoint(t, win, TitleButtonMinimize)

	click(titleX, titleY) // one plain title click arms the tracker
	click(bx, by)         // a caption button, which the window takes
	click(titleX, titleY) // one plain title click
	if h.zoomed {
		t.Error("a single title click paired with a button press the window had taken")
	}

	// And two plain title clicks still zoom, so the disarming did not simply
	// break the gesture.
	click(titleX, titleY)
	if !h.zoomed {
		t.Error("two plain title clicks did not zoom the window")
	}
}

// A press that lands outside the title bar disarms it too: the gesture is two
// clicks ON the title bar, not two clicks with anything in between.
func TestATornWindowsTrackerIsDisarmedByAPressElsewhere(t *testing.T) {
	surf := &nativeFakeSurface{size: core.UnitSize{Width: 400, Height: 240}, x: 100, y: 100}
	win := NewWindow("torn")
	h := NewTearOffHost(win, surf, ppu1, func() (int, int) { return 0, 0 }, nil)

	click := func(x, y core.Unit) {
		h.Event(core.MousePressEvent{X: x, Y: y, Button: core.LeftButton})
		h.Event(core.MouseReleaseEvent{X: x, Y: y, Button: core.LeftButton})
	}
	fr := win.FrameRect()
	titleX, titleY := fr.Width-20, core.Unit(7)

	click(titleX, titleY)
	click(fr.Width/2, fr.Height/2) // the middle of the window's content
	click(titleX, titleY)
	if h.zoomed {
		t.Error("a single title click paired with one across the window's content")
	}
}

// The in-surface manager keeps the same rule: a caption button disarms its
// tracker rather than being recorded as a click on the title bar.
func TestTheManagersTrackerIsDisarmedByACaptionButton(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 1200, Height: 800})
	win := NewWindow("w")
	win.SetBounds(core.UnitRect{X: 96, Y: 96, Width: 300, Height: 200})
	m.AddWindow(win)

	b := win.Bounds()
	bx, by := buttonPoint(t, win, TitleButtonMaximize)
	m.HandleMousePress(core.MousePressEvent{X: b.X + bx, Y: b.Y + by, Button: core.LeftButton})
	m.HandleMouseRelease(core.MouseReleaseEvent{X: b.X + bx, Y: b.Y + by, Button: core.LeftButton})

	m.mu.RLock()
	armed := !m.titleClicks.at.IsZero()
	m.mu.RUnlock()
	if armed {
		t.Error("a caption button left a click in the title bar's double-click tracker")
	}
}

// And a real double-click still works, in the host that owns the gesture:
// press, release, press, release on the title bar maximizes, and the same
// again restores. Without this the release wiring above could go missing and
// nothing would notice -- the tracker would simply never fire.
func TestATitleDoubleClickStillMaximizesAndRestores(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 800, Height: 608})
	win := NewWindow("w")
	win.SetBounds(core.UnitRect{X: 96, Y: 96, Width: 304, Height: 208})
	m.AddWindow(win)

	click := func() {
		b := win.Bounds()
		fr := win.FrameRect()
		x, y := b.X+fr.X+fr.Width/2, b.Y+fr.Y+4
		m.HandleMousePress(core.MousePressEvent{X: x, Y: y, Button: core.LeftButton})
		m.HandleMouseRelease(core.MouseReleaseEvent{X: x, Y: y, Button: core.LeftButton})
	}

	click()
	click()
	if !win.IsMaximized() {
		t.Fatal("a double-click on the title bar did not maximize the window")
	}

	// A single click on the maximized window's title bar does nothing...
	click()
	if !win.IsMaximized() {
		t.Error("a single click after the maximize restored the window")
	}
	// ...and the one after it completes the pair.
	click()
	if win.IsMaximized() {
		t.Error("a second click did not restore the window")
	}
}
