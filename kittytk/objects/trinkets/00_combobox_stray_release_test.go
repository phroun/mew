package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// comboOnADesktop stands a combo box in a window on a desktop, so clicks go
// the way a real one does: through the window manager, which routes a press to
// a popup only when the pointer is over it but hands EVERY release to every
// popup there is.
func comboOnADesktop(t *testing.T) (*Desktop, *ComboBox, core.UnitPoint) {
	t.Helper()
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	d.windowManager.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})
	d.setupTearOff(nil, nil)

	win := window.NewWindow("w")
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 400, Height: 300})
	d.windowManager.AddWindow(win)

	cb := NewComboBox()
	for _, item := range []string{"one", "two", "three", "four", "five", "six"} {
		cb.AddItem(item)
	}
	win.AddChild(cb)
	cb.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 160, Height: 16})

	client := win.ClientArea()
	return d, cb, core.UnitPoint{X: client.X + 8, Y: client.Y + 8}
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

	d.windowManager.HandleMousePress(press)
	if !cb.IsOpen() {
		t.Fatal("a press on the box did not open the drop-down")
	}
	d.windowManager.HandleMouseRelease(release)
	if !cb.IsOpen() {
		t.Fatal("the release of the opening click closed the drop-down")
	}

	d.windowManager.HandleMouseRelease(release)
	if !cb.IsOpen() {
		t.Error("a second release with no press behind it closed the drop-down")
	}
}

// A click outside the open list still dismisses it: the press-and-release the
// user actually made is the one that answers.
func TestAClickOutsideShutsTheDropDown(t *testing.T) {
	d, cb, at := comboOnADesktop(t)

	d.windowManager.HandleMousePress(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	d.windowManager.HandleMouseRelease(core.MouseReleaseEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	if !cb.IsOpen() {
		t.Fatal("a click on the box did not leave the drop-down open")
	}

	away := core.UnitPoint{X: at.X + 240, Y: at.Y + 160}
	d.windowManager.HandleMousePress(core.MousePressEvent{X: away.X, Y: away.Y, Button: core.LeftButton})
	d.windowManager.HandleMouseRelease(core.MouseReleaseEvent{X: away.X, Y: away.Y, Button: core.LeftButton})
	if cb.IsOpen() {
		t.Error("a click away from the drop-down left it open")
	}
}
