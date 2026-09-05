package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A window that says how far it grows does not fill the room it was maximized
// into: it takes what it may and sits in the middle of the rest.
//
// Maximizing set the bounds to the whole client area, so max_width and
// max_height meant nothing the moment the window was maximized.
func TestAMaximizedWindowStopsAtItsMaximum(t *testing.T) {
	room := core.UnitRect{X: 40, Y: 24, Width: 800, Height: 600}

	// Nothing bounding it: the whole room.
	win := NewWindow("plain")
	if got := MaximizedBounds(win, room); got != room {
		t.Errorf("an unbounded window maximized to %v, want the whole %v", got, room)
	}

	// Bounded on one axis: capped there, centered there, whole on the other.
	win = NewWindow("wide")
	win.SetMaximumSize(core.UnitSize{Width: 300, Height: core.Unbounded})
	want := core.UnitRect{X: 40 + (800-300)/2, Y: 24, Width: 300, Height: 600}
	if got := MaximizedBounds(win, room); got != want {
		t.Errorf("a window capped at 300 wide maximized to %v, want %v", got, want)
	}

	// Bounded on both.
	win = NewWindow("both")
	win.SetMaximumSize(core.UnitSize{Width: 300, Height: 200})
	want = core.UnitRect{X: 40 + 250, Y: 24 + 200, Width: 300, Height: 200}
	if got := MaximizedBounds(win, room); got != want {
		t.Errorf("a window capped at 300x200 maximized to %v, want %v", got, want)
	}

	// A maximum larger than the room does not shrink it.
	win = NewWindow("roomy")
	win.SetMaximumSize(core.UnitSize{Width: 2000, Height: 2000})
	if got := MaximizedBounds(win, room); got != room {
		t.Errorf("a window capped above the room maximized to %v, want the whole %v", got, room)
	}
}

// Where a maximum and a minimum conflict the minimum wins here too.
func TestAMaximizedWindowsMinimumBeatsItsMaximum(t *testing.T) {
	room := core.UnitRect{Width: 800, Height: 600}
	win := NewWindow("fixed")
	win.SetMaximumSize(core.UnitSize{Width: 100, Height: core.Unbounded})
	win.SetMinimumSize(core.UnitSize{Width: 400, Height: 0})

	if got := MaximizedBounds(win, room).Width; got != 400 {
		t.Errorf("a minimum of 400 against a maximum of 100 gave %d, want the minimum", got)
	}
}

// What the window left over is the room around it, and nothing is left over
// when it fills its room.
func TestTheFillerIsWhatTheWindowLeftOver(t *testing.T) {
	// A small room, because the tiling below is checked a unit at a time: a
	// seam one unit wide is exactly the mistake worth catching, and sampling
	// coarsely steps straight over it.
	room := core.UnitRect{X: 7, Y: 5, Width: 200, Height: 150}

	if got := MaximizedFillerRects(NewWindow("plain"), room); len(got) != 0 {
		t.Errorf("a window filling its room left %v over", got)
	}

	win := NewWindow("capped")
	win.SetMaximumSize(core.UnitSize{Width: 80, Height: 60})
	inner := MaximizedBounds(win, room)
	rects := MaximizedFillerRects(win, room)
	if len(rects) != 4 {
		t.Fatalf("a window capped on both axes left %d rectangles over, want 4", len(rects))
	}

	// None of them touches the window, and none escapes the room.
	for _, r := range rects {
		if !r.Intersection(inner).IsEmpty() {
			t.Errorf("filler %v overlaps the window at %v", r, inner)
		}
		if r.Intersection(room) != r {
			t.Errorf("filler %v reaches outside the room %v", r, room)
		}
	}

	// And together with the window they tile the room: every point is in the
	// window or in exactly one filler rectangle. Area alone would not say
	// this -- a rectangle nudged off its edge keeps its area while leaving a
	// seam behind it.
	for y := room.Y; y < room.Y+room.Height; y++ {
		for x := room.X; x < room.X+room.Width; x++ {
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
				t.Fatalf("%v is covered %d times by the window and its filler, want once", pt, covers)
			}
		}
	}
}

// The room a maximized window declined belongs to it: a press there raises it
// rather than falling through to whatever is behind.
func TestAPressInTheFillerRaisesTheWindowItSurrounds(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})
	room := m.ClientArea()

	front := NewWindow("front")
	front.SetMaximumSize(core.UnitSize{Width: 200, Height: 150})
	m.AddWindow(front)
	m.MaximizeWindow(front)

	// A small window raised above it, well clear of the point pressed: it is
	// on top, so the filler must beat what is BELOW rather than what is over
	// the point itself.
	corner := NewWindow("corner")
	corner.SetBounds(core.UnitRect{X: room.X, Y: room.Y, Width: 40, Height: 32})
	m.AddWindow(corner)
	m.ActivateWindow(corner)

	rects := MaximizedFillerRects(front, room)
	if len(rects) == 0 {
		t.Fatal("the capped window left no room over to press in")
	}
	// The rectangle below the window, whose middle no other window covers.
	pt := core.UnitPoint{X: rects[1].X + rects[1].Width/2, Y: rects[1].Y + rects[1].Height/2}
	if front.Bounds().Contains(pt) || corner.Bounds().Contains(pt) {
		t.Fatalf("the point %v is inside a window rather than the filler", pt)
	}

	if !m.HandleMousePress(core.MousePressEvent{X: pt.X, Y: pt.Y, Button: core.LeftButton}) {
		t.Error("a press in the filler was not consumed")
	}
	if m.ActiveWindow() != front {
		t.Error("a press in the filler did not raise the window it surrounds")
	}
}

// A window ABOVE the filler still takes the press: the filler is the room its
// own window declined, not a claim over anything drawn on top of it.
func TestAWindowOverTheFillerTakesThePress(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})
	room := m.ClientArea()

	capped := NewWindow("capped")
	capped.SetMaximumSize(core.UnitSize{Width: 200, Height: 150})
	m.AddWindow(capped)
	m.MaximizeWindow(capped)

	over := NewWindow("over")
	over.SetBounds(room)
	m.AddWindow(over)
	m.ActivateWindow(over)

	rects := MaximizedFillerRects(capped, room)
	pt := core.UnitPoint{X: rects[1].X + rects[1].Width/2, Y: rects[1].Y + rects[1].Height/2}

	m.HandleMousePress(core.MousePressEvent{X: pt.X, Y: pt.Y, Button: core.LeftButton})
	if m.ActiveWindow() != over {
		t.Error("a press over a window covering the filler raised the window underneath")
	}
}
