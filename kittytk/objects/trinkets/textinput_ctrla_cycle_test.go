package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Ctrl+A cycles: caret-to-start -> select-all(caret at end) -> deselect(caret
// at start) -> and round again.
func TestCtrlAHomeCycle(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("hello")
	n := len(ti.text)

	// Start from the middle.
	ti.cursorPos = 3
	ti.selStart, ti.selEnd = 0, 0

	ctrlA := func() { ti.HandleKeyPress(core.KeyPressEvent{Key: "^A"}) }

	// 1st press: caret to the beginning, no selection.
	ctrlA()
	if ti.cursorPos != 0 || ti.HasSelection() {
		t.Fatalf("1st ^A: pos=%d hasSel=%v, want pos=0 no-selection", ti.cursorPos, ti.HasSelection())
	}

	// 2nd press (already at start): select all, caret to the end.
	ctrlA()
	if ti.selStart != 0 || ti.selEnd != n || ti.cursorPos != n {
		t.Fatalf("2nd ^A: sel=(%d,%d) pos=%d, want select-all with caret at %d",
			ti.selStart, ti.selEnd, ti.cursorPos, n)
	}

	// 3rd press (all selected): deselect, caret back to the beginning.
	ctrlA()
	if ti.cursorPos != 0 || ti.HasSelection() {
		t.Fatalf("3rd ^A: pos=%d hasSel=%v, want pos=0 no-selection", ti.cursorPos, ti.HasSelection())
	}

	// 4th press: cycles back to select-all.
	ctrlA()
	if ti.selStart != 0 || ti.selEnd != n || ti.cursorPos != n {
		t.Fatalf("4th ^A: sel=(%d,%d) pos=%d, want select-all again", ti.selStart, ti.selEnd, ti.cursorPos)
	}
}

// The very first Ctrl+A from a non-start caret always just goes to the start,
// even when there is an unrelated partial selection.
func TestCtrlAFromPartialSelectionGoesToStart(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("hello world")
	// A partial selection in the middle, caret not at the start.
	ti.selStart, ti.selEnd = 2, 5
	ti.cursorPos = 5

	ti.HandleKeyPress(core.KeyPressEvent{Key: "^A"})
	if ti.cursorPos != 0 || ti.HasSelection() {
		t.Fatalf("^A with partial selection: pos=%d hasSel=%v, want pos=0 no-selection",
			ti.cursorPos, ti.HasSelection())
	}
}

// Shift+Ctrl+A is unaffected by the cycle: it extends the selection to the
// beginning.
func TestShiftCtrlAStillExtends(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("hello")
	ti.cursorPos = 4
	ti.selStart, ti.selEnd = 4, 4

	ti.HandleKeyPress(core.KeyPressEvent{Key: "^A", Modifiers: core.ShiftModifier})
	if ti.cursorPos != 0 || ti.selEnd != 0 {
		t.Fatalf("Shift+^A: pos=%d selEnd=%d, want extend-to-start (pos=0, selEnd=0)",
			ti.cursorPos, ti.selEnd)
	}
}
