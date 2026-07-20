package editor

import (
	"fmt"
	"strings"
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
	if cols := w.Buffer.MarksOnLine(0, false); len(cols) != 1 || cols[0] != 2 {
		t.Fatalf("MarksOnLine(0) = %v, want [2]", cols)
	}

	// showMarks off: no offset.
	w.ViewState.ShowMarks = "no"
	if got := e.caretVisualColumn(w, "abcd", 3, 4); got != 3 {
		t.Fatalf("off: caretVisualColumn(3) = %d, want 3", got)
	}

	// showMarks on: visual layout is [a@0][b@1][*@2][c@3][d@4].
	w.ViewState.ShowMarks = "yes"
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
		w.ViewState.ShowMarks = "yes"
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
		w.ViewState.ShowMarks = "yes"
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
			if err := w.Buffer.SetMark(fmt.Sprintf("m%d", p), 0, p); err != nil {
				t.Fatalf("SetMark(%d): %v", p, err)
			}
		}
		if cols := w.Buffer.MarksOnLine(0, false); len(cols) != 4 {
			t.Fatalf("want 4 distinct marks, got %v", cols)
		}
		w.ViewState.ShowMarks = "yes"
		w.SetCursorPos(window.Position{Line: 0, Rune: 0})

		for r := 0; r <= len([]rune(line)); r++ {
			col := e.caretVisualColumn(w, line, r, 4)
			if back := e.visualColumnToRune(w, line, col, 4); back != r {
				t.Errorf("round trip: rune %d -> col %d -> rune %d", r, col, back)
			}
		}
	})
}

// A mark at end of line (rune position len, past the last character) shows even
// with invisibles off: on a plain line the renderer appends a trailing "*" cell
// and the caret math reserves it, so the final mark on the line is visible.
func TestShowMarksEndOfLine(t *testing.T) {
	e, w, out := newRenderedEditor(t, "abc\n")
	w.ViewState.ShowMarks = "yes"
	if err := w.Buffer.SetMark("m", 0, 3); err != nil { // past 'c'
		t.Fatalf("SetMark: %v", err)
	}
	if cols := w.Buffer.MarksOnLine(0, false); len(cols) != 1 || cols[0] != 3 {
		t.Fatalf("MarksOnLine(0) = %v, want [3]", cols)
	}

	// Invisibles OFF: the "*" is appended right after the content.
	e.performRender()
	if plain := stripAnsi(out.String()); !strings.Contains(plain, "abc*") {
		t.Fatalf("EOL mark should append a trailing '*' with invisibles off: %q", plain)
	}

	// Caret math reserves the cell: the "*" is at col 3, the EOL caret one past.
	if got := e.caretVisualColumn(w, "abc", 3, 4); got != 4 {
		t.Errorf("EOL caret with trailing mark: col %d, want 4", got)
	}
	if got := e.visualColumnToRune(w, "abc", 3, 4); got != 3 {
		t.Errorf("click on the trailing '*' (col 3): rune %d, want 3", got)
	}

	// The renderer's own cursor placement agrees: caret at EOL sits at visual
	// col 4 -> screen col 5 (no gutter).
	w.SetCursorPos(window.Position{Line: 0, Rune: 3})
	e.afterHorizontalMovement(w)
	out.Reset()
	e.performRender()
	if _, col := lastCursor(out.Bytes()); col != 5 {
		t.Errorf("hardware cursor past the trailing mark: col %d, want 5", col)
	}
}

// bidiMarkLine is bidiLine "abc שלום xyz": logical 0-2 "abc", 3 space, 4-7 the
// Hebrew run (ש,ל,ו,ם) painted reversed at cols 4-7, 8 space, 9-11 "xyz".
//
// On a bidirectional line the showMarks "*" is inserted in VISUAL order, just
// left of the marked rune's painted cell (exactly as prepareLineForDisplay
// draws it) — NOT by a flat reading-order offset, which would land it on the
// wrong side of a reversed rune. A mark at logical 5 (ל, painted at col 6)
// inserts a "*" at col 6 and shifts ל and everything visually right of the "*"
// one cell over; cells left of it (ו col 5, ם col 4) are untouched.
func TestShowMarksBidiColumnMath(t *testing.T) {
	e, w := newTestEditor(t, bidiLine+"\n")
	w.ViewState.ShowMarks = "yes"
	if err := w.Buffer.SetMark("m", 0, 5); err != nil {
		t.Fatalf("SetMark: %v", err)
	}
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})

	for _, c := range []struct{ rune_, want int }{
		{7, 4},                      // ם — left of the "*", unchanged
		{6, 5},                      // ו — left of the "*", unchanged
		{5, 7},                      // ל — marked; one past its "*" at col 6
		{4, 8},                      // ש — right of the "*", shifted +1
		{9, 10},                     // x — LTR tail, shifted +1
		{len([]rune(bidiLine)), 13}, // EOL = total width (12 + 1 mark cell)
	} {
		if got := e.runeToVisualColumn(w, bidiLine, c.rune_, 4); got != c.want {
			t.Errorf("runeToVisualColumn(%d) = %d, want %d", c.rune_, got, c.want)
		}
	}
	if got := e.caretVisualColumn(w, bidiLine, 5, 4); got != 7 {
		t.Errorf("caretVisualColumn(5) = %d, want 7", got)
	}
	// Inverse: the "*" cell (col 6) and ל's cell (col 7) both select rune 5.
	for _, c := range []struct{ col, wantRune int }{
		{6, 5}, // the "*"
		{7, 5}, // ל
		{5, 6}, // ו
		{8, 4}, // ש
		{4, 7}, // ם
	} {
		if got := e.visualColumnToRune(w, bidiLine, c.col, 4); got != c.wantRune {
			t.Errorf("visualColumnToRune(%d) = %d, want %d", c.col, got, c.wantRune)
		}
	}
	// Forward/inverse are mutual inverses at every rune's own cell.
	for r := 0; r < len([]rune(bidiLine)); r++ {
		col := e.runeToVisualColumn(w, bidiLine, r, 4)
		if back := e.visualColumnToRune(w, bidiLine, col, 4); back != r {
			t.Errorf("round trip: rune %d -> col %d -> rune %d", r, col, back)
		}
	}
}

