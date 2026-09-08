package render

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
)

// emittedRow renders a right-to-left viewport and returns the bytes that reach
// the TERMINAL -- through present(), which is where a blank past the drift is
// turned into a shade. Reading the back buffer instead sees the cells before
// that happens and cannot show the thing this is about.
func emittedRow(t *testing.T, textStyle string) string {
	t.Helper()
	sr, w := testRenderer()
	sr.Width = 70
	sr.Height = 4
	sr.frame.flipBidi = true
	sr.frame.flipRideSafe = true
	sr.colorScheme.Global["text"] = textStyle

	w.Buffer = buffer.NewFromString(pointedLine + "\n")
	w.ViewState.ShowLineNumbers = true
	w.ViewState.Direction = "rtl"
	w.LineNumWidth = 4
	w.FrameWidth = sr.Width

	sr.frame.reshape(sr.Width, sr.Height)
	sr.frame.begin()
	sr.renderContent(w, 1, 1)
	var sb strings.Builder
	sr.frame.present(&sb)
	return sb.String()
}

// A theme's black is the ground the terminal already shows, whichever of its
// four spellings the theme happens to use. Reading only two of them left the
// rest answering as a colour, so every blank in the content area past the drift
// drew a shade where the file has a space.
func TestABlackGroundIsBlackInEverySpelling(t *testing.T) {
	for _, c := range []struct{ name, style string }{
		{"the basic black", "\x1b[0;37;40m"},
		{"the system palette's black", "\x1b[0;37;48;5;0m"},
		{"the cube's own black", "\x1b[0;37;48;5;16m"},
		{"a direct colour with no light in it", "\x1b[0;37;48;2;0;0;0m"},
		{"and the terminal's own background", "\x1b[0;37;49m"},
	} {
		out := emittedRow(t, c.style)
		if n := strings.Count(out, fallbackBlank); n != 0 {
			t.Errorf("%s (%q): %d spaces became %s", c.name, c.style, n, fallbackBlank)
		}
		if strings.Count(out, gutterBlank) != 3 {
			t.Errorf("%s: the gutter should still draw its own ground", c.name)
		}
	}
}

// And a ground that is a colour still draws one, so the test above says
// something about black rather than about the shade being switched off.
func TestAColouredGroundStillDrawsItsOwn(t *testing.T) {
	out := emittedRow(t, "\x1b[0;37;44m")
	if strings.Count(out, fallbackBlank) == 0 {
		t.Errorf("a blue ground past the drift drew nothing in its place: %q", out)
	}
}
