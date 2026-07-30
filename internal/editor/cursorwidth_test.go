package editor

import (
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// The painted hardware cursor must land on the terminal column the RENDERED
// row actually produces: well-formed combining marks and zero-width
// characters advance no columns, wide characters advance two, and a
// DEFECTIVE mark advances the width of the substitute painted in its place.
func TestRenderedCursorColumnComplexText(t *testing.T) {
	const comb = "́" // combining acute (Inherited: rides any base)
	const dot = "֗"  // hebrew accent revia (Hebrew: needs a Hebrew base)
	const zw = "​"   // zero-width space
	content := "e" + comb + "x" + zw + "日" + dot + "k\n"
	// Prefix-sum columns per cursor rune position; both sides of a
	// zero-width rune share a column (the mark overlays the base's cell).
	// The Hebrew accent sits on a CJK ideograph — an ill-formed sequence no
	// shaper composes — so it is lifted onto a dotted circle of its own
	// (U+25CC, one cell) rather than promised as zero-width and then rendered
	// by the terminal as a spacing fallback that slides the rest of the row
	// off the window. See textwidth.AnchorMark.
	wantCol := []int{0, 1, 1, 2, 2, 4, 5, 6}

	for runePos, want := range wantCol {
		e, w, out := newRenderedEditor(t, content)
		w.SetCursorPos(viewport.Position{Line: 0, Rune: runePos})
		e.performRender()
		_, col := lastCursor(out.Bytes())
		// No line numbers, no margins: screen column = visual column + 1.
		if col != want+1 {
			t.Errorf("rune %d: hardware cursor col %d, want %d", runePos, col, want+1)
		}
	}
}

// With the ruler and line numbers on (the common configuration), the cursor
// row is offset below the ruler and the column past the gutter.
func TestRenderedCursorWithRulerAndLineNumbers(t *testing.T) {
	e, w, out := newRenderedEditor(t, "hello\nworld\n")
	w.ViewState.ShowRuler = true
	w.ViewState.ShowLineNumbers = true
	w.SetCursorPos(viewport.Position{Line: 1, Rune: 3})
	e.performRender()
	row, col := lastCursor(out.Bytes())
	// Ruler takes row 1; line 1 renders on row 3. Gutter is LineNumWidth
	// wide; the cursor's screen column is 1 + gutter + rune column.
	if row != 3 {
		t.Errorf("cursor row %d, want 3 (below the ruler)", row)
	}
	wantCol := 1 + w.LineNumWidth + 3
	if col != wantCol {
		t.Errorf("cursor col %d, want %d", col, wantCol)
	}
}

// A caret PARTWAY THROUGH a cluster is placed as though it followed the whole
// cluster. Those positions have no cell of their own — the marks ride the base —
// so the caret has to borrow one, and the cluster's trailing edge is the honest
// choice: the caret is logically past the base, and the leading edge would draw
// it on the far side of a character it has already gone by.
//
// Left-to-right text always did this, because that walk is a prefix sum and a
// prefix sum already lands past the cluster. The bidi walk stepped BACK to the
// cluster base instead, which in a right-to-left run is the opposite side — so
// the caret appeared before the letter it had just passed.
func TestCaretInsideAClusterFollowsIt(t *testing.T) {
	const lamed, hiriq, alef = "ל", "ִ", "א"

	e, w := newTestEditor(t, alef+lamed+hiriq+alef+"\n", "direction=rtl")
	line := alef + lamed + hiriq + alef

	onLamed := e.caretVisualColumn(w, line, 1, 4)  // before the lamed
	inside := e.caretVisualColumn(w, line, 2, 4)   // between lamed and its point
	pastMark := e.caretVisualColumn(w, line, 3, 4) // past the whole cluster

	if inside != pastMark {
		t.Errorf("caret inside the cluster at col %d, want col %d (where past it sits)",
			inside, pastMark)
	}
	// In an RTL run reading runs right to left, so "past the cluster" is to its
	// LEFT: a smaller column than the letter's own cell.
	if inside >= onLamed {
		t.Errorf("caret inside the cluster at col %d, want left of the letter at col %d",
			inside, onLamed)
	}
}

// The same rule in LEFT-to-right text, which is where it already held: the
// prefix-sum walk puts a mid-cluster caret past the base, and the change must
// not have disturbed that.
func TestCaretInsideAClusterFollowsItLTR(t *testing.T) {
	const comb = "́" // combining acute
	e, w := newTestEditor(t, "e"+comb+"x\n")
	line := "e" + comb + "x"

	onE := e.caretVisualColumn(w, line, 0, 4)
	inside := e.caretVisualColumn(w, line, 1, 4)   // between e and its accent
	pastMark := e.caretVisualColumn(w, line, 2, 4) // past the cluster

	if inside != pastMark {
		t.Errorf("caret inside the cluster at col %d, want col %d", inside, pastMark)
	}
	if inside <= onE {
		t.Errorf("caret inside the cluster at col %d, want right of the base at col %d", inside, onE)
	}
}
