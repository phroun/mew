package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// A field completed says so, once, whatever key said it.
//
// The field had a Go callback for this and no wire event at all, so `change`
// was the only thing a client could hear: it could watch every keystroke and
// never learn the person was finished.
func TestCompleteFiresOnReturn(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("hello")

	completed := 0
	ti.SetOnComplete(func() { completed++ })

	if !ti.HandleKeyPress(core.KeyPressEvent{Key: "Return"}) {
		t.Fatal("the field declined Return")
	}
	if completed != 1 {
		t.Errorf("completed %d time(s), want 1", completed)
	}
	// Completing is not leaving: the field still holds what it held.
	if got := ti.Text(); got != "hello" {
		t.Errorf("text = %q; completing must not disturb the content", got)
	}
}

// The space bar types, and does NOT complete, even though a field offers
// trinket_activate and the default keymap puts activate on Space.
//
// This is the table's doing, not the field's: Space is written
// trinket_type_space first and trinket_activate second, and a context takes
// the first meaning the trinket offers. Nothing at this end re-decides it,
// which is why the test asserts the outcome rather than the mechanism.
func TestSpaceTypesRatherThanCompletes(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("ab")
	ti.SetCursorPosition(2)

	completed := 0
	ti.SetOnComplete(func() { completed++ })

	ti.HandleKeyPress(core.KeyPressEvent{Key: "Space"})
	if completed != 0 {
		t.Errorf("the space bar completed the field %d time(s)", completed)
	}
	if got := ti.Text(); got != "ab " {
		t.Errorf("text = %q, want %q", got, "ab ")
	}
}

// The precedence that protects the space bar is a fact about the keymap, so it
// is checked there: Space means typing BEFORE it means activation, and a
// context offering both keeps the first.
func TestKeymapPutsTypingAheadOfActivationOnSpace(t *testing.T) {
	r := core.DefaultKeyRegistry()
	ctx := r.BuildContext([]string{core.CmdTrinketTypeSpace, core.CmdTrinketActivate})
	if got := ctx.Resolve("Space"); got != core.CmdTrinketTypeSpace {
		t.Errorf("Space -> %q for a context offering both, want %s",
			got, core.CmdTrinketTypeSpace)
	}
	// ...and a trinket offering only activation still activates on it, which
	// is what keeps a button working.
	only := r.BuildContext([]string{core.CmdTrinketActivate})
	if got := only.Resolve("Space"); got != core.CmdTrinketActivate {
		t.Errorf("Space -> %q for a context offering only activate, want %s",
			got, core.CmdTrinketActivate)
	}
}

// The event reaches the wire, carrying the content so a handler need not read
// it back across the seam.
func TestCompleteReachesTheWire(t *testing.T) {
	f, events := buildWithEvents(t, nil, `new textinput text="typed"`)
	ti := f.targets[0].(*TextInput)
	*events = nil

	ti.HandleKeyPress(core.KeyPressEvent{Key: "Return"})

	got := eventsOfType(*events, "complete")
	if len(got) != 1 {
		t.Fatalf("complete events = %d, want 1", len(got))
	}
	if id, ok := got[0].Trinket(); !ok || id != uint64(ti.ObjectID()) {
		t.Errorf("complete named trinket %d, want %d", id, ti.ObjectID())
	}
	if text, ok := got[0].Text("text"); !ok || text != "typed" {
		t.Errorf("complete carried text %q, want %q", text, "typed")
	}
}

// ...and it is declared, so a client can subscribe to it by name.
func TestCompleteIsDeclared(t *testing.T) {
	if !protocol.TypeEmits("textinput", "complete") {
		t.Errorf("textinput does not declare complete; it declares %v",
			protocol.EventNames("textinput"))
	}
}
