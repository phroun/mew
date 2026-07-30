package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A button in a MessageBox must behave like a button anywhere else: it lights
// up on hover, and once pressed it drops its pressed look the moment the
// pointer drags off it (so the click can be cancelled). This exercises the
// content trinket's HandleMouseMove forwarding.
func TestMessageBoxButtonHoverAndDragOff(t *testing.T) {
	m := NewMessageBox("t", "msg", ButtonOK|ButtonCancel)
	ok := m.content.buttonTrinkets[0]
	// Paint normally assigns bounds; set them by hand for the test.
	ok.SetBounds(core.UnitRect{X: 10, Y: 40, Width: 40, Height: 32})

	// Plain hover over the button lights mouseOver.
	m.content.HandleMouseMove(core.MouseMoveEvent{X: 12, Y: 44, Buttons: 0})
	if !ok.mouseOver {
		t.Error("hover over a MessageBox button did not set mouseOver")
	}
	// Pointer leaves: hover clears.
	m.content.HandleMouseMove(core.MouseMoveEvent{X: 200, Y: 44, Buttons: 0})
	if ok.mouseOver {
		t.Error("hover did not clear when the pointer left the button")
	}

	// Press the button, then drag off: the pressed look must drop.
	m.content.HandleMousePress(core.MousePressEvent{X: 12, Y: 44, Button: core.LeftButton})
	if !ok.pressed || !ok.hovered {
		t.Fatal("button should be pressed and hovered right after the press")
	}
	m.content.HandleMouseMove(core.MouseMoveEvent{X: 200, Y: 44, Buttons: 1})
	if ok.hovered {
		t.Error("button stayed depressed after the pointer dragged off it")
	}
	// Drag back on: it re-lights as pressed.
	m.content.HandleMouseMove(core.MouseMoveEvent{X: 12, Y: 44, Buttons: 1})
	if !ok.hovered {
		t.Error("button did not re-light when the pointer dragged back on")
	}
}

// Releasing while the pointer is off the pressed button cancels the click.
func TestMessageBoxButtonReleaseOffCancels(t *testing.T) {
	m := NewMessageBox("t", "msg", ButtonOK)
	ok := m.content.buttonTrinkets[0]
	ok.SetBounds(core.UnitRect{X: 10, Y: 40, Width: 40, Height: 32})

	clicked := false
	ok.SetOnClick(func() { clicked = true })

	m.content.HandleMousePress(core.MousePressEvent{X: 12, Y: 44, Button: core.LeftButton})
	// Drag off, then release: no click.
	m.content.HandleMouseMove(core.MouseMoveEvent{X: 200, Y: 44, Buttons: 1})
	m.content.HandleMouseRelease(core.MouseReleaseEvent{X: 200, Y: 44, Button: core.LeftButton})
	if clicked {
		t.Error("releasing off the button still fired the click")
	}
}
