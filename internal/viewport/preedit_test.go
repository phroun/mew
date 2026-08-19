package viewport

import "testing"

func composing(text string, caret, line, rune_ int) *Viewport {
	w := &Viewport{}
	w.SetPreedit(Preedit{Text: []rune(text), Caret: caret, Line: line, Rune: rune_})
	return w
}

// The composition is synthesized into the line at the caret, and from there it
// is ordinary text — which is the whole point. The existing width model
// measures it, so nothing downstream needs a preedit-aware twin of the column
// arithmetic.
func TestTheCompositionIsSplicedInAtTheCaret(t *testing.T) {
	w := composing("きょう", 3, 2, 4)

	got, lo, hi := w.PreeditSplice(2, "abcdef")

	if got != "abcdきょうef" {
		t.Errorf("display line = %q, want the composition at the caret", got)
	}
	if lo != 4 || hi != 7 {
		t.Errorf("composition occupies [%d,%d), want [4,7)", lo, hi)
	}
}

// Every line but the one being composed on comes back untouched, and says so
// with an empty range.
func TestOtherLinesAreUntouched(t *testing.T) {
	w := composing("きょう", 3, 2, 4)

	got, lo, hi := w.PreeditSplice(3, "abcdef")

	if got != "abcdef" || lo != hi {
		t.Errorf("line 3 = %q [%d,%d), want it untouched", got, lo, hi)
	}
}

// A composition whose position has been left behind by an edit lands at the end
// of the line rather than out of range. The alternative is a panic in the
// painter, on a value that arrives from outside.
func TestAStrandedCompositionClampsToTheLine(t *testing.T) {
	w := composing("ò", 1, 0, 99)

	got, lo, hi := w.PreeditSplice(0, "ab")

	if got != "abò" || lo != 2 || hi != 3 {
		t.Errorf("display = %q [%d,%d), want it clamped onto the end", got, lo, hi)
	}
}

// One offset is the whole document-to-display mapping, because the composition
// is one contiguous run at one known place. Text the caret was in front of
// stays in front of it.
func TestTheShiftIsOneOffsetNotAMap(t *testing.T) {
	w := composing("きょう", 3, 0, 4)

	for _, c := range []struct {
		doc, want int
	}{
		{0, 0}, {3, 0}, // before the composition: unmoved
		{4, 3}, {6, 3}, // at it or after: pushed by its length
	} {
		if got := w.PreeditShift(0, c.doc); got != c.want {
			t.Errorf("shift at doc rune %d = %d, want %d", c.doc, got, c.want)
		}
	}
}

// The caret paints INSIDE the composition, at the input method's own cursor.
// That is what shows progress through a long one; parking at either end would
// claim the composition was finished when it is not.
func TestTheCaretSitsAtTheInputMethodsOwnCursor(t *testing.T) {
	w := composing("きょう", 1, 0, 4)

	if got := w.PreeditCaretRune(0, 4); got != 5 {
		t.Errorf("caret at display rune %d, want 5 — one into the composition", got)
	}

	// A document position that is not the composition's own is merely shifted.
	if got := w.PreeditCaretRune(0, 6); got != 9 {
		t.Errorf("a later position mapped to %d, want 9", got)
	}
}

// Ending a composition leaves nothing behind: cancelling shows the document as
// it always was, because nothing provisional was ever in it.
func TestClearingEndsIt(t *testing.T) {
	w := composing("きょう", 3, 0, 1)
	w.ClearPreedit()

	if w.Preedit().Active() {
		t.Error("a cleared composition still reports itself active")
	}
	got, lo, hi := w.PreeditSplice(0, "ab")
	if got != "ab" || lo != hi {
		t.Errorf("display = %q [%d,%d), want the plain line back", got, lo, hi)
	}
}

// A caret past the end of the composition is clamped rather than trusted: it
// arrives from an input method through a host, and PreeditCaretRune would
// otherwise put the cursor beyond the cells that exist.
func TestAnOutOfRangeCaretIsClamped(t *testing.T) {
	w := composing("ab", 99, 0, 0)

	if got := w.Preedit().Caret; got != 2 {
		t.Errorf("caret = %d, want it clamped to the composition's length", got)
	}
}
