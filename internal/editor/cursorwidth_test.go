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
	// shaper composes — so it is classified like a control code and painted
	// as "0597", four columns wide, rather than promised as zero-width and
	// then rendered by the terminal as a spacing fallback that slides the
	// rest of the row off the window. See textwidth.DefectiveMark.
	wantCol := []int{0, 1, 1, 2, 2, 4, 8, 9}

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
