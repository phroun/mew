package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The I-beam is a promise the pointer will do something here.
//
// A disabled field advertised it anyway, so the pointer said "you can work in
// this" over a field that refuses every edit and, since the focus fix, will
// not even take focus.
func TestDisabledFieldShowsNoIBeam(t *testing.T) {
	ti := NewTextInput()
	if got := ti.CursorShape(); got != core.CursorText {
		t.Errorf("an ordinary field shows cursor %v, want the I-beam", got)
	}

	ti.SetEnabled(false)
	if got := ti.CursorShape(); got != core.CursorDefault {
		t.Errorf("a disabled field shows cursor %v, want the ordinary pointer", got)
	}

	// Read-only keeps it: the text is still selectable with the mouse, which
	// is the thing the I-beam offers.
	ro := NewTextInput()
	ro.SetReadOnly(true)
	if got := ro.CursorShape(); got != core.CursorText {
		t.Errorf("a read-only field shows cursor %v, want the I-beam", got)
	}
}

// A read-only field has a caret position, and moving through it moves that
// position -- which is the state the caret exists to show. It used to be
// painted for an editable field only, so a reader selecting or walking a
// read-only field could not see where they were.
func TestReadOnlyFieldKeepsACaretPosition(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("navigate me")
	ti.SetReadOnly(true)
	ti.SetCursorPosition(0)

	ti.HandleKeyPress(core.KeyPressEvent{Key: "Right"})
	ti.HandleKeyPress(core.KeyPressEvent{Key: "Right"})
	if got := ti.CursorPosition(); got != 2 {
		t.Errorf("cursor = %d after two Rights on a read-only field, want 2", got)
	}

	ti.HandleKeyPress(core.KeyPressEvent{Key: "End"})
	if got := ti.CursorPosition(); got != len("navigate me") {
		t.Errorf("End left the cursor at %d", got)
	}
	// ...and none of that changed the text.
	if got := ti.Text(); got != "navigate me" {
		t.Errorf("navigating a read-only field changed it to %q", got)
	}
}

// The block does not blink. A blink says "type here" and paces itself to a
// keystroke that is not coming, so a read-only field's caret is steady --
// the same rule a cell surface already followed for its block.
func TestReadOnlyCaretDoesNotBlink(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("steady")
	ti.SetReadOnly(true)
	ti.SetFocus()

	if ti.caretTimer != nil {
		t.Error("a read-only field started a blink timer")
	}
	if !ti.caretVisible() {
		t.Error("a read-only caret is not visible; with no timer it should always be")
	}
}
