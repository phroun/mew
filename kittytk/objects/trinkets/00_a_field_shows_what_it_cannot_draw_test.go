package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// A character a renderer must not be handed -- a control code, an ill-formed
// combining mark -- is shown as a stand-in that takes room of its own, and the
// field treats the whole stand-in as ONE thing: the caret covers all of it, and
// it is the field's own notation, so it is coloured as such.
func TestAFieldShowsWhatItCannotDraw(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	// A tab, which a single-line field has no tab stops for, between two
	// letters. It shows as ^I: two cells, one character.
	const text = "a\tb"
	runes := []rune(text)

	for _, graphical := range []bool{false, true} {
		if graphical {
			px, err := raster.New(600, 200)
			if err != nil {
				t.Fatal(err)
			}
			core.SetTextMeasurer(px)
		} else {
			core.SetTextMeasurer(nil)
		}
		ti := NewTextInput()
		ti.SetText(text)
		ti.SetBounds(core.UnitRect{Width: 30 * 8, Height: 16})
		g := ti.runGeometry(runes, ti.EffectiveFont(), graphical, false, 0)

		aLo, aHi, _ := g.boxOf(0)
		tabLo, tabHi, ok := g.boxOf(1)
		if !ok {
			t.Fatalf("graphical=%v: the tab was given no box at all", graphical)
		}
		bLo, _, _ := g.boxOf(2)

		// It sits between its neighbours, and it is wider than one of them:
		// two characters of notation stand where one character was.
		if tabLo < aHi || bLo < tabHi {
			t.Errorf("graphical=%v: the stand-in at [%d,%d) does not sit between "+
				"[%d,%d) and %d", graphical, tabLo, tabHi, aLo, aHi, bLo)
		}
		if tabHi-tabLo <= aHi-aLo {
			t.Errorf("graphical=%v: the stand-in is %d wide and a letter is %d; "+
				"^I takes two characters' room", graphical, tabHi-tabLo, aHi-aLo)
		}

		// The caret covers the whole of it rather than a sliver.
		if lo, hi := g.caretBox(1, 8); lo != tabLo || hi != tabHi {
			t.Errorf("graphical=%v: the caret on the stand-in covers [%d,%d), want [%d,%d)",
				graphical, lo, hi, tabLo, tabHi)
		}

		// And it is marked as the field's own notation, over its whole width.
		found := false
		for _, m := range g.marks {
			if m.x == tabLo && m.w == tabHi-tabLo {
				found = true
			}
		}
		if !found {
			t.Errorf("graphical=%v: the stand-in at [%d,%d) was not marked as notation (marks %v)",
				graphical, tabLo, tabHi, g.marks)
		}
	}
}

// A combining mark with no base it can attach to is not text, and it cannot be
// drawn as zero-width: a shaper that rejects the pairing falls back to a
// spacing glyph, advancing a cell the field never budgeted. It gets a circle to
// sit on and a cell of its own.
func TestAnIsolatedMarkGetsRoomOfItsOwn(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	// A Hebrew point on a Latin base: no script that pairing belongs to.
	runes := []rune{'a', 'ִ', 'b'}
	ti := NewTextInput()
	ti.SetText(string(runes))
	ti.SetBounds(core.UnitRect{Width: 30 * 8, Height: 16})
	g := ti.runGeometry(runes, ti.EffectiveFont(), false, false, 0)

	lo, hi, ok := g.boxOf(1)
	if !ok || hi <= lo {
		t.Fatalf("the isolated mark got no room: [%d,%d)", lo, hi)
	}
	// It does not share the letter before it -- that is exactly the pairing
	// that is wrong.
	if aLo, _, _ := g.boxOf(0); lo == aLo {
		t.Errorf("the isolated mark shares the box of the base it cannot attach to at %d", lo)
	}
	if bLo, _, _ := g.boxOf(2); bLo <= lo {
		t.Errorf("the letter after the mark sits at %d, not past the mark at %d", bLo, lo)
	}
}

// The direction markers are for working on the text, so they show while the
// field is focused and the plain picture comes back when it is not.
func TestDirectionMarkersComeAndGoWithTheFocus(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	const mixed = "ab שלום cd"
	runes := []rune(mixed)

	form := NewPanel()
	ti := NewTextInput()
	form.AddChild(ti)
	ti.SetText(mixed)
	ti.SetBounds(core.UnitRect{Width: 40 * 8, Height: 16})

	// On unless it is turned off, and even then only while focused.
	if !ti.ShowBidiControls() {
		t.Error("a field that was not asked either way keeps the marks off")
	}
	if ti.markersShown() {
		t.Error("an unfocused field shows markers")
	}
	ti.SetFocus()
	if !ti.markersShown() {
		t.Fatal("a focused field shows no markers")
	}
	ti.SetShowBidiControls(false)
	if ti.markersShown() {
		t.Error("a field asked for none shows them anyway")
	}
	ti.SetShowBidiControls(true)

	plain := ti.runGeometry(runes, ti.EffectiveFont(), false, false, 0)
	marked := ti.runGeometry(runes, ti.EffectiveFont(), false, true, 0)
	if len(marked.marks) == 0 {
		t.Fatal("the marked run carries no markers")
	}
	if marked.total <= plain.total {
		t.Errorf("the marked run is %d wide and the plain one %d; markers take room",
			marked.total, plain.total)
	}
	// The markers land where the reading turns, which is inside the line, not
	// only at its ends.
	inside := false
	for _, m := range marked.marks {
		if m.x > 0 && m.x+m.w < marked.total {
			inside = true
		}
	}
	if !inside {
		t.Errorf("every marker sits at an end of the run (%v); a line that turns "+
			"over turns somewhere in the middle", marked.marks)
	}
}

// A click lands on the character DRAWN under it. On a line that turns over
// that is not the character a prefix of that width ends at -- the two are at
// opposite ends of the word.
func TestAClickFindsTheCharacterUnderIt(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	const shalom = "שלום" // drawn ם ו ל ש: the first letter is the rightmost
	ti := NewTextInput()
	ti.SetText(shalom)
	ti.SetBounds(core.UnitRect{Width: 30 * 8, Height: 16})
	font := ti.EffectiveFont()

	g := ti.runGeometry([]rune(shalom), font, false, false, 0)
	for i := 0; i < 4; i++ {
		lo, hi, _ := g.boxOf(i)
		// The trailing half of a right-to-left letter is its LEFT half, so a
		// click there asks for the position after it.
		if got := ti.findCharAtX(lo+(hi-lo)/4, font); got != i+1 {
			t.Errorf("a click on the trailing half of letter %d found %d, want %d", i, got, i+1)
		}
		if got := ti.findCharAtX(hi-(hi-lo)/4, font); got != i {
			t.Errorf("a click on the leading half of letter %d found %d, want %d", i, got, i)
		}
	}
}
