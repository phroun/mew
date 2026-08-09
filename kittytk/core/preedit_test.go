package core

import "testing"

// An empty update is not a composition. Input methods send one to end a
// composition, and a field must be able to tell that from a live one.
func TestEmptyPreeditIsNotActive(t *testing.T) {
	p := PreeditFrom(TextEditingEvent{Text: "", Start: -1, Length: -1})
	if p.Active() {
		t.Error("empty text produced an active composition")
	}
	if len(p.Text) != 0 {
		t.Errorf("text = %q, want empty", string(p.Text))
	}
}

// The overwhelmingly common case: an input method that reports no
// position at all. The caret belongs at the end, where the next
// keystroke extends.
func TestPreeditWithoutPositionPutsCaretAtEnd(t *testing.T) {
	p := PreeditFrom(TextEditingEvent{Text: "きょう", Start: -1, Length: -1})
	if !p.Active() {
		t.Fatal("composition not active")
	}
	if p.Caret != 3 {
		t.Errorf("caret = %d, want 3 (end)", p.Caret)
	}
	if p.ClauseLen != 0 {
		t.Errorf("clause length = %d, want none", p.ClauseLen)
	}
}

// Length <= 0 with a position means that position IS the caret, not the
// start of a zero-width clause.
func TestPreeditZeroLengthIsACaretNotAClause(t *testing.T) {
	p := PreeditFrom(TextEditingEvent{Text: "nihon", Start: 2, Length: 0})
	if p.Caret != 2 {
		t.Errorf("caret = %d, want 2", p.Caret)
	}
	if p.ClauseLen != 0 {
		t.Errorf("clause length = %d, want none", p.ClauseLen)
	}
}

// A reported clause is the segment being converted, and the caret goes
// at its far end - where typing continues.
func TestPreeditClauseSetsCaretAtItsEnd(t *testing.T) {
	p := PreeditFrom(TextEditingEvent{Text: "きょうは", Start: 1, Length: 2})
	if p.ClauseStart != 1 || p.ClauseLen != 2 {
		t.Errorf("clause = [%d,+%d), want [1,+2)", p.ClauseStart, p.ClauseLen)
	}
	if p.Caret != 3 {
		t.Errorf("caret = %d, want 3 (clause end)", p.Caret)
	}
}

// Start/Length are measured in units SDL3 does not pin down, so a
// backend counting UTF-8 BYTES against a multi-byte composition hands us
// indices past the end. That has to clamp: a wrong-looking clause is a
// cosmetic problem, an out-of-range slice is a crash in the paint path.
func TestPreeditClampsIndicesFromAByteCountingBackend(t *testing.T) {
	// "きょう" is 3 runes but 9 bytes.
	p := PreeditFrom(TextEditingEvent{Text: "きょう", Start: 3, Length: 6})
	if p.ClauseStart < 0 || p.ClauseStart > 3 {
		t.Errorf("clause start = %d, outside the composition", p.ClauseStart)
	}
	if p.ClauseStart+p.ClauseLen > 3 {
		t.Errorf("clause ends at %d, past the composition's 3 runes",
			p.ClauseStart+p.ClauseLen)
	}
	if p.Caret < 0 || p.Caret > 3 {
		t.Errorf("caret = %d, outside the composition", p.Caret)
	}
}

// Negative indices are SDL's "unset" marker on either field, and a
// negative Start must not survive into a slice bound.
func TestPreeditNegativeStartWithLengthIsIgnored(t *testing.T) {
	p := PreeditFrom(TextEditingEvent{Text: "abc", Start: -1, Length: 2})
	if p.ClauseLen != 0 {
		t.Errorf("clause length = %d, want none when the start is unset", p.ClauseLen)
	}
	if p.Caret != 3 {
		t.Errorf("caret = %d, want 3 (end)", p.Caret)
	}
}

// The composition is measured in runes throughout, never bytes - a text
// field slices it by index to paint the clause.
func TestPreeditCountsRunesNotBytes(t *testing.T) {
	p := PreeditFrom(TextEditingEvent{Text: "日本語", Start: -1, Length: -1})
	if len(p.Text) != 3 {
		t.Errorf("len = %d, want 3 runes (not 9 bytes)", len(p.Text))
	}
}
