package render

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/viewport"
)

const hebrewABG = "אבג" // aleph bet gimel

func invisiblesRenderer(t *testing.T) (*ScreenRenderer, *viewport.Viewport) {
	t.Helper()
	sr, w := testRenderer()
	sr.indicators = config.DefaultConfig().Indicators
	w.ViewState.ShowInvisibles = true
	return sr, w
}

// A line's terminator marker belongs at the line's END — the visual RIGHT under
// ltr, the visual LEFT under rtl. Appending it to the visual slot order pinned
// it to the right in both directions, so under rtl it landed on the reading
// START, reading as though the terminator had switched to LTR by itself.
func TestInvisibleTerminatorPaintsAtLineEnd(t *testing.T) {
	sr, w := invisiblesRenderer(t)
	nl := sr.indicators.VisibleNewline

	cases := []struct {
		dir  string
		line string
	}{
		{"ltr", hebrewABG},
		{"ltr", "abc"},
		{"rtl", hebrewABG},
		{"rtl", "abc"},
		{"rtl", ""},
	}
	for _, c := range cases {
		w.ViewState.Direction = c.dir
		out := stripAnsi(sr.prepareLineForDisplay(c.line, "\n", 10, 0, w, 0, selectionRange{}, nil, nil))
		idx := strings.Index(out, nl)
		if idx < 0 {
			t.Fatalf("dir=%s line=%q: no terminator marker in %q", c.dir, c.line, out)
		}
		trailing := strings.TrimSpace(out[idx+len(nl):])
		leading := strings.TrimSpace(out[:idx])
		if c.dir == "rtl" {
			// Under rtl the marker leads the content: nothing but padding left
			// of it, and the line's text to its right.
			if leading != "" {
				t.Errorf("dir=rtl line=%q: marker should sit at the visual LEFT of the text, got %q",
					c.line, out)
			}
		} else if trailing != "" {
			t.Errorf("dir=ltr line=%q: marker should sit at the visual RIGHT of the text, got %q",
				c.line, out)
		}
	}
}

// The terminator markers ride in the right-alignment PADDING under rtl, never
// in content columns: turning invisibles on must not shift the text by a cell.
// The caret column (ensureCursorVisibleHorizontal, rtlPadOffset) omits them for
// exactly this reason, so any displacement here desynchronises the caret.
func TestInvisiblesDoNotShiftRTLContent(t *testing.T) {
	sr, w := invisiblesRenderer(t)
	w.ViewState.Direction = "rtl"

	for _, line := range []string{hebrewABG, "abc", hebrewABG + " abc", ""} {
		w.ViewState.ShowInvisibles = false
		plain := stripAnsi(sr.prepareLineForDisplay(line, "\n", 20, 0, w, 0, selectionRange{}, nil, nil))
		w.ViewState.ShowInvisibles = true
		marked := stripAnsi(sr.prepareLineForDisplay(line, "\n", 20, 0, w, 0, selectionRange{}, nil, nil))

		// Compare the column the content STARTS on (the markers replace spaces
		// and tabs inside it, so the text itself differs).
		plainCol := len([]rune(plain)) - len([]rune(strings.TrimLeft(plain, " ")))
		markedCol := len([]rune(marked))
		if i := strings.Index(marked, sr.indicators.VisibleNewline); i >= 0 {
			markedCol = len([]rune(marked[:i])) + 1
		}
		if markedCol != plainCol {
			t.Errorf("line %q: invisibles shifted the content to column %d, want %d\n plain  %q\n marked %q",
				line, markedCol, plainCol, plain, marked)
		}
	}
}

// A full-width line has no padding for the markers to ride in: its visual end
// is off the left edge, and so are they. They must be dropped rather than
// displace the text back into view.
func TestInvisiblesDroppedWhenRTLLineFillsWidth(t *testing.T) {
	sr, w := invisiblesRenderer(t)
	w.ViewState.Direction = "rtl"

	line := strings.Repeat("a", 10)
	out := stripAnsi(sr.prepareLineForDisplay(line, "\n", 10, 0, w, 0, selectionRange{}, nil, nil))
	if strings.Contains(out, sr.indicators.VisibleNewline) {
		t.Errorf("a full-width rtl line has no room for the terminator marker: %q", out)
	}
	if out != line {
		t.Errorf("content must fill the row unshifted; got %q", out)
	}
}
