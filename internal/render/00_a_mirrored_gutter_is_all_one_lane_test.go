package render

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
)

// gutterCells returns the painted cells of row y's line-number gutter, which on
// a right-to-left viewport is the last lineNumWidth cells of the row.
func gutterCells(sr *ScreenRenderer, y, lineNumWidth int) []bbCell {
	row := sr.frame.cur[y]
	return row[len(row)-lineNumWidth:]
}

func gutterGlyphs(cells []bbCell) string {
	var b strings.Builder
	for _, c := range cells {
		if len(c.runes) == 0 {
			b.WriteByte(' ')
			continue
		}
		b.WriteString(string(c.runes))
	}
	return b.String()
}

// mirroredGutter renders two lines into a right-to-left viewport: one carrying a
// pointed right-to-left run, which this host cannot place a fill on, and one
// carrying none.
func mirroredGutter(t *testing.T, drifts bool) (*ScreenRenderer, int) {
	t.Helper()
	sr, w := testRenderer()
	sr.Width = 40
	sr.Height = 6
	sr.frame.flipBidi = true
	sr.frame.flipRideSafe = drifts

	w.Buffer = buffer.NewFromString("abc שָם\nabc plain\n")
	w.ViewState.ShowLineNumbers = true
	w.ViewState.Direction = "rtl"
	w.LineNumWidth = 4
	w.FrameWidth = sr.Width

	sr.frame.reshape(sr.Width, sr.Height)
	sr.frame.begin()
	sr.renderContent(w, 1, 2)
	return sr, w.LineNumWidth
}

// The gutter is one lane, and every cell in it belongs to the gutter -- the
// blank before the number and the blanks after it as much as the number itself.
// Written as a colour and then a field, the leading blank went out before the
// colour did and reached the screen wearing whatever the content had left in
// effect, on every row whether it drifted or not.
func TestTheMirroredGutterColoursEveryCell(t *testing.T) {
	sr, width := mirroredGutter(t, false)

	for y := 0; y < 2; y++ {
		for i, c := range gutterCells(sr, y, width) {
			if !strings.Contains(c.style, "44") {
				t.Errorf("row %d cell %d of the gutter wears %q, which is not the "+
					"gutter's own colour", y, i, c.style)
			}
		}
	}
}

// And on a row the host cannot place a fill on, EVERY blank cell of it draws
// the shade -- not just the one before the number, which left the rest of the
// lane bare where the blue used to be.
func TestTheMirroredGuttersShadeFillsTheLane(t *testing.T) {
	sr, width := mirroredGutter(t, true)

	cells := gutterCells(sr, 0, width)
	glyphs := gutterGlyphs(cells)
	if !strings.Contains(glyphs, "1") {
		t.Fatalf("the drifting row's gutter lost its number: %q", glyphs)
	}
	for i, c := range cells {
		if strings.Contains(c.style, "44") {
			t.Errorf("cell %d kept a ground the drift would misplace: %q", i, c.style)
		}
		blank := len(c.runes) == 0 || string(c.runes) == " "
		if blank {
			t.Errorf("cell %d of a shaded gutter is bare, not %s", i, gutterBlank)
		}
	}
	if n := strings.Count(glyphs, gutterBlank); n != width-1 {
		t.Errorf("the shaded gutter drew %d of %s in a lane of %d holding a "+
			"one-digit number; want %d", n, gutterBlank, width, width-1)
	}

	// The row that carries no pointed run is untouched, so the shade says
	// something about the row rather than about the setting.
	for i, c := range gutterCells(sr, 1, width) {
		if strings.Contains(string(c.runes), gutterBlank) {
			t.Errorf("row 1 cell %d took a shade on a row that places its fill", i)
		}
		if !strings.Contains(c.style, "44") {
			t.Errorf("row 1 cell %d lost the gutter's colour: %q", i, c.style)
		}
	}
}
