package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// buttonPoint is the middle of the given title-bar button, in window
// coordinates. The middle rather than a corner: the resize grip runs along
// the frame's outer edge, and a press there resizes instead of hovering, so
// the manager sends nothing that would light a button standing on it.
func buttonPoint(t *testing.T, w *Window, b TitleButton) (core.Unit, core.Unit) {
	t.Helper()
	bounds := w.Bounds()
	minX, minY := core.Unit(-1), core.Unit(-1)
	maxX, maxY := core.Unit(-1), core.Unit(-1)
	for y := core.Unit(0); y < bounds.Height; y++ {
		for x := core.Unit(0); x < bounds.Width; x++ {
			if w.buttonAtPosition(x, y) != b {
				continue
			}
			if minX < 0 || x < minX {
				minX = x
			}
			if minY < 0 || y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if minX < 0 {
		t.Fatalf("no point in the window is over button %v", b)
	}
	return (minX + maxX) / 2, (minY + maxY) / 2
}

// A title-bar button lights up because the pointer is over it, and only a
// move can say that. A window that moves out from under a stationary pointer
// takes its buttons elsewhere with nothing to notice, so the highlight has to
// go out with the move -- restoring from maximized is the gesture that shows
// it, because the button that did the restoring is the one left lit.
func TestTitleButtonHoverGoesOutWhenTheWindowMoves(t *testing.T) {
	w := NewWindow("w")
	w.SetBounds(core.UnitRect{X: 96, Y: 96, Width: 304, Height: 208})

	x, y := buttonPoint(t, w, TitleButtonMinimize)
	w.HandleMouseMove(core.MouseMoveEvent{X: x, Y: y})
	if got := w.hoveredButton; got != TitleButtonMinimize {
		t.Fatalf("hovering the minimize button left %v lit, want TitleButtonMinimize", got)
	}

	w.SetBounds(core.UnitRect{X: 400, Y: 304, Width: 304, Height: 208})
	if got := w.hoveredButton; got != TitleButtonNone {
		t.Errorf("after the window moved, %v is still lit under a pointer that never moved", got)
	}

	// A SetBounds that changes nothing changes nothing: a relayout must not
	// put out a highlight the pointer is still sitting on.
	w.HandleMouseMove(core.MouseMoveEvent{X: x, Y: y})
	if w.hoveredButton != TitleButtonMinimize {
		t.Fatal("could not re-light the button to test the no-op case")
	}
	w.SetBounds(w.Bounds())
	if got := w.hoveredButton; got != TitleButtonMinimize {
		t.Errorf("a no-op SetBounds put the highlight out (%v)", got)
	}
}

// The reported gesture end to end: the maximize button is clicked, the window
// leaves the pointer behind, and the button it left behind is dark.
func TestRestoringLeavesNoButtonLit(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 1200, Height: 800})
	w := NewWindow("w")
	w.SetBounds(core.UnitRect{X: 96, Y: 96, Width: 304, Height: 208})
	m.AddWindow(w)

	m.MaximizeWindow(w)
	x, y := buttonPoint(t, w, TitleButtonMaximize)
	w.HandleMouseMove(core.MouseMoveEvent{X: x, Y: y})
	if w.hoveredButton != TitleButtonMaximize {
		t.Fatalf("the maximized window's restore button did not light: %v", w.hoveredButton)
	}

	m.RestoreWindow(w)
	if got := w.Bounds(); got.X != 96 || got.Y != 96 {
		t.Fatalf("restore put the window at %v, want it back at 96,96", got)
	}
	if got := w.hoveredButton; got != TitleButtonNone {
		t.Errorf("%v is still lit after the window restored out from under the pointer", got)
	}
}
