package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// A caret, a selection and a composition all ask the same thing: which part of
// the DRAWN run is this stretch of the text? Measuring the text before the
// position answers it only while the text reads one way -- the first letter of
// a Hebrew word is drawn at that word's right edge, so the width before it
// names where the word ENDS.
func TestAFieldKnowsWhereItsRunesWent(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	const shalom = "שלום" // ש ל ו ם, drawn ם ו ל ש

	field := func(dir core.Direction, graphical bool) (*TextInput, *fieldGeometry) {
		px, err := raster.New(600, 200)
		if err != nil {
			t.Fatal(err)
		}
		if graphical {
			core.SetTextMeasurer(px)
		} else {
			core.SetTextMeasurer(nil)
		}
		form := NewPanel()
		form.SetDirection(dir)
		ti := NewTextInput()
		form.AddChild(ti)
		ti.SetText(shalom)
		ti.SetBounds(core.UnitRect{Width: 30 * 8, Height: 16})
		return ti, ti.runGeometry([]rune(shalom), ti.EffectiveFont(), graphical, false, 0)
	}

	for _, graphical := range []bool{false, true} {
		for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
			_, g := field(dir, graphical)

			// The word's FIRST letter is its rightmost box; its last is the
			// leftmost. That is the whole point.
			firstLo, _, ok := g.boxOf(0)
			if !ok {
				t.Fatalf("graphical=%v %v: no box for the first letter", graphical, dir)
			}
			lastLo, _, ok := g.boxOf(3)
			if !ok {
				t.Fatalf("graphical=%v %v: no box for the last letter", graphical, dir)
			}
			if firstLo <= lastLo {
				t.Errorf("graphical=%v %v: the first letter sits at %d and the last at %d; "+
					"in a right-to-left run the first letter is the rightmost",
					graphical, dir, firstLo, lastLo)
			}
			// The run starts after the room reserved for the caret's last
			// position, which on a line that reads to the left is out past the
			// leftmost letter.
			if lastLo != g.head {
				t.Errorf("graphical=%v %v: the last letter sits at %d, want the run's "+
					"left edge at %d", graphical, dir, lastLo, g.head)
			}

			// The caret covers the box of the rune it precedes, so a caret at
			// the start of the word is at the word's RIGHT end.
			blank := core.Unit(8)
			lo, hi := g.caretBox(0, blank)
			if lo != firstLo {
				t.Errorf("graphical=%v %v: the caret before the first letter is at %d, "+
					"want the letter's own box at %d", graphical, dir, lo, firstLo)
			}
			if hi <= lo {
				t.Errorf("graphical=%v %v: the caret box is %d wide", graphical, dir, hi-lo)
			}

			// Past the end there is no rune to cover, so the caret takes a
			// blank at the run's READING end -- the left edge, here.
			lo, hi = g.caretBox(4, blank)
			if hi != lastLo {
				t.Errorf("graphical=%v %v: the caret past the end runs to %d, want the "+
					"reading end of the run at %d", graphical, dir, hi, lastLo)
			}
			// And it lands INSIDE the run, in the room reserved for it: a caret
			// off the left edge is a caret nobody sees.
			if lo < 0 {
				t.Errorf("graphical=%v %v: the caret past the end starts at %d, off the "+
					"left edge", graphical, dir, lo)
			}
			if hi <= lo {
				t.Errorf("graphical=%v %v: the caret past the end is %d wide",
					graphical, dir, hi-lo)
			}
		}
	}
}

// A logical stretch is not one stretch on the line. Choose a word that crosses
// a direction change and the characters chosen sit in two places, with text
// between them that was not chosen.
func TestAChosenStretchCanSitInTwoPlaces(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	const mixed = "ab שלום cd"
	form := NewPanel()
	form.SetDirection(core.DirLTR)
	ti := NewTextInput()
	form.AddChild(ti)
	ti.SetText(mixed)
	ti.SetBounds(core.UnitRect{Width: 40 * 8, Height: 16})
	g := ti.runGeometry([]rune(mixed), ti.EffectiveFont(), false, false, 0)

	// "b שלו" -- the b, the space and three Hebrew letters: one piece of the
	// English and part of the Hebrew, which land apart.
	if got := g.spans(1, 6); len(got) < 2 {
		t.Errorf("the stretch was drawn in %d place(s) (%v); crossing a direction change "+
			"it sits in two", len(got), got)
	}
	// A stretch inside one direction is one place.
	if got := g.spans(0, 2); len(got) != 1 {
		t.Errorf("a stretch of English alone was drawn in %d places (%v), want one", len(got), got)
	}
}
