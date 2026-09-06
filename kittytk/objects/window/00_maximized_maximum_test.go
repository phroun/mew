package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A maximized window takes the whole room as its surface, whatever it says
// about how far it grows. What a maximum does is hold its FRAME to a size
// inside that surface, centered, with the room it declined shaded around it.
//
// Handing it smaller bounds instead is what the shade kept failing over: the
// room around the window then belongs to the layer underneath, and on a
// compositing host nothing repaints that layer when a window maximizes.
func TestAMaximizedWindowTakesTheWholeRoom(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})
	room := m.ClientArea()
	for _, max := range []core.UnitSize{
		{Width: core.Unbounded, Height: core.Unbounded},
		{Width: 300, Height: core.Unbounded},
		{Width: 300, Height: 200},
	} {
		win := NewWindow("w")
		win.SetMaximumSize(max)
		m.AddWindow(win)
		m.MaximizeWindow(win)
		if got := win.Bounds(); got != room {
			t.Errorf("capped at %v it maximized to %v, want the whole room %v", max, got, room)
		}
		m.RemoveWindow(win)
	}
}

// And its frame sits inside that surface: capped where it says so, centered
// there, whole on any axis it says nothing about.
//
// Centred and then floored onto the cell grid, which a cell surface needs and
// which the numbers below are picked to make visible.
func TestAMaximizedWindowsFrameStopsAtItsMaximum(t *testing.T) {
	m := core.DefaultCellMetrics()
	surface := core.UnitSize{Width: 800, Height: 600}
	whole := core.UnitRect{Width: 800, Height: 600}

	// Nothing bounding it: the whole surface.
	win := NewWindow("plain")
	if got := MaximizedFrameRect(win, surface); got != whole {
		t.Errorf("an unbounded window's frame is %v, want the whole %v", got, whole)
	}

	// Bounded on one axis: capped there, centered there, whole on the other.
	win = NewWindow("wide")
	win.SetMaximumSize(core.UnitSize{Width: 300, Height: core.Unbounded})
	want := core.UnitRect{X: m.RoundDownToCellX((800 - 300) / 2), Y: 0, Width: 300, Height: 600}
	if got := MaximizedFrameRect(win, surface); got != want {
		t.Errorf("a window capped at 300 wide framed %v, want %v", got, want)
	}

	// Bounded on both.
	win = NewWindow("both")
	win.SetMaximumSize(core.UnitSize{Width: 300, Height: 200})
	want = core.UnitRect{X: m.RoundDownToCellX(250), Y: m.RoundDownToCellY(200), Width: 300, Height: 200}
	if got := MaximizedFrameRect(win, surface); got != want {
		t.Errorf("a window capped at 300x200 framed %v, want %v", got, want)
	}

	// A maximum larger than the room does not shrink it.
	win = NewWindow("roomy")
	win.SetMaximumSize(core.UnitSize{Width: 2000, Height: 2000})
	if got := MaximizedFrameRect(win, surface); got != whole {
		t.Errorf("a window capped above the room framed %v, want the whole %v", got, whole)
	}
}

// Where a maximum and a minimum conflict the minimum wins here too.
func TestAMaximizedWindowsMinimumBeatsItsMaximum(t *testing.T) {
	surface := core.UnitSize{Width: 800, Height: 600}
	win := NewWindow("fixed")
	win.SetMaximumSize(core.UnitSize{Width: 100, Height: core.Unbounded})
	win.SetMinimumSize(core.UnitSize{Width: 400, Height: 0})

	if got := MaximizedFrameRect(win, surface).Width; got != 400 {
		t.Errorf("a minimum of 400 against a maximum of 100 gave %d, want the minimum", got)
	}
}

