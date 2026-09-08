package render

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// A forced colour (a browse-mode heading, a markup run) must step ASIDE for
// the selection bar: it is a colour choice about the text, not a claim on the
// cell, and hiding the selection behind it makes selected heading text look
// unselected.
func TestSelectionShowsThroughForcedColor(t *testing.T) {
	sr, w := testRenderer()
	selectionColor := sr.col(w, "selection")

	// A heading-style span: forced colour, no selected variant of its own.
	span := DisplaySpan{
		Start: 0, End: 5,
		Runes: []rune("HEAD!"),
		Doc:   []int{0, 1, 2, 3, 4},
		Style: []string{headC, headC, headC, headC, headC},
	}
	disp := substituteSpans([]rune("HEAD!"), []DisplaySpan{span}, false)
	if disp == nil {
		t.Fatal("substitution expected")
	}
	disp.Text = string(disp.Runes)

	sel := selectionRange{exists: true, startLine: 0, startRune: 1, endLine: 0, endRune: 4}
	out := sr.prepareLineForDisplay("HEAD!", "\n", 20, 0, w, 0, sel, disp, nil)

	if !strings.Contains(out, selectionColor) {
		t.Errorf("selected heading cells must show the selection colour, got %q", out)
	}
	// The unselected cells keep the heading colour.
	if !strings.Contains(out, headC) {
		t.Errorf("unselected heading cells should keep their forced colour: %q", out)
	}
}

// A selected BUTTON gets its own scheme instead of the plain selection bar —
// and only on the cells the selection covers, so a partly-selected button
// splits at the boundary. The shaping is untouched either way: same caps, same
// shadow glyph.
func TestSelectedButtonUsesItsOwnScheme(t *testing.T) {
	sr, w := testRenderer()
	selectionColor := sr.col(w, "selection")

	doc := []rune("ab[[x]]cd")
	span := ButtonSpan{
		Start: 2, End: 7, Runes: []rune("<X>"), Shadow: '#',
		Color: btnC, ShadowColor: shdC,
		SelColor: selBtnC, SelShadowColor: selShdC,
	}.Span()
	disp := substituteSpans(doc, []DisplaySpan{span}, false)
	if disp == nil {
		t.Fatal("substitution expected")
	}
	disp.Text = string(disp.Runes)

	// The whole button (doc 2..7) is inside the selection.
	sel := selectionRange{exists: true, startLine: 0, startRune: 0, endLine: 0, endRune: 9}
	out := sr.prepareLineForDisplay(string(doc), "\n", 30, 0, w, 0, sel, disp, nil)

	if !strings.Contains(out, selBtnC) {
		t.Errorf("a selected button should use its selected face colour: %q", out)
	}
	if !strings.Contains(out, selShdC) {
		t.Errorf("a selected button's shadow should use its selected shadow colour: %q", out)
	}
	// The shaping is unchanged: caps and shadow glyph still painted.
	plain := stripAnsi(out)
	if !strings.Contains(plain, "<X>") || !strings.Contains(plain, "#") {
		t.Errorf("selection must not change the button's shaping: %q", plain)
	}
	// Text outside the button still takes the ordinary selection bar.
	if !strings.Contains(out, selectionColor) {
		t.Errorf("selected non-button text should still show the selection colour: %q", out)
	}
}

// An UNSELECTED button is untouched by the new slots.
func TestUnselectedButtonKeepsItsColors(t *testing.T) {
	sr, w := testRenderer()
	doc := []rune("ab[[x]]cd")
	span := ButtonSpan{
		Start: 2, End: 7, Runes: []rune("<X>"), Shadow: '#',
		Color: btnC, ShadowColor: shdC,
		SelColor: selBtnC, SelShadowColor: selShdC,
	}.Span()
	disp := substituteSpans(doc, []DisplaySpan{span}, false)
	disp.Text = string(disp.Runes)

	out := sr.prepareLineForDisplay(string(doc), "\n", 30, 0, w, 0, selectionRange{}, disp, nil)
	if !strings.Contains(out, btnC) || !strings.Contains(out, shdC) {
		t.Errorf("an unselected button keeps its ordinary colours: %q", out)
	}
	if strings.Contains(out, selBtnC) || strings.Contains(out, selShdC) {
		t.Errorf("an unselected button must not use the selected scheme: %q", out)
	}
}

// Distinct SGR sequences standing in for the configured colours: real escapes,
// so stripAnsi removes them and the shaping assertions see only glyphs.
const (
	headC   = "\x1b[35m"
	btnC    = "\x1b[36m"
	shdC    = "\x1b[37m"
	selBtnC = "\x1b[38m"
	selShdC = "\x1b[39m"
)

var _ = viewport.Position{}

