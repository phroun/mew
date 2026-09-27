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

// A theme's black is rarely the pure one, so what counts is not the value but
// whether there is enough light in it to tell from the ground: below 14% of
// full, weighted for the eye, a colour IS the ground and a shade over it says
// nothing a reader can see.
//
// Stated either side of the line rather than at one point, since a threshold
// nothing is measured against is a number rather than a rule.
func TestAGroundTooDarkToSeeIsTheGround(t *testing.T) {
	for _, c := range []struct {
		name, ground string
		black        bool
	}{
		{"pure black said in channels", "48;2;0;0;0", true},
		{"a near-black a theme would actually write", "48;2;30;30;46", true},
		{"the greyscale ramp's foot", "48;5;232", true},
		{"and its fourth step, still under the line", "48;5;234", true},
		{"its fifth step is over it", "48;5;235", false},
		{"grey 34 is under", "48;2;34;34;34", true},
		{"grey 36 is over", "48;2;36;36;36", false},
		{"a mid colour is nowhere near", "48;2;10;120;200", false},
		{"nor is a cube colour", "48;5;27", false},
		{"the sixteen the terminal draws itself have no answer", "48;5;8", false},
		{"except the one named black", "48;5;0", true},
	} {
		if got := isBlackGround(c.ground); got != c.black {
			t.Errorf("%s: isBlackGround(%q) = %v, want %v",
				c.name, c.ground, got, c.black)
		}
		// And the whole way through: a ground that is the ground draws no shade.
		out := emittedRow(t, "\x1b[0;37;"+c.ground+"m")
		if n := strings.Count(out, fallbackBlank); (n == 0) != c.black {
			t.Errorf("%s (%q): %d shades in the content, black=%v",
				c.name, c.ground, n, c.black)
		}
	}
}
