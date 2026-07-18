package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

// The caret must never be placed past a line's end. Garland's SeekLine neither
// clamps nor rejects an out-of-range rune-within-line — it leaves the cursor
// with a byte position on a later line while still reporting the requested
// (line, rune) — so the window setters clamp the rune to the line's length.
func TestSetCursorClampsRunePastLineEnd(t *testing.T) {
	_, w := newTestEditor(t, "hello\nworld\n\n")

	// Rune 20 on "hello" (5 runes) must clamp to 5, not report a bogus (0,20).
	w.SetCursorPos(window.Position{Line: 0, Rune: 20})
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 5 {
		t.Fatalf("SetCursorPos past line end: got (%d,%d), want (0,5)", got.Line, got.Rune)
	}

	// SetCursorLine carries the column; moving onto a shorter line clamps it.
	w.SetCursorPos(window.Position{Line: 0, Rune: 5}) // end of "hello"
	w.SetCursorLine(2)                                // empty line
	if got := w.CursorPos(); got.Line != 2 || got.Rune != 0 {
		t.Fatalf("SetCursorLine onto shorter line: got (%d,%d), want (2,0)", got.Line, got.Rune)
	}

	// SetCursorRune past the current line's end clamps too.
	w.SetCursorPos(window.Position{Line: 1, Rune: 0}) // "world"
	w.SetCursorRune(99)
	if got := w.CursorPos(); got.Line != 1 || got.Rune != 5 {
		t.Fatalf("SetCursorRune past line end: got (%d,%d), want (1,5)", got.Line, got.Rune)
	}
}

// After the clamp, the caret's reported line agrees with its actual content:
// reading the line at the reported cursor position returns that same line,
// rather than one further down (which the raw garland seek would have caused).
func TestSetCursorStaysConsistentPastEnd(t *testing.T) {
	e, w := newTestEditor(t, "aaaaa\nb\nccccc\n")

	// Seek to line 1 ("b", 1 rune) with a column from the longer line above.
	w.SetCursorPos(window.Position{Line: 1, Rune: 5})
	cp := w.CursorPos()
	if cp.Line != 1 {
		t.Fatalf("reported line drifted: got %d, want 1", cp.Line)
	}
	// The content at the reported line is "b", and the rune is clamped within it.
	got := strings.TrimRight(w.Buffer.GetLine(cp.Line), "\n\r")
	if got != "b" {
		t.Fatalf("line at cursor is %q, want %q", got, "b")
	}
	if cp.Rune > len([]rune(got)) {
		t.Fatalf("rune %d exceeds line length %d", cp.Rune, len([]rune(got)))
	}
	_ = e
}
