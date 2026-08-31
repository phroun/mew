package editor

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/textwidth"
)

var cupRe = regexp.MustCompile(`\x1b\[(\d+);(\d+)H`)
var sgrRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// rowCells returns, for each screen row the frame addressed, how many terminal
// columns the text written after that CUP actually advances. A content row
// must cover the full screen width; anything less leaves the terminal's own
// background showing through at the end of the line.
func rowCells(frame string) map[int]int {
	out := map[int]int{}
	idx := cupRe.FindAllStringSubmatchIndex(frame, -1)
	for i, m := range idx {
		row := 0
		for _, c := range frame[m[2]:m[3]] {
			row = row*10 + int(c-'0')
		}
		col := 0
		for _, c := range frame[m[4]:m[5]] {
			col = col*10 + int(c-'0')
		}
		end := len(frame)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		seg := sgrRe.ReplaceAllString(frame[m[1]:end], "")
		seg = strings.ReplaceAll(seg, "\x1b[K", "")
		n := col - 1
		for _, r := range seg {
			if r == '\x1b' || r < 0x20 {
				continue
			}
			n += textwidth.Rune(r)
		}
		if n > out[row] {
			out[row] = n
		}
	}
	return out
}

// A line whose only oddity is a baseless combining mark must still paint the
// full width of the window. The anchored form occupies one cell; if the
// renderer's column budget and the cells it actually emits disagree, the row
// comes up short and the terminal's background shows through the tail.
func TestAnchoredMarkRowCoversFullWidth(t *testing.T) {
	data, err := os.ReadFile("../render/testdata/lorem-torture.txt")
	if err != nil {
		t.Fatalf("torture sample: %v", err)
	}
	e, w, out := newRenderedEditor(t, string(data))
	e.performRender()

	rows := rowCells(out.String())
	// The content rows are the ones holding our text; check every row the
	// viewport owns.
	short := 0
	for row := w.ContentY + 1; row <= w.ContentY+w.ContentHeight; row++ {
		n, ok := rows[row]
		if !ok {
			continue
		}
		want := e.Renderer.Width
		if row == e.Renderer.Height {
			want-- // the bottom-right corner cell is never written
		}
		if n != want {
			t.Errorf("row %d painted %d columns, want %d", row, n, want)
			short++
		}
	}
	if short > 0 {
		t.Logf("%d rows fell short — the terminal's background shows through their tails", short)
	}
}
