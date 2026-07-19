package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A plain Left with the caret already at the beginning can't move, but it must
// still collapse an existing selection (caret stays at the start).
func TestLeftAtStartClearsSelection(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("hello")
	// Selection over the whole text, caret parked at the start.
	ti.selStart, ti.selEnd = 0, 5
	ti.cursorPos = 0

	ti.HandleKeyPress(core.KeyPressEvent{Key: "Left"})
	if ti.HasSelection() {
		t.Errorf("Left at start should clear the selection, sel=(%d,%d)", ti.selStart, ti.selEnd)
	}
	if ti.cursorPos != 0 {
		t.Errorf("caret moved to %d, want it to stay at 0", ti.cursorPos)
	}
}

// A plain Right with the caret already at the end collapses an existing
// selection (caret stays at the end).
func TestRightAtEndClearsSelection(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("hello")
	n := len(ti.text)
	ti.selStart, ti.selEnd = 0, n
	ti.cursorPos = n

	ti.HandleKeyPress(core.KeyPressEvent{Key: "Right"})
	if ti.HasSelection() {
		t.Errorf("Right at end should clear the selection, sel=(%d,%d)", ti.selStart, ti.selEnd)
	}
	if ti.cursorPos != n {
		t.Errorf("caret moved to %d, want it to stay at %d", ti.cursorPos, n)
	}
}

// Shift+Left at the start (and Shift+Right at the end) must NOT collapse the
// selection - they keep extending semantics (here, a no-op that preserves it).
func TestShiftArrowAtEdgeKeepsSelection(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("hello")
	n := len(ti.text)

	ti.selStart, ti.selEnd, ti.cursorPos = 0, n, 0
	ti.HandleKeyPress(core.KeyPressEvent{Key: "Left", Modifiers: core.ShiftModifier})
	if !ti.HasSelection() {
		t.Error("Shift+Left at start should keep the selection")
	}

	ti.selStart, ti.selEnd, ti.cursorPos = 0, n, n
	ti.HandleKeyPress(core.KeyPressEvent{Key: "Right", Modifiers: core.ShiftModifier})
	if !ti.HasSelection() {
		t.Error("Shift+Right at end should keep the selection")
	}
}
