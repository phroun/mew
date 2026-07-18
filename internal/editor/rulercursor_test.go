package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

// rulerCursorColor is the default rulerCursor color (black on silver).
const rulerCursorColor = "\x1b[0;30;47m"

// rowCellColors extracts the physical terminal row `row` (1-based) from a
// rendered stream and returns, for each printed column, the SGR color escape
// active when that column's glyph was written. Columns are 1-based in the
// returned map.
func rowCellColors(raw string, row int) map[int]string {
	// Find "\x1b[<row>;1H" and read until the next cursor-move to a new row.
	colors := map[int]string{}
	target := "\x1b[" + itoa(row) + ";1H"
	i := strings.Index(raw, target)
	if i < 0 {
		return colors
	}
	seg := raw[i+len(target):]
	// Stop at the next absolute cursor move to a different row.
	if j := strings.Index(seg, "H"); j >= 0 {
		// find the first "\x1b[<n>;<n>H" after some content
		for k := 0; k+1 < len(seg); k++ {
			if seg[k] == '\x1b' && seg[k+1] == '[' {
				// is it an H-terminated move?
				e := k + 2
				for e < len(seg) && seg[e] != 'H' && seg[e] != 'm' {
					e++
				}
				if e < len(seg) && seg[e] == 'H' {
					seg = seg[:k]
					break
				}
			}
		}
	}
	cur := ""
	col := 1
	bs := []byte(seg)
	for p := 0; p < len(bs); {
		if bs[p] == '\x1b' && p+1 < len(bs) && bs[p+1] == '[' {
			e := p + 2
			for e < len(bs) && !(bs[e] >= '@' && bs[e] <= '~') {
				e++
			}
			if e < len(bs) {
				if bs[e] == 'm' {
					cur = string(bs[p : e+1])
				}
				p = e + 1
				continue
			}
		}
		// a printed byte (may be part of a multi-byte rune; only column
		// bookkeeping matters, and ruler glyphs used here are single-byte)
		colors[col] = cur
		col++
		p++
	}
	return colors
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// With rulerShowsCursor on, the ruler cell under the caret is painted with the
// rulerCursor color; other cells are not.
func TestRulerShowsCursorHighlightsCaret(t *testing.T) {
	e, w, out := newRenderedEditor(t, "hello world\n")
	w.ViewState.ShowRuler = true
	e.PawScript.ExecuteAsync("set_option 'rulerShowsCursor', 'true'")

	w.SetCursorPos(window.Position{Line: 0, Rune: 3})
	e.afterHorizontalMovement(w)
	out.Reset()
	e.performRender()

	// Caret at rune 3 on an LTR line with no gutter -> screen column 4.
	colors := rowCellColors(out.String(), 1) // ruler is the window's top row
	if colors[4] != rulerCursorColor {
		t.Fatalf("ruler cell under caret (col 4) color = %q, want rulerCursor %q", colors[4], rulerCursorColor)
	}
	if colors[3] == rulerCursorColor || colors[5] == rulerCursorColor {
		t.Fatalf("only the caret column should be highlighted (cols 3/5 got rulerCursor)")
	}
}

// With rulerShowsCursor off (default), no ruler cell uses the rulerCursor color.
func TestRulerShowsCursorOffByDefault(t *testing.T) {
	e, w, out := newRenderedEditor(t, "hello world\n")
	w.ViewState.ShowRuler = true
	w.SetCursorPos(window.Position{Line: 0, Rune: 3})
	e.afterHorizontalMovement(w)
	out.Reset()
	e.performRender()

	colors := rowCellColors(out.String(), 1)
	for c, col := range colors {
		if col == rulerCursorColor {
			t.Fatalf("rulerShowsCursor is off, but col %d used the rulerCursor color", c)
		}
	}
}

// The ghost cursor's column is highlighted on the ruler too.
func TestRulerShowsCursorHighlightsGhost(t *testing.T) {
	e, w, out := newRenderedEditor(t, "hello world\nab\n")
	w.ViewState.ShowRuler = true
	e.PawScript.ExecuteAsync("set_option 'rulerShowsCursor', 'true'")

	w.SetCursorPos(window.Position{Line: 0, Rune: 8}) // col 8
	e.afterHorizontalMovement(w)
	e.executeCommand("go_line_next") // onto "ab": ghost at col 8, caret at end
	if !w.HasGhostCursor {
		t.Fatal("expected a ghost cursor on the shorter line")
	}
	out.Reset()
	e.performRender()

	// The ghost is on line 1 (buffer row 2). Its ruler is the same shared
	// ruler? No — the ruler is per-window top line (row 1). The ghost column
	// is still the caret line's column; highlight reflects the caret's line.
	colors := rowCellColors(out.String(), 1)
	// caret clamped to end of "ab" -> screen col 3; ghost at col 9 (rune 8 +1).
	if colors[9] != rulerCursorColor {
		t.Fatalf("ruler cell under ghost (col 9) = %q, want rulerCursor", colors[9])
	}
}

// At an automatic direction boundary the secondary bidi cursor has its own
// column, so the ruler highlights TWO cells (the caret and its other end).
func TestRulerShowsCursorHighlightsSecondary(t *testing.T) {
	e, w, out := newRenderedEditor(t, bidiLine+"\n") // "abc שלום xyz"
	w.ViewState.ShowRuler = true
	e.PawScript.ExecuteAsync("set_option 'rulerShowsCursor', 'true'")

	w.SetCursorPos(window.Position{Line: 0, Rune: 4}) // entering the Hebrew run
	e.afterHorizontalMovement(w)
	out.Reset()
	e.performRender()

	colors := rowCellColors(out.String(), 1)
	highlighted := 0
	for _, col := range colors {
		if col == rulerCursorColor {
			highlighted++
		}
	}
	if highlighted < 2 {
		t.Fatalf("expected the caret AND secondary columns highlighted, got %d", highlighted)
	}
}
