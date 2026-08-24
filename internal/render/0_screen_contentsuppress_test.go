package render

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
)

// renderRowsPlain drives renderContent into a fresh frame and returns the
// painted glyphs of each cell row, one string per row (read from the back
// buffer directly — renderContent positions cells by address, not newlines).
func renderRowsPlain(sr *ScreenRenderer, w *viewport.Viewport, height int) []string {
	sr.frame.reshape(sr.Width, sr.Height)
	sr.frame.begin()
	sr.renderContent(w, 1, height)
	rows := make([]string, 0, height)
	for y := 0; y < height && y < len(sr.frame.cur); y++ {
		var b strings.Builder
		for _, c := range sr.frame.cur[y] {
			if c.cont {
				continue
			}
			if len(c.runes) == 0 {
				b.WriteByte(' ')
				continue
			}
			b.WriteString(string(c.runes))
		}
		rows = append(rows, b.String())
	}
	return rows
}

// A viewport flagged by the content suppressor (a terminal session's viewport)
// paints NO document text — the host draws the terminal grid over the area, and
// a grid too short/narrow must reveal the editor background, not the buffer
// behind. The line-number gutter still renders. Toggling the suppressor off
// brings the document text back, proving the gutter/text split is exactly the
// suppressor's doing.
func TestContentSuppressorBlanksTextKeepsGutter(t *testing.T) {
	sr, w := testRenderer()
	sr.Width = 40
	sr.Height = 6

	w.Buffer = buffer.NewFromString("HELLOWORLD\nSECONDLINE\n")
	w.ViewState.ShowLineNumbers = true
	w.LineNumWidth = 3
	w.FrameWidth = sr.Width

	// Suppressor off: the document text paints.
	sr.SetContentSuppressor(func(*viewport.Viewport) bool { return false })
	rows := renderRowsPlain(sr, w, 4)
	if !strings.Contains(rows[0], "HELLOWORLD") {
		t.Fatalf("baseline: row 0 should show the document text, got %q", rows[0])
	}
	if !strings.Contains(rows[0], "1") {
		t.Fatalf("baseline: row 0 should show the line number, got %q", rows[0])
	}

	// Suppressor on: the same viewport keeps its gutter but blanks the text.
	sr.SetContentSuppressor(func(*viewport.Viewport) bool { return true })
	rows = renderRowsPlain(sr, w, 4)
	if strings.Contains(rows[0], "HELLOWORLD") {
		t.Errorf("suppressed viewport must not paint document text, got %q", rows[0])
	}
	if strings.Contains(rows[1], "SECONDLINE") {
		t.Errorf("suppressed viewport must not paint document text, got %q", rows[1])
	}
	if !strings.Contains(rows[0], "1") {
		t.Errorf("suppressed viewport must still show line numbers, got %q", rows[0])
	}
	if !strings.Contains(rows[1], "2") {
		t.Errorf("suppressed viewport must still show line numbers, got %q", rows[1])
	}
}
