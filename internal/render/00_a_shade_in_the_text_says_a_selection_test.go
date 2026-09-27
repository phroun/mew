package render

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
)

// A pointed right-to-left run beside Latin and wide CJK: the shape of line the
// whole give-up-the-fill question is about.
const pointedLine = "#  >^.^<  龖釁爨鬱饕餮齉魑魅魍魎       אֲנִי רוֹצֶה לִשְׁתּוֹת מַיִם."

// pointedRow renders that line into a right-to-left viewport and returns the
// glyphs of its row.
func pointedRow(t *testing.T, rideSafe, flip bool) string {
	t.Helper()
	sr, w := testRenderer()
	sr.Width = 70
	sr.Height = 6
	sr.frame.flipBidi = flip
	sr.frame.flipRideSafe = rideSafe

	w.Buffer = buffer.NewFromString("plain line\n" + pointedLine + "\n")
	w.ViewState.ShowLineNumbers = true
	w.ViewState.Direction = "rtl"
	w.LineNumWidth = 4
	w.FrameWidth = sr.Width

	return renderRowsPlain(sr, w, 2)[1]
}

// The shade that stands for a selection means a selection and nothing else. A
// space in the text is a space, however little of what a line wears can be
// placed where it was written -- giving up a fill is a reason to stop asking
// for one, not a reason to draw something that was never there.
//
// Every combination, because a shade appearing without a selection would be a
// setting's doing rather than the text's.
func TestNoSelectionMeansNoSelectionShade(t *testing.T) {
	for _, rideSafe := range []bool{false, true} {
		for _, flip := range []bool{false, true} {
			row := pointedRow(t, rideSafe, flip)
			if n := strings.Count(row, selectedBlank); n != 0 {
				t.Errorf("rideSafe=%v flip=%v: %d selection shades on an unselected "+
					"line: %q", rideSafe, flip, n, row)
			}
			if n := strings.Count(row, fallbackBlank); n != 0 {
				t.Errorf("rideSafe=%v flip=%v: %d fallback shades where every ground "+
					"is the black one: %q", rideSafe, flip, n, row)
			}
		}
	}
}

// And the shade does appear once there IS a selection, so the test above says
// something about the selection rather than about the line.
func TestASelectedSpaceOnSuchALineTakesTheShade(t *testing.T) {
	sr, w := testRenderer()
	sr.Width = 70
	sr.Height = 6
	sr.frame.flipRideSafe = true

	w.Buffer = buffer.NewFromString("plain line\n" + pointedLine + "\n")
	w.ViewState.ShowLineNumbers = true
	w.ViewState.Direction = "rtl"
	w.LineNumWidth = 4
	w.FrameWidth = sr.Width
	if err := w.Buffer.SetMark("_block_begin", 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := w.Buffer.SetMark("_block_end", 1, len([]rune(pointedLine))); err != nil {
		t.Fatal(err)
	}

	row := renderRowsPlain(sr, w, 2)[1]
	if !strings.Contains(row, selectedBlank) {
		t.Errorf("a selected space in a run that gives up its fill drew no shade, "+
			"so the check for its absence proves nothing: %q", row)
	}
}