// The rendered output ties the bidi mark math to what the renderer actually
// paints: the "*" lands just left of the reversed ל, and the hardware cursor —
// placed by the renderer's own caretVisualColumn — lands on the marked cell.
func TestShowMarksBidiRendered(t *testing.T) {
	e, w, out := newRenderedEditor(t, bidiLine+"\n")
	w.ViewState.ShowMarks = "yes"
	if err := w.Buffer.SetMark("m", 0, 5); err != nil {
		t.Fatalf("SetMark: %v", err)
	}
	e.performRender()
	if plain := stripAnsi(out.String()); !strings.Contains(plain, "abc םו*לש xyz") {
		t.Fatalf("rendered marked bidi line = %q, want it to contain %q", plain, "abc םו*לש xyz")
	}

	// ל is painted at visual col 7 -> screen col 8 (no gutter); the renderer's
	// cursor placement must agree with the editor's caret math.
	w.SetCursorPos(window.Position{Line: 0, Rune: 5})
	e.afterHorizontalMovement(w)
	out.Reset()
	e.performRender()
	if _, col := lastCursor(out.Bytes()); col != 8 {
		t.Fatalf("hardware cursor on the marked ל: col %d, want 8", col)
	}
}

// showMarks stays an exact mutual inverse under an RTL base, with showBidi
// markers, and with tabs intermixed into the bidi run — the cases the removed
// flat-offset fallback got wrong. No hardcoded columns: forward-then-inverse
// must return each rune.
func TestShowMarksBidiRoundTrips(t *testing.T) {
	cases := []struct {
		name    string
		content string
		marks   []int
		cfg     []string
		bidi    bool
	}{
		{"ltr base hebrew", bidiLine, []int{2, 5, 9}, nil, false},
		{"rtl base hebrew", bidiLine, []int{0, 5, 11}, []string{"direction=rtl"}, false},
		{"showBidi markers", bidiLine, []int{4, 7}, nil, true},
		{"tab before hebrew", "a\tאב c", []int{0, 1, 3}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, w := newTestEditor(t, tc.content+"\n", tc.cfg...)
			w.ViewState.ShowMarks = "yes"
			w.ViewState.ShowBidi = tc.bidi
			for _, p := range tc.marks {
				if err := w.Buffer.SetMark(fmt.Sprintf("m%d", p), 0, p); err != nil {
					t.Fatalf("SetMark(%d): %v", p, err)
				}
			}
			w.SetCursorPos(window.Position{Line: 0, Rune: 0})
			rn := []rune(tc.content)
			for r := 0; r < len(rn); r++ {
				col := e.runeToVisualColumn(w, tc.content, r, 4)
				if back := e.visualColumnToRune(w, tc.content, col, 4); back != r {
					t.Errorf("rune %d -> col %d -> rune %d", r, col, back)
				}
			}
		})
	}
}

// showMarks "all" also indicates mew's internal (underscore-prefixed) marks,
// which "yes" hides. MarksOnLine's includeInternal flag drives it, and the caret
// math reserves a cell for each indicated mark.
func TestShowMarksAllIncludesInternal(t *testing.T) {
	e, w := newTestEditor(t, "abcd\n")
	if err := w.Buffer.SetMark("user", 0, 1); err != nil {
		t.Fatalf("SetMark user: %v", err)
	}
	if err := w.Buffer.SetMark("_internal", 0, 3); err != nil {
		t.Fatalf("SetMark internal: %v", err)
	}

	// The buffer filter: "yes" mode sees only the user mark, "all" sees both.
	if cols := w.Buffer.MarksOnLine(0, false); len(cols) != 1 || cols[0] != 1 {
		t.Fatalf("user-visible marks = %v, want [1]", cols)
	}
	if cols := w.Buffer.MarksOnLine(0, true); len(cols) != 2 || cols[0] != 1 || cols[1] != 3 {
		t.Fatalf("all marks = %v, want [1 3]", cols)
	}

	// Through the view mode: "all" reserves two "*" cells before end of line,
	// "yes" only one.
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	w.ViewState.ShowMarks = "all"
	if got := e.caretVisualColumn(w, "abcd", 4, 4); got != 6 {
		t.Fatalf("all-mode EOL caret: %d, want 6 (both marks reserved)", got)
	}
	w.ViewState.ShowMarks = "yes"
	if got := e.caretVisualColumn(w, "abcd", 4, 4); got != 5 {
		t.Fatalf("yes-mode EOL caret: %d, want 5 (only the user mark)", got)
	}
	w.ViewState.ShowMarks = "no"
	if got := e.caretVisualColumn(w, "abcd", 4, 4); got != 4 {
		t.Fatalf("no-mode EOL caret: %d, want 4 (no marks)", got)
	}
}
