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
