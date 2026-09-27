package editor

import (
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// A click on a bidi direction marker ("<" / ">" / "|", shown under showBidi)
// lands the caret on the correct run boundary. Uses deterministic mixed-run
// lines whose visual layout is one cell per column (no marks, no showMarks), so
// the visual column equals the Perm index.
func TestMarkerCaretAt(t *testing.T) {
	const (
		gimel = "ג"
		dalet = "ד"
	)

	// Base LTR, Latin then Hebrew: "ab" + "גד".
	// Perm: a b | ד ג <   (cols 0..5); logical a=0 b=1 ג=2 ד=3.
	t.Run("latin-then-hebrew", func(t *testing.T) {
		e, w := markerEditor(t, "ab"+gimel+dalet)
		cases := []struct {
			col, subX, want int
			name            string
		}{
			{2, 0, 2, "pipe left half → right edge of b (after b)"},
			{2, 700, 4, "pipe right half → left edge of ד (reading-end of RTL word)"},
			{5, 0, 2, "'<' → before ג (the RTL cell to its left)"},
		}
		for _, c := range cases {
			e.mouseSubX = c.subX
			got, ok := e.markerCaretAt(w, "ab"+gimel+dalet, c.col, c.subX)
			if !ok {
				t.Fatalf("%s: col %d not recognised as a marker", c.name, c.col)
			}
			if got != c.want {
				t.Errorf("%s: got caret %d, want %d", c.name, got, c.want)
			}
		}
	})

	// Base LTR, Hebrew then Latin: "גד" + "ab".
	// Perm: | ד ג < > a b |  (cols 0..7); logical ג=0 ד=1 a=2 b=3.
	t.Run("hebrew-then-latin", func(t *testing.T) {
		line := gimel + dalet + "ab"
		e, w := markerEditor(t, line)
		cases := []struct {
			col, subX, want int
			name            string
		}{
			{3, 0, 0, "'<' → before ג (index 0)"},
			{4, 0, 2, "'>' → before a (the LTR cell to its right)"},
			{7, 0, 4, "trailing pipe left half → right edge of b (after b)"},
			{0, 700, 2, "leading pipe right half → left edge of ד (reading-end)"},
		}
		for _, c := range cases {
			e.mouseSubX = c.subX
			got, ok := e.markerCaretAt(w, line, c.col, c.subX)
			if !ok {
				t.Fatalf("%s: col %d not recognised as a marker", c.name, c.col)
			}
			if got != c.want {
				t.Errorf("%s: got caret %d, want %d", c.name, got, c.want)
			}
		}
	})

	// A real cell is not a marker.
	t.Run("real-cell", func(t *testing.T) {
		e, w := markerEditor(t, "ab"+gimel+dalet)
		if _, ok := e.markerCaretAt(w, "ab"+gimel+dalet, 0, 0); ok {
			t.Error("column 0 (the letter 'a') should not resolve as a marker")
		}
	})
}

func markerEditor(t *testing.T, content string) (*Editor, *viewport.Viewport) {
	t.Helper()
	e, w, _ := newRenderedEditor(t, content+"\n")
	w.ViewState.ShowBidi = true
	return e, w
}
