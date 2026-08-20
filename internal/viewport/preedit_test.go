package viewport

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
)

func composing(text string, caret, line, rune_ int) *Viewport {
	return covering(text, caret, line, rune_, 0)
}

// covering makes a viewport composing over n committed runes at a position —
// what macOS's press-and-hold palette does, having committed the held letter
// before it opened.
func covering(text string, caret, line, rune_, n int) *Viewport {
	buf := buffer.NewFromString("aaaaaaaaaa\nbbbbbbbbbb\ncccccccccc")
	w := &Viewport{Buffer: buf, Caret: buf.NewCaret()}
	w.SetCursorPos(Position{Line: line, Rune: rune_})
	w.SetPreedit(Preedit{Text: []rune(text), Caret: caret, Covers: n})
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

// A composition stands OVER what it covers and hides it, rather than painting
// in front of it.
//
// This is the half a replacement at commit time never fixed: macOS commits the
// held letter before the palette opens, so a composition painted after it shows
// both at once — "ABCDoò" for as long as the palette is up — where macOS shows
// one character changing.
func TestACompositionHidesWhatItCovers(t *testing.T) {
	w := covering("ò", 1, 0, 5, 1)

	got, lo, hi := w.PreeditSplice(0, "ABCDo")

	if got != "ABCDò" {
		t.Errorf("display line = %q, want the accent in the letter's place", got)
	}
	if lo != 4 || hi != 5 {
		t.Errorf("composition spans [%d,%d), want [4,5) — where the letter was", lo, hi)
	}
}

// Nothing is removed from the line the buffer holds: hiding is a painting
// decision, and ending the composition shows the text again with no edit to
// undo.
func TestHidingTakesNothingFromTheDocument(t *testing.T) {
	w := covering("ò", 1, 0, 5, 1)
	w.ClearPreedit()

	got, lo, hi := w.PreeditSplice(0, "ABCDo")
	if got != "ABCDo" || lo != hi {
		t.Errorf("display = %q [%d,%d), want the letter back untouched", got, lo, hi)
	}
}

// The shift nets out what is hidden against what is shown: a composition
// standing in one rune's place and showing three runes moves later text by two,
// not by three.
func TestTheShiftNetsOutWhatIsHidden(t *testing.T) {
	w := covering("abc", 3, 0, 5, 1)

	if got := w.PreeditShift(0, 5); got != 2 {
		t.Errorf("shift = %d, want three shown less one hidden", got)
	}
	// A position before the hidden run is untouched by either.
	if got := w.PreeditShift(0, 3); got != 0 {
		t.Errorf("shift before the composition = %d, want 0", got)
	}
}

// The caret is measured from where the composition STARTS, which is back at the
// first rune it hides — not from the document position it was opened at.
func TestTheCaretIsMeasuredFromTheCompositionsStart(t *testing.T) {
	w := covering("ò", 1, 0, 5, 1)

	if got := w.PreeditCaretRune(0, 5); got != 5 {
		t.Errorf("caret at display rune %d, want 5 — past the one-rune accent", got)
	}
}

// A composition covering more than the line holds clamps at its start rather
// than reaching into the line before it.
func TestCoveringMoreThanThereIsClampsAtTheLineStart(t *testing.T) {
	w := covering("X", 1, 0, 2, 9)

	got, lo, hi := w.PreeditSplice(0, "ab")
	if got != "X" || lo != 0 || hi != 1 {
		t.Errorf("display = %q [%d,%d), want the whole line stood over", got, lo, hi)
	}
}

// converting makes a viewport composing with the clause an input method is
// currently converting marked — what a Japanese candidate list changes, one
// clause at a time, while the rest of the composition stays as it was typed.
func converting(text string, start, length int) *Viewport {
	buf := buffer.NewFromString("aaaaaaaaaa\nbbbbbbbbbb")
	w := &Viewport{Buffer: buf, Caret: buf.NewCaret()}
	w.SetCursorPos(Position{Line: 0, Rune: 2})
	w.SetPreedit(Preedit{
		Text: []rune(text), Caret: start + length,
		ClauseStart: start, ClauseLen: length,
	})
	return w
}

// The clause is reported against the composition and answered against the
// DISPLAY line, so the renderer can colour it without repeating the offset.
func TestTheClauseLandsInTheDisplayLine(t *testing.T) {
	w := converting("羅なに", 0, 1)

	_, lo, hi := w.PreeditSplice(0, "ab")
	if clauseLo, clauseHi := w.PreeditClauseSpan(lo, hi); clauseLo != 2 || clauseHi != 3 {
		t.Errorf("clause at [%d,%d), want the first composed rune at [2,3)", clauseLo, clauseHi)
	}
}

// No clause is the ordinary case — every composition that is BUILT rather than
// converted reports none — and it means the composition is all one piece.
func TestNoClauseIsAnEmptySpan(t *testing.T) {
	w := converting("きょう", 0, 0)

	_, lo, hi := w.PreeditSplice(0, "ab")
	if clauseLo, clauseHi := w.PreeditClauseSpan(lo, hi); clauseLo != clauseHi {
		t.Errorf("clause at [%d,%d), want nothing marked", clauseLo, clauseHi)
	}
}

// A clause reaching past the composition is trimmed to it: the numbers arrive
// from an input method through a host, in units the SDL3 documentation leaves
// open, and are not trusted past what is there.
func TestAClausePastTheCompositionIsTrimmed(t *testing.T) {
	w := converting("羅なに", 2, 9)

	if got := w.Preedit().ClauseLen; got != 1 {
		t.Errorf("clause length = %d, want it trimmed to the one rune left", got)
	}
	_, lo, hi := w.PreeditSplice(0, "ab")
	if clauseLo, clauseHi := w.PreeditClauseSpan(lo, hi); clauseLo != 4 || clauseHi != 5 {
		t.Errorf("clause at [%d,%d), want the last composed rune at [4,5)", clauseLo, clauseHi)
	}
}
