package editor

import "testing"

// The accent palette replaces the letter it opened over.
//
// macOS commits a held letter the moment the key goes down, so by the time an
// accent is chosen the "o" is already in the document. The host says how many
// characters the composition stands in for, because only the host can know: it
// watched the key commit the letter and watched an input method take the key
// over. Without the count the buffer keeps both.
func TestReplacePriorStandsInForTheLetterBeforeTheCaret(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`insert "ABCDo"`)

	e.executeCommand(`replace_prior 1, 'ò'`)

	if got := docContent(w); got != "ABCDò" {
		t.Errorf("content = %q, want the accent in place of the letter", got)
	}
	if got := w.CursorPos().Rune; got != 5 {
		t.Errorf("caret at rune %d, want 5 — one character out, one in", got)
	}
}

// Replacing nothing is an ordinary insert. That is what every composition which
// appends rather than replaces sends — a CJK candidate committing, and any host
// that cannot work out a replacement count at all.
func TestReplacePriorWithNothingToReplaceInserts(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`insert "ab"`)

	e.executeCommand(`replace_prior 0, 'きょう'`)

	if got := docContent(w); got != "abきょう" {
		t.Errorf("content = %q, want the composition appended", got)
	}
}

// Picking an accent is ONE user action, so one undo takes it back to the plain
// letter.
//
// The state between the halves — the "o" gone, the "ò" not yet arrived — is one
// nobody ever saw: the letter was there before the palette opened and the
// accented one after it closed. Undo should step between states that were on
// screen, so the replacement is a single mutation rather than a delete and an
// insert.
func TestReplacePriorUndoesInOneStep(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`insert "ABCDo"`)
	e.executeCommand("go_line_end") // bake, so the typing run is not part of this
	e.executeCommand(`replace_prior 1, 'ò'`)

	if got := docContent(w); got != "ABCDò" {
		t.Fatalf("content = %q before undo", got)
	}
	if !w.Buffer.Undo() {
		t.Fatal("expected an undo step for the replacement")
	}
	if got := docContent(w); got != "ABCDo" {
		t.Errorf("one undo gave %q, want the plain letter back in one step", got)
	}
}

// A count larger than what is there takes what is there and stops at the start
// of the line. The number is inferred by the host from what it watched, so it
// is not something to trust into a panic — and the replacement never has to
// cross a line boundary, because the character a palette replaces is the one
// its own key just typed.
func TestReplacePriorCannotReachPastTheLineStart(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`insert "ab"`)
	e.executeCommand("go_char_prior") // caret between a and b

	e.executeCommand(`replace_prior 9, 'X'`)

	if got := docContent(w); got != "Xb" {
		t.Errorf("content = %q, want only the one character before the caret gone", got)
	}
}

// A malformed count changes nothing rather than guessing at one.
func TestReplacePriorRefusesANonsenseCount(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand(`insert "ab"`)

	e.executeCommand(`replace_prior -3, 'X'`)
	e.executeCommand(`replace_prior nonsense, 'X'`)

	if got := docContent(w); got != "ab" {
		t.Errorf("content = %q, want it untouched", got)
	}
}
