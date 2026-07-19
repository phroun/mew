package editor

import (
	"testing"

	"github.com/phroun/mew/internal/window"
)

// showMarks inserts a "*" cell at each mark position; the caret and click column
// math must count those inserted cells so the caret lands on the right cell and
// clicks/round-trips stay aligned.
func TestShowMarksColumnMath(t *testing.T) {
	e, w := newTestEditor(t, "abcd\n")

	// A mark between 'b' and 'c' (rune position 2).
	if err := w.Buffer.SetMark("m", 0, 2); err != nil {
		t.Fatalf("SetMark: %v", err)
	}
	if cols := w.Buffer.MarksOnLine(0); len(cols) != 1 || cols[0] != 2 {
		t.Fatalf("MarksOnLine(0) = %v, want [2]", cols)
	}

	// showMarks off: no offset.
	w.ViewState.ShowMarks = false
	if got := e.caretVisualColumn(w, "abcd", 3, 4); got != 3 {
		t.Fatalf("off: caretVisualColumn(3) = %d, want 3", got)
	}

	// showMarks on: visual layout is [a@0][b@1][*@2][c@3][d@4].
	w.ViewState.ShowMarks = true
	w.SetCursorPos(window.Position{Line: 0, Rune: 0}) // marks are read from the caret line
	for _, c := range []struct{ rune_, want int }{
		{0, 0}, {1, 1}, {2, 3}, {3, 4}, {4, 5},
	} {
		if got := e.caretVisualColumn(w, "abcd", c.rune_, 4); got != c.want {
			t.Errorf("caretVisualColumn(%d) = %d, want %d", c.rune_, got, c.want)
		}
	}
	// Inverse round-trips, including a click on the "*" cell (column 2 -> rune 2).
	for _, c := range []struct{ col, wantRune int }{
		{0, 0}, {1, 1}, {2, 2}, {3, 2}, {4, 3}, {5, 4},
	} {
		if got := e.visualColumnToRune(w, "abcd", c.col, 4); got != c.wantRune {
			t.Errorf("visualColumnToRune(%d) = %d, want %d", c.col, got, c.wantRune)
		}
	}
}

// A mark that precedes a tab must not be counted as a flat +1 shift: inserting
// the "*" cell moves the tab's start column, so the tab shrinks to the same
// stop and the net shift past it is absorbed. The column math must walk the
// cells inline (like the renderer) rather than add a constant. Covers the
// reported "mark at the first position / before a tab" breakage.
func TestShowMarksTabColumnMath(t *testing.T) {
	// Leading tab with a mark at the very first position (rune 0), tabSize 4.
	// Visual cells: [*@0][tab@1,2,3][x@4]. The "*" steals column 0, so the tab
	// runs cols 1..3 (width 3) and 'x' lands at col 4 — NOT col 5.
	t.Run("leading tab", func(t *testing.T) {
		e, w := newTestEditor(t, "\tx\n")
		if err := w.Buffer.SetMark("m", 0, 0); err != nil {
			t.Fatalf("SetMark: %v", err)
		}
		w.ViewState.ShowMarks = true
		w.SetCursorPos(window.Position{Line: 0, Rune: 0})

		for _, c := range []struct{ rune_, want int }{
			{0, 1}, // tab cell, one past its leading "*"
			{1, 4}, // 'x'
			{2, 5}, // end of line
		} {
			if got := e.caretVisualColumn(w, "\tx", c.rune_, 4); got != c.want {
				t.Errorf("caretVisualColumn(%d) = %d, want %d", c.rune_, got, c.want)
			}
		}
		for _, c := range []struct{ col, wantRune int }{
			{0, 0}, {1, 0}, {2, 0}, {3, 0}, // "*" and the tab body all map to the tab
			{4, 1}, {5, 2},
		} {
			if got := e.visualColumnToRune(w, "\tx", c.col, 4); got != c.wantRune {
				t.Errorf("visualColumnToRune(%d) = %d, want %d", c.col, got, c.wantRune)
			}
		}
	})

	// Mark between 'a' and a tab (rune 1). Cells: [a@0][*@1][tab@2,3][b@4].
	t.Run("interior tab", func(t *testing.T) {
		e, w := newTestEditor(t, "a\tb\n")
		if err := w.Buffer.SetMark("m", 0, 1); err != nil {
			t.Fatalf("SetMark: %v", err)
		}
		w.ViewState.ShowMarks = true
		w.SetCursorPos(window.Position{Line: 0, Rune: 0})

		for _, c := range []struct{ rune_, want int }{
			{0, 0}, // 'a'
			{1, 2}, // tab cell, past its leading "*"
			{2, 4}, // 'b'
			{3, 5}, // end of line
		} {
			if got := e.caretVisualColumn(w, "a\tb", c.rune_, 4); got != c.want {
				t.Errorf("caretVisualColumn(%d) = %d, want %d", c.rune_, got, c.want)
			}
		}
		for _, c := range []struct{ col, wantRune int }{
			{0, 0}, {1, 1}, {2, 1}, {3, 1}, {4, 2}, {5, 3},
		} {
			if got := e.visualColumnToRune(w, "a\tb", c.col, 4); got != c.wantRune {
				t.Errorf("visualColumnToRune(%d) = %d, want %d", c.col, got, c.wantRune)
			}
		}
	})

	// Forward/inverse must be mutual inverses at every rune boundary for a line
	// mixing several tabs and marks — the general "intermixed tabs and marks"
	// case. Each rune's caret column must map back to that rune.
	t.Run("round trip mixed", func(t *testing.T) {
		e, w := newTestEditor(t, "\ta\t\tbc\n")
		line := "\ta\t\tbc"
		for _, p := range []int{0, 2, 3, 5} { // marks before tabs and letters
			if err := w.Buffer.SetMark("m", 0, p); err != nil {
				t.Fatalf("SetMark(%d): %v", p, err)
			}
		}
		w.ViewState.ShowMarks = true
		w.SetCursorPos(window.Position{Line: 0, Rune: 0})

		for r := 0; r <= len([]rune(line)); r++ {
			col := e.caretVisualColumn(w, line, r, 4)
			if back := e.visualColumnToRune(w, line, col, 4); back != r {
				t.Errorf("round trip: rune %d -> col %d -> rune %d", r, col, back)
			}
		}
	})
}
