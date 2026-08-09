package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The [<]/[>] overflow buttons sit ON TOP of the title run, so menu
// titles extending beneath them must not steal their events: a press
// scrolls (and opens nothing), a hover highlights the button (and no
// title), and a drag across them switches no menus.
func TestMenuBarScrollButtonsConsumeMouse(t *testing.T) {
	m := newOverflowingMenuBar()
	if !m.menusNeedScrolling() {
		t.Fatal("precondition: bar should overflow")
	}

	buttonWidth := m.scrollButtonWidth()
	buttonsX := m.Bounds().Width - m.dateTimeWidth() - buttonWidth*2
	if buttonsX < 0 {
		t.Fatal("precondition: buttons should be laid out on the bar")
	}
	inLeft := buttonsX + buttonWidth/2
	inRight := buttonsX + buttonWidth + buttonWidth/2

	// The fall-through scenario needs a title actually reaching the
	// button region.
	if m.calculateTotalMenusWidth() <= buttonsX {
		t.Fatal("precondition: the title run should extend beneath the buttons")
	}

	// Hovering a button highlights the button, never a title beneath it.
	m.HandleMouseMove(core.MouseMoveEvent{X: inRight, Y: 0})
	if m.hoverIndex != -1 {
		t.Errorf("hover over [>] highlighted title %d, want none", m.hoverIndex)
	}
	if m.hoverScrollBtn != 1 {
		t.Errorf("hover over [>]: hoverScrollBtn = %d, want 1", m.hoverScrollBtn)
	}

	// menuItemAt reports no title under either button.
	if idx := m.menuItemAt(inLeft, 0); idx != -1 {
		t.Errorf("menuItemAt over [<] = %d, want -1", idx)
	}
	if idx := m.menuItemAt(inRight, 0); idx != -1 {
		t.Errorf("menuItemAt over [>] = %d, want -1", idx)
	}

	// Pressing [>] scrolls right and opens nothing.
	if !m.HandleMousePress(core.MousePressEvent{X: inRight, Y: 0, Button: core.LeftButton}) {
		t.Fatal("press on [>] not consumed")
	}
	if m.scrollOffset != 1 {
		t.Errorf("after [>]: scrollOffset = %d, want 1", m.scrollOffset)
	}
	if m.activeMenu != nil {
		t.Error("press on [>] opened a menu")
	}

	// Pressing [<] scrolls back and opens nothing.
	if !m.HandleMousePress(core.MousePressEvent{X: inLeft, Y: 0, Button: core.LeftButton}) {
		t.Fatal("press on [<] not consumed")
	}
	if m.scrollOffset != 0 {
		t.Errorf("after [<]: scrollOffset = %d, want 0", m.scrollOffset)
	}
	if m.activeMenu != nil {
		t.Error("press on [<] opened a menu")
	}

	// A drag from an open menu across the buttons must not switch to the
	// title hidden beneath them.
	m.OpenMenu(0)
	opened := m.activeMenu
	if opened == nil {
		t.Fatal("OpenMenu(0) did not open a menu")
	}
	m.mouseDown = true
	m.dragging = true
	m.HandleMouseMove(core.MouseMoveEvent{X: inRight, Y: 0, Buttons: core.LeftButton})
	if m.activeMenu != opened {
		t.Error("drag across [>] switched the open menu")
	}
}
