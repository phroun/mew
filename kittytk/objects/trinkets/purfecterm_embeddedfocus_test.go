package trinkets

import "testing"

// A terminal hosted inside another focused trinket never enters the focus
// chain, so it has no focus of its own to hold: its host declares it. What it
// declares has to reach everything that behaves differently when focused —
// the cursor form and blink (gfxFocused), the platform caret, and the
// emulator's own flag, which is what a child process sees when it asked for
// focus reporting.
func TestEmbeddedFocusIsTheWholeNotionOfFocus(t *testing.T) {
	term := NewPurfecTerm()
	term.Init(term)

	if term.focused() || term.gfxFocused() {
		t.Fatal("a fresh unfocused terminal should not report focus")
	}
	if term.terminal != nil && term.terminal.IsFocused() {
		t.Fatal("the emulator should start unfocused too")
	}

	term.SetEmbeddedFocus(true)
	if !term.EmbeddedFocus() || !term.focused() {
		t.Error("SetEmbeddedFocus(true) should make the terminal report focus")
	}
	// The window chain is active for a parentless child, so the graphical
	// cursor takes its focused form instead of the hollow box.
	if !term.gfxFocused() {
		t.Error("the graphical cursor should take its focused form")
	}
	if term.terminal != nil && !term.terminal.IsFocused() {
		t.Error("the emulator's own focused flag should follow")
	}

	term.SetEmbeddedFocus(false)
	if term.focused() || term.gfxFocused() {
		t.Error("clearing it should put the terminal back to unfocused")
	}
	if term.terminal != nil && term.terminal.IsFocused() {
		t.Error("the emulator's flag should clear with it")
	}
}