// The shade is what the frame left over, and there is none when the frame
// fills the surface.
func TestTheShadeIsWhatTheFrameLeftOver(t *testing.T) {
	// A small surface, because the tiling below is checked a unit at a time:
	// a seam one unit wide is exactly the mistake worth catching, and
	// sampling coarsely steps straight over it.
	surface := core.UnitRect{Width: 200, Height: 150}

	if got := shadeRects(surface, surface); len(got) != 0 {
		t.Errorf("a frame filling its surface left %v over", got)
	}

	win := NewWindow("capped")
	win.SetMaximumSize(core.UnitSize{Width: 80, Height: 60})
	inner := MaximizedFrameRect(win, surface.Size())
	rects := shadeRects(surface, inner)
	if len(rects) != 4 {
		t.Fatalf("a window capped on both axes left %d rectangles over, want 4", len(rects))
	}

	// None of them touches the frame, and none escapes the surface.
	for _, r := range rects {
		if !r.Intersection(inner).IsEmpty() {
			t.Errorf("shade %v overlaps the frame at %v", r, inner)
		}
		if r.Intersection(surface) != r {
			t.Errorf("shade %v reaches outside the surface %v", r, surface)
		}
	}

	// And together with the frame they tile the surface: every point is in
	// the frame or in exactly one shade rectangle. Area alone would not say
	// this -- a rectangle nudged off its edge keeps its area while leaving a
	// seam behind it.
	for y := surface.Y; y < surface.Y+surface.Height; y++ {
		for x := surface.X; x < surface.X+surface.Width; x++ {
			pt := core.UnitPoint{X: x, Y: y}
			covers := 0
			if inner.Contains(pt) {
				covers++
			}
			for _, r := range rects {
				if r.Contains(pt) {
					covers++
				}
			}
			if covers != 1 {
				t.Fatalf("%v is covered %d times by the frame and its shade, want once", pt, covers)
			}
		}
	}
}

// The shade is the window's own surface: a press there belongs to it and goes
// no further, rather than falling through to whatever is behind.
func TestAPressInTheShadeStaysWithTheWindow(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})
	room := m.ClientArea()

	behind := NewWindow("behind")
	behind.SetBounds(room)
	m.AddWindow(behind)

	front := NewWindow("front")
	front.SetMaximumSize(core.UnitSize{Width: 200, Height: 150})
	m.AddWindow(front)
	m.MaximizeWindow(front)
	m.ActivateWindow(front)

	fr := front.frameRect()
	// A point in the band below the frame, inside the window's surface.
	pt := core.UnitPoint{
		X: room.X + room.Width/2,
		Y: room.Y + fr.Y + fr.Height + (room.Height-(fr.Y+fr.Height))/2,
	}
	if fr.Contains(core.UnitPoint{X: pt.X - room.X, Y: pt.Y - room.Y}) {
		t.Fatalf("the point %v is on the frame rather than the shade", pt)
	}

	if !m.HandleMousePress(core.MousePressEvent{X: pt.X, Y: pt.Y, Button: core.LeftButton}) {
		t.Error("a press in the shade was not consumed")
	}
	if m.ActiveWindow() != front {
		t.Error("a press in the shade did not stay with the window it belongs to")
	}
}

// A window ABOVE it still takes the press: the shade is the room its own
// window declined, not a claim over anything drawn on top of it.
func TestAWindowOverTheShadeTakesThePress(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})
	room := m.ClientArea()

	capped := NewWindow("capped")
	capped.SetMaximumSize(core.UnitSize{Width: 200, Height: 150})
	m.AddWindow(capped)
	m.MaximizeWindow(capped)

	over := NewWindow("over")
	over.SetBounds(core.UnitRect{X: room.X, Y: room.Y, Width: 80, Height: 64})
	m.AddWindow(over)
	m.ActivateWindow(over)

	// The top-left corner is shade on the capped window and frame on the one
	// above it.
	pt := core.UnitPoint{X: room.X + 40, Y: room.Y + 32}
	m.HandleMousePress(core.MousePressEvent{X: pt.X, Y: pt.Y, Button: core.LeftButton})
	if m.ActiveWindow() != over {
		t.Error("a press over a window covering the shade raised the window underneath")
	}
}

