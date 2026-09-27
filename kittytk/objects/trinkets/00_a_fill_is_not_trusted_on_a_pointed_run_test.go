package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// A vowel has no presentation form to fold into, so it survives folding; a
// point has one and does not.
const (
	fillVowel = "ִ"
	fillPoint = "ׁ"
)

// A terminal that reorders what it is sent misplaces a background fill one RUN
// at a time, so a field gives up the fill on the run that carries surviving
// marks and keeps its bar on everything else. A field of chrome and English
// with one pointed word in it wears BOTH.
//
// Foreground colour and weight ride each glyph through that reordering intact,
// which is what the run that gave up its fill wears instead.
func TestOnlyThePointedRunGivesUpItsFill(t *testing.T) {
	t.Cleanup(func() {
		core.SetTextMeasurer(nil)
		core.ForgetHostBidi()
		core.SetRtlMarkMode("")
	})
	core.SetTextMeasurer(nil)

	// What the field stamped the selection in: whether the ordinary bar
	// appeared anywhere, and whether the riding style did.
	selectionStyles := func(text string) (bar, riding bool) {
		px, err := raster.New(600, 200)
		if err != nil {
			t.Fatal(err)
		}
		ink := &noteInk{RenderBackend: px}
		form := NewPanel()
		ti := NewTextInput()
		form.AddChild(ti)
		ti.SetShowBidiControls(false)
		ti.SetText(text)
		ti.SetBounds(core.UnitRect{Width: 40 * 8, Height: 16})
		ti.SetFocus()
		ti.SelectAll()
		ti.Paint(core.NewPainter(ink))

		plain := ti.GetScheme().GetEditBoxSelection(true, style.PaneDefault)
		rides := ti.GetScheme().GetEditBoxSelectionRiding(true, style.PaneDefault)
		for _, st := range ink.styles {
			switch {
			case st.Fg == rides.Fg && st.Bg == rides.Bg && st.Attrs&style.StyleBold != 0:
				riding = true
			case st.Fg == plain.Fg && st.Bg == plain.Bg:
				bar = true
			}
		}
		return bar, riding
	}

	const (
		pointed = "ל" + fillVowel  // a vowel survives the fold
		folds   = "ש" + fillPoint  // a shin dot folds into its base
		mixed   = "abc " + pointed // English beside a pointed word
	)

	// On a stream-order host nothing is in question: the bar throughout.
	core.SetHostAppliesBidi(false, false, false)
	for _, text := range []string{pointed, folds, "hello", mixed} {
		if _, riding := selectionStyles(text); riding {
			t.Errorf("on a stream-order host %q gave up a selection bar", text)
		}
	}

	// On a host that misplaces a fill, nothing folds and the vowel survives.
	core.SetHostAppliesBidi(true, false, true)
	core.SetRtlMarkMode("")

	if bar, riding := selectionStyles(pointed); !riding || bar {
		t.Errorf("a pointed run: bar=%v riding=%v, want the riding style alone",
			bar, riding)
	}
	if bar, riding := selectionStyles("hello"); riding || !bar {
		t.Errorf("a line with nothing zero-width: bar=%v riding=%v, want the bar alone",
			bar, riding)
	}

	// The one the whole change is for: the English keeps its bar and only the
	// pointed word gives one up.
	if bar, riding := selectionStyles(mixed); !bar || !riding {
		t.Errorf("English beside a pointed word: bar=%v riding=%v, want both -- "+
			"the run gives up its fill and the rest of the field keeps it",
			bar, riding)
	}

	// With folding on, a point that HAS a presentation form no longer counts.
	core.SetRtlMarkMode("compose")
	if _, riding := selectionStyles(folds); riding {
		t.Error("a run whose points fold into their base gave up its fill")
	}
	if _, riding := selectionStyles(pointed); !riding {
		t.Error("a vowel that survives the fold kept a fill the host cannot place")
	}
}

// The riding selection uses only what rides a glyph: colour and weight. A
// background, a reverse or an underline drifts the same way the fill does, so
// none of them is any use here.
func TestTheRidingSelectionRidesTheGlyph(t *testing.T) {
	scheme := NewTextInput().GetScheme()
	for _, pane := range []style.PaneType{style.PaneDefault, style.PaneDark} {
		for _, focused := range []bool{true, false} {
			riding := scheme.GetEditBoxSelectionRiding(focused, pane)
			if riding.Bg != style.ColorDefault {
				t.Errorf("focused=%v pane=%v: the riding selection carries a background (%v)",
					focused, pane, riding.Bg)
			}
			if riding.Attrs&style.StyleUnderline != 0 || riding.Attrs&style.StyleReverse != 0 {
				t.Errorf("focused=%v pane=%v: the riding selection uses an attribute that "+
					"drifts (%v)", focused, pane, riding.Attrs)
			}
			if riding.Attrs&style.StyleBold == 0 {
				t.Errorf("focused=%v pane=%v: the riding selection is not bold, so it has "+
					"only a colour to be told apart by", focused, pane)
			}
			// It takes the ordinary selection's ground as its ink, so a scheme
			// that says what a selection looks like has said what this does.
			if want := scheme.GetEditBoxSelection(focused, pane).Bg; riding.Fg != want {
				t.Errorf("focused=%v pane=%v: the riding ink is %v, want the selection's "+
					"own ground %v", focused, pane, riding.Fg, want)
			}
		}
	}
}

// A stretch splits where the answer changes and nowhere else, so a selection
// that crosses from a run which cannot carry a fill into one that can is
// painted as two pieces and not as one.
func TestASelectionSplitsWhereTheAnswerChanges(t *testing.T) {
	type piece struct {
		from, to int
		giveUp   bool
	}
	got := func(giveUp []bool, from, to int) []piece {
		var out []piece
		for _, s := range fillStretches(giveUp, from, to) {
			out = append(out, piece{s.from, s.to, s.giveUp})
		}
		return out
	}
	same := func(a, b []piece) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	// No answer at all -- every field on a terminal that places what it is sent
	// where it was put -- is one stretch wearing the bar.
	if want := []piece{{2, 7, false}}; !same(got(nil, 2, 7), want) {
		t.Errorf("with nothing given up: %v, want %v", got(nil, 2, 7), want)
	}

	// Two runs of one answer are one stretch, not two.
	flat := []bool{true, true, true, true}
	if want := []piece{{0, 4, true}}; !same(got(flat, 0, 4), want) {
		t.Errorf("with one answer throughout: %v, want %v", got(flat, 0, 4), want)
	}

	// And a change in the middle splits it there.
	mixed := []bool{false, false, true, true, false}
	want := []piece{{0, 2, false}, {2, 4, true}, {4, 5, false}}
	if !same(got(mixed, 0, 5), want) {
		t.Errorf("across a change: %v, want %v", got(mixed, 0, 5), want)
	}

	// A selection covering part of it splits only inside its own range.
	if want := []piece{{1, 2, false}, {2, 3, true}}; !same(got(mixed, 1, 3), want) {
		t.Errorf("inside a range: %v, want %v", got(mixed, 1, 3), want)
	}

	// Nothing selected is nothing painted.
	if p := got(mixed, -1, 3); p != nil {
		t.Errorf("with no selection: %v, want nothing", p)
	}
	if p := got(mixed, 3, 3); p != nil {
		t.Errorf("with an empty selection: %v, want nothing", p)
	}
}
