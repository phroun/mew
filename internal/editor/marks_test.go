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