// The cap survives a relayout. A desktop resize re-fits every maximized
// window to the client area, and that happens at startup too -- so a frame
// sized only at the moment of maximizing is wiped before anyone sees it.
func TestAMaximizedWindowsCapSurvivesARelayout(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 1200, Height: 800})

	win := NewWindow("bounded")
	win.SetMaximumSize(core.UnitSize{Width: 480, Height: 320})
	m.AddWindow(win)
	m.MaximizeWindow(win)

	framed := func(what string) {
		t.Helper()
		room := m.ClientArea()
		if got := win.Bounds(); got != room {
			t.Errorf("%s its surface is %v, want the whole room %v", what, got, room)
		}
		fr := win.frameRect()
		if fr.Width != 480 || fr.Height != 320 {
			t.Errorf("%s its frame is %v, want its maximum of 480x320", what, fr.Size())
		}
		cm := core.DefaultCellMetrics()
		wantX := cm.RoundDownToCellX((room.Width - 480) / 2)
		wantY := cm.RoundDownToCellY((room.Height - 320) / 2)
		if fr.X != wantX || fr.Y != wantY {
			t.Errorf("%s its frame sits at %d,%d, want %d,%d -- centered in %v and "+
				"floored onto the cell grid", what, fr.X, fr.Y, wantX, wantY, room)
		}
	}
	framed("maximized")

	// The same bounds again is still a relayout, which is what startup does.
	m.SetScreenBounds(core.UnitRect{Width: 1200, Height: 800})
	framed("after a relayout")

	// And a resize re-fits it to the new room, still capped and still centered.
	m.SetScreenBounds(core.UnitRect{Width: 900, Height: 700})
	framed("after a resize")
}

// A window minimized while maximized comes back maximized, and comes back
// capped: RestoreWindow re-fits it to the client area too.
func TestARestoredMaximizedWindowKeepsItsCap(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 1200, Height: 800})

	win := NewWindow("bounded")
	win.SetMaximumSize(core.UnitSize{Width: 480, Height: 320})
	m.AddWindow(win)
	m.MaximizeWindow(win)
	m.MinimizeWindow(win)
	m.RestoreWindow(win)

	if !win.IsMaximized() {
		t.Fatal("it came back un-maximized")
	}
	if got := win.Bounds(); got != m.ClientArea() {
		t.Errorf("it came back at %v, want the whole room %v", got, m.ClientArea())
	}
	if got := win.frameRect(); got.Width != 480 || got.Height != 320 {
		t.Errorf("its frame came back %v, want its maximum of 480x320", got.Size())
	}
}

// Every gesture that maximizes asks the same question. A window that may not
// be maximized is not maximized by any of them: the desktop's title
// double-click, the keyboard toggle, the title button's own case, or a torn
// window's zoom -- which is what maximizing means once it is out on the OS.
func TestNoGestureMaximizesAWindowThatMayNot(t *testing.T) {
	for _, flags := range []WindowFlags{WindowFlagNoResize, WindowFlagNoMaximize} {
		// The desktop's title double-click.
		m := NewWindowManager()
		m.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})
		win := NewWindow("dlg")
		win.SetFlags(flags)
		win.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 240, Height: 160})
		m.AddWindow(win)
		before := win.Bounds()
		for i := 0; i < 2; i++ {
			m.HandleMousePress(core.MousePressEvent{X: 180, Y: 104, Button: core.LeftButton})
			m.HandleMouseRelease(core.MouseReleaseEvent{X: 180, Y: 104, Button: core.LeftButton})
		}
		if win.IsMaximized() || win.Bounds() != before {
			t.Errorf("flags %v: a title double-click maximized it to %v", flags, win.Bounds())
		}

		// The keyboard toggle.
		win = NewWindow("dlg")
		win.SetFlags(flags)
		win.SetBounds(core.UnitRect{X: 10, Y: 10, Width: 240, Height: 160})
		win.HandleKeyPress(core.KeyPressEvent{Key: "M-F10"})
		if win.IsMaximized() {
			t.Errorf("flags %v: the keyboard toggle maximized it", flags)
		}
	}
}
