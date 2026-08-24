package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The space bar types a space into a text field rather than submitting it.
//
// It shares a key with activation, and a text field has to offer activation so
// that Return can submit -- so without a command of its own the space bar
// resolved to trinket_activate and fired the submit handler, inserting nothing.
// The key layer names the space bar "Space", five runes, so the typing path
// (which inserts a one-rune key name as itself) sees no character either.
func TestSpaceBarTypesASpace(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("ab")
	ti.SetCursorPosition(2)

	completed := 0
	ti.SetOnComplete(func() { completed++ })

	if !ti.HandleKeyPress(core.KeyPressEvent{Key: "Space"}) {
		t.Fatal("the field declined the space bar")
	}
	if got := ti.Text(); got != "ab " {
		t.Errorf("text = %q, want %q", got, "ab ")
	}
	if completed != 0 {
		t.Errorf("the space bar completed the field %d time(s); it should only type", completed)
	}
	if got := ti.CursorPosition(); got != 3 {
		t.Errorf("cursor = %d, want 3 — one rune in", got)
	}
}

// Return still completes the field. A text field offers no edit command, so Return falls
// through to activate -- the reason the field has to offer activate at all,
// and therefore the reason the space bar needed saying out loud.
func TestReturnStillCompletes(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("ab")

	completed := 0
	ti.SetOnComplete(func() { completed++ })

	ti.HandleKeyPress(core.KeyPressEvent{Key: "Return"})
	if completed != 1 {
		t.Errorf("Return completed the field %d time(s), want 1", completed)
	}
	if got := ti.Text(); got != "ab" {
		t.Errorf("Return changed the text to %q", got)
	}
}

// The space bar goes through the ordinary insert, so it replaces a selection
// the way any other typed character does.
func TestSpaceBarReplacesTheSelection(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("hello")
	ti.SelectAll()

	ti.HandleKeyPress(core.KeyPressEvent{Key: "Space"})
	if got := ti.Text(); got != " " {
		t.Errorf("text = %q, want the selection replaced by a space", got)
	}
}

// ...and a read-only field types nothing, again because it is the same insert.
func TestSpaceBarTypesNothingWhenReadOnly(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("ab")
	ti.SetCursorPosition(2)
	ti.SetReadOnly(true)

	ti.HandleKeyPress(core.KeyPressEvent{Key: "Space"})
	if got := ti.Text(); got != "ab" {
		t.Errorf("a read-only field typed a space: %q", got)
	}
}

// The command is claimed only by trinkets that accept text. A button offers
// activate and not trinket_type_space, so the space bar still activates it --
// which is what the whole first-match-wins ordering on the Space line is for.
func TestSpaceStillActivatesAButton(t *testing.T) {
	if got := NewButton("OK").KeyCommand("Space"); got != core.CmdTrinketActivate {
		t.Errorf("a button resolves Space to %q, want %s", got, core.CmdTrinketActivate)
	}
	if got := NewTextInput().KeyCommand("Space"); got != core.CmdTrinketTypeSpace {
		t.Errorf("a text field resolves Space to %q, want %s", got, core.CmdTrinketTypeSpace)
	}
}