// A terminal that reorders what it is sent misplaces a background fill one RUN
// at a time, so a line of English with one pointed word in it gives up the bar
// on that word and keeps it everywhere else. Giving up the whole line loses the
// fill on every cell where it would have landed exactly right.
func TestOnlyThePointedRunGivesUpTheBar(t *testing.T) {
	const (
		bar     = "\x1b[0;30;47m" // the ordinary selection fill
		flipSel = "\x1b[0;1;93m"  // what rides a glyph instead
	)
	sr, w := testRenderer()
	sr.frame.flipBidi = true
	sr.frame.flipRideSafe = true // the ride-safe (Terminal.app) profile
	w.ViewState.SuppressRTLCombining = false
	whole := selectionRange{startLine: 0, endLine: 0, startRune: 0, endRune: 50, exists: true}
	render := func(line string) string {
		return sr.prepareLineForDisplay(line, "\n", 40, 0, w, 0, whole, nil, nil)
	}

	// English on both sides of a pointed word: both styles on one line.
	out := render("abc שָם xyz")
	if !strings.Contains(out, flipSel) {
		t.Errorf("the pointed run should give up its fill: %q", out)
	}
	if !strings.Contains(out, bar) {
		t.Errorf("the English around it should keep the bar: %q", out)
	}

	// The same line with nothing pointed keeps the bar throughout.
	out = render("abc שם xyz")
	if strings.Contains(out, flipSel) {
		t.Errorf("a run with no marks gave up its fill: %q", out)
	}

	// And a line that is nothing but the pointed run has only the one style,
	// which is what the whole-line answer used to give every line.
	out = render("שָם")
	if strings.Contains(out, bar) {
		t.Errorf("a line that is all one pointed run kept a bar it cannot place: %q", out)
	}
}

// The riding selection paints no ground, so a selected SPACE under it would be
// indistinguishable from an unselected one. It stands a shaded cell there
// instead, in the selection's own ink -- which is what survives this host's
// reordering.
func TestTheRidingSelectionShowsItsWhitespace(t *testing.T) {
	const (
		bar     = "\x1b[0;30;47m"
		flipSel = "\x1b[0;1;93m"
	)
	sr, w := testRenderer()
	sr.frame.flipBidi = true
	sr.frame.flipRideSafe = true
	w.ViewState.SuppressRTLCombining = false
	whole := selectionRange{startLine: 0, endLine: 0, startRune: 0, endRune: 50, exists: true}
	render := func(line string) string {
		return sr.prepareLineForDisplay(line, "\n", 40, 0, w, 0, whole, nil, nil)
	}

	// A space between two pointed words is inside the run that gave up its
	// fill, so it wears the shade.
	out := render("שָם שָם")
	if !strings.Contains(out, flipSel+selectedBlank) {
		t.Errorf("a selected space in a run wearing the riding style kept a blank "+
			"that reads as unselected: %q", out)
	}

	// A line with nothing to give up keeps ordinary spaces under the bar.
	out = render("a b c")
	if strings.Contains(out, selectedBlank) {
		t.Errorf("a line that can carry its fill stood a shade where a space "+
			"belongs: %q", out)
	}
	if !strings.Contains(out, bar) {
		t.Errorf("a line that can carry its fill lost its bar: %q", out)
	}
}

// And the padding past the end of the content follows the line, not the run.
// It sits past the point the drift starts from, so its bar comes back over the
// letters -- a selected newline says itself with the shade instead.
func TestTheSelectedEndOfLineGivesUpItsFillToo(t *testing.T) {
	const (
		bar     = "\x1b[0;30;47m"
		flipSel = "\x1b[0;1;93m"
	)
	sr, w := testRenderer()
	sr.frame.flipBidi = true
	sr.frame.flipRideSafe = true
	w.ViewState.SuppressRTLCombining = false

	// A selection running past this line's newline, so the padding is selected.
	through := selectionRange{startLine: 0, endLine: 1, startRune: 0, endRune: 5, exists: true}
	render := func(line string) string {
		return sr.prepareLineForDisplay(line, "\n", 40, 0, w, 0, through, nil, nil)
	}

	out := render("abc שָם")
	if !strings.Contains(out, flipSel+strings.Repeat(selectedBlank, 4)) {
		t.Errorf("the padding on a line carrying a pointed run kept a fill that "+
			"lands back over the letters: %q", out)
	}

	// A line with nothing to give up pads with the ordinary bar.
	out = render("abc")
	if strings.Contains(out, selectedBlank) {
		t.Errorf("a line that can carry its fill shaded its padding: %q", out)
	}
	if !strings.Contains(out, bar+strings.Repeat(" ", 4)) {
		t.Errorf("a line that can carry its fill lost its padding bar: %q", out)
	}
}

// A full stop after a pointed word is in no right-to-left run of its own --
// nothing strong follows it to pull it in -- so a per-run answer left it
// holding the ordinary bar. One lone cell of fill on a line whose fill cannot
// be placed, and it came back behind a letter that was never selected.
//
// The damage runs to the end of the line, so it gives up with everything else
// after the run.
func TestATrailingStopAfterAPointedRunGivesUpToo(t *testing.T) {
	const (
		bar     = "\x1b[0;30;47m"
		flipSel = "\x1b[0;1;93m"
	)
	sr, w := testRenderer()
	sr.frame.flipBidi = true
	sr.frame.flipRideSafe = true
	w.ViewState.SuppressRTLCombining = false
	whole := selectionRange{startLine: 0, endLine: 0, startRune: 0, endRune: 50, exists: true}

	out := sr.prepareLineForDisplay("abc שָם.", "\n", 40, 0, w, 0, whole, nil, nil)
	if strings.Contains(out, bar+".") {
		t.Errorf("the stop after a pointed run kept a fill that lands elsewhere: %q", out)
	}
	if !strings.Contains(out, flipSel+".") {
		t.Errorf("the stop after a pointed run did not take the riding style: %q", out)
	}
	// The English BEFORE the run is placed correctly and keeps its bar.
	if !strings.Contains(out, bar+"a") {
		t.Errorf("the English before the run lost the fill it can carry: %q", out)
	}
}
