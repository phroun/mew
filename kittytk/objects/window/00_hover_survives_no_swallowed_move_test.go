package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// swallowingDesktop stands in for a desktop with a dropdown open: it takes
// every move handed to it and reports it handled, which is what a menu bar
// does for as long as one of its menus is down.
type swallowingDesktop struct {
	graphicalDesktop
	swallow bool
}

func (s *swallowingDesktop) HandleMouseMove(core.MouseMoveEvent) bool { return s.swallow }

// A move the desktop swallows still says the pointer left the window it was
// over. Nothing else will say so -- the hover routing below never runs -- so
// a title-bar button lit on the way to the menu bar stays lit for as long as
// the menu is up, and past whatever the menu goes on to do.
func TestAMoveSwallowedAboveTheWindowsPutsTheirHoverOut(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 1200, Height: 800})
	d := &swallowingDesktop{}
	d.Init(d)
	m.SetDesktop(d)

	w := NewWindow("w")
	w.SetBounds(core.UnitRect{X: 100, Y: 200, Width: 300, Height: 200})
	m.AddWindow(w)
	w.SetParent(d)

	bx, by := buttonPoint(t, w, TitleButtonMinimize)
	b := w.Bounds()
	m.HandleMouseMove(core.MouseMoveEvent{X: b.X + bx, Y: b.Y + by})
	if got := w.hoveredButton; got != TitleButtonMinimize {
		t.Fatalf("the minimize button did not light under the pointer: %v", got)
	}

	// The pointer travels up to the menu bar and opens a menu; from here the
	// desktop takes the moves.
	d.swallow = true
	m.HandleMouseMove(core.MouseMoveEvent{X: 40, Y: 4})
	if got := w.hoveredButton; got != TitleButtonNone {
		t.Errorf("%v is still lit while the pointer is up in an open menu", got)
	}
}

// The popup layer is above the windows for the same reason, and takes moves
// the same way.
func TestAMoveTakenByAPopupPutsWindowHoverOut(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 1200, Height: 800})

	w := NewWindow("w")
	w.SetBounds(core.UnitRect{X: 100, Y: 200, Width: 300, Height: 200})
	m.AddWindow(w)

	bx, by := buttonPoint(t, w, TitleButtonMinimize)
	b := w.Bounds()
	m.HandleMouseMove(core.MouseMoveEvent{X: b.X + bx, Y: b.Y + by})
	if got := w.hoveredButton; got != TitleButtonMinimize {
		t.Fatalf("the minimize button did not light under the pointer: %v", got)
	}

	// A dropdown opens over the desktop and takes the next move.
	m.RegisterPopup(&core.PopupRequest{
		ID:              "menu",
		Bounds:          core.UnitRect{X: 0, Y: 0, Width: 200, Height: 120},
		HandleMouseMove: func(core.MouseMoveEvent) bool { return true },
	})
	m.HandleMouseMove(core.MouseMoveEvent{X: 40, Y: 40})
	if got := w.hoveredButton; got != TitleButtonNone {
		t.Errorf("%v is still lit while the pointer is over the popup", got)
	}
}
