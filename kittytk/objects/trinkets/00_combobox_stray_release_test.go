package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// comboOnADesktop stands a combo box in a window on a desktop, away from the
// origin, so clicks go the way a real one does: through the window manager, in
// screen coordinates, to a box that knows its own.
func comboOnADesktop(t *testing.T) (*Desktop, *ComboBox, core.UnitPoint) {
	t.Helper()
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	d.windowManager.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})
	d.setupTearOff(nil, nil)

	win := window.NewWindow("w")
	win.SetBounds(core.UnitRect{X: 80, Y: 96, Width: 400, Height: 300})
	d.windowManager.AddWindow(win)

	cb := NewComboBox()
	for _, item := range []string{"one", "two", "three", "four", "five", "six"} {
		cb.AddItem(item)
	}
	win.AddChild(cb)
	cb.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 160, Height: 16})

	b, client := win.Bounds(), win.ClientArea()
	return d, cb, core.UnitPoint{X: b.X + client.X + 8, Y: b.Y + client.Y + 8}
}

// One click drops the list open and leaves it open, and a release the terminal
// reports a second time does not close it behind the user's back.
//
// Every release reaches the popup's handler wherever the pointer is, and in
// click mode a release outside the list is how the list is dismissed. A
// release with no press of its own behind it is not that.
func TestAStrayReleaseDoesNotShutTheDropDown(t *testing.T) {
	d, cb, at := comboOnADesktop(t)

	press := core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton}
	release := core.MouseReleaseEvent{X: at.X, Y: at.Y, Button: core.LeftButton}

	d.dispatchEvent(press)
	if !cb.IsOpen() {
		t.Fatal("a press on the box did not open the drop-down")
	}
	d.dispatchEvent(release)
	if !cb.IsOpen() {
		t.Fatal("the release of the opening click closed the drop-down")
	}

	d.dispatchEvent(release)
	if !cb.IsOpen() {
		t.Error("a second release with no press behind it closed the drop-down")
	}
}

// A click that does not move is a click, however many position reports it
// carries.
//
// A press on the box opens the drop-down; from there the gesture belongs to
// the drop-down, whose handlers are fed screen coordinates. The press that
// opened it landed on the box, and the box knows where in ITSELF that was --
// so the two ends of "how far has the pointer travelled" have to be brought
// into one space before they are subtracted. A move that went nowhere read as
// a drag of the whole distance between the box's corner and the screen's, and
// the release then cancelled the gesture and shut the list.
func TestAClickThatDoesNotMoveLeavesTheDropDownOpen(t *testing.T) {
	d, cb, at := comboOnADesktop(t)

	d.dispatchEvent(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	if !cb.IsOpen() {
		t.Fatal("a press on the box did not open the drop-down")
	}
	// The position report a click carries, at the point the press was.
	d.dispatchEvent(core.MouseMoveEvent{X: at.X, Y: at.Y})
	if cb.dragging {
		t.Error("a move that went nowhere began a drag")
	}
	d.dispatchEvent(core.MouseReleaseEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	if !cb.IsOpen() {
		t.Error("the click shut the drop-down it opened")
	}
}

// Dragging off the list still cancels, so the gesture that does something has
// not been lost with the one that does not.
func TestADragOffTheListCancelsIt(t *testing.T) {
	d, cb, at := comboOnADesktop(t)

	d.dispatchEvent(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	away := core.UnitPoint{X: at.X + 240, Y: at.Y + 160}
	d.dispatchEvent(core.MouseMoveEvent{X: away.X, Y: away.Y, Buttons: core.LeftButton})
	if !cb.dragging {
		t.Fatal("a real drag was not read as one")
	}
	d.dispatchEvent(core.MouseReleaseEvent{X: away.X, Y: away.Y, Button: core.LeftButton})
	if cb.IsOpen() {
		t.Error("releasing away from the list left it open")
	}
}

// A click outside the open list still dismisses it: the press-and-release the
// user actually made is the one that answers.
func TestAClickOutsideShutsTheDropDown(t *testing.T) {
	d, cb, at := comboOnADesktop(t)

	d.dispatchEvent(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	d.dispatchEvent(core.MouseReleaseEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	if !cb.IsOpen() {
		t.Fatal("a click on the box did not leave the drop-down open")
	}

	away := core.UnitPoint{X: at.X + 240, Y: at.Y + 160}
	d.dispatchEvent(core.MousePressEvent{X: away.X, Y: away.Y, Button: core.LeftButton})
	d.dispatchEvent(core.MouseReleaseEvent{X: away.X, Y: away.Y, Button: core.LeftButton})
	if cb.IsOpen() {
		t.Error("a click away from the drop-down left it open")
	}
}
