package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// moveRecorder is a ScrollArea content trinket that records the mouse
// events it receives, so we can assert the ScrollArea forwards Buttons
// and Modifiers (not just X/Y).
type moveRecorder struct {
	*core.TrinketBase
	moveButtons  core.MouseButton
	pressMods    core.KeyModifiers
	gotMove      bool
	gotPress     bool
	moveX, moveY core.Unit
}

func (m *moveRecorder) HandleMouseMove(e core.MouseMoveEvent) bool {
	m.gotMove = true
	m.moveButtons = e.Buttons
	m.moveX, m.moveY = e.X, e.Y
	return true
}

func (m *moveRecorder) HandleMousePress(e core.MousePressEvent) bool {
	m.gotPress = true
	m.pressMods = e.Modifiers
	return true
}

// A ScrollArea must forward Buttons on move (drag-select needs
// Buttons&LeftButton) and Modifiers on press (shift-click), plus offset
// X/Y by the scroll amount. Dropping Buttons broke drag-to-select in any
// text control inside a scroll area.
func TestScrollAreaForwardsButtonsAndModifiers(t *testing.T) {
	rec := &moveRecorder{TrinketBase: core.NewTrinketBase()}
	sa := NewScrollArea()
	sa.SetBounds(core.UnitRect{Width: 200, Height: 100})
	sa.SetContent(rec)

	sa.HandleMouseMove(core.MouseMoveEvent{X: 40, Y: 8, Buttons: core.LeftButton})
	if !rec.gotMove {
		t.Fatal("content never received the move")
	}
	if rec.moveButtons&core.LeftButton == 0 {
		t.Errorf("move Buttons = %v, want LeftButton preserved (drag-select needs it)", rec.moveButtons)
	}

	sa.HandleMousePress(core.MousePressEvent{X: 40, Y: 8, Button: core.LeftButton, Modifiers: core.ShiftModifier})
	if !rec.gotPress {
		t.Fatal("content never received the press")
	}
	if rec.pressMods&core.ShiftModifier == 0 {
		t.Errorf("press Modifiers = %v, want Shift preserved (shift-click selection)", rec.pressMods)
	}
}
