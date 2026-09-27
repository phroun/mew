package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// A field narrower than its text shows a window onto the RUN, and the caret is
// always inside it -- with room to spare on the side it is heading toward, and
// never under the arrow saying the text carries on that way.
func TestAFieldNeverLosesItsCaret(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	const cell = core.Unit(8)
	long := strings.Repeat("abcdefghij", 6) // 60 characters into a 10-cell field

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
		ti.SetText(long)
		ti.SetBounds(core.UnitRect{Width: 10 * cell, Height: 16})

		runes := []rune(long)
		g := ti.runGeometry(runes, ti.EffectiveFont(), graphical, false, 0)
		mark := ti.markWidth()

		for _, caret := range []int{0, 1, 12, 30, 59, len(runes)} {
			scroll, usable, left, right := ti.window(g, caret, ti.Bounds().Width,
				ti.blankWidth(), 0, ti.scrollQuantum())

			// The room the run has is what is left after the arrows, and the
			// caret's whole box is inside it.
			lo, hi := g.caretBox(caret, ti.blankWidth())
			roomLo := core.Unit(0)
			if left {
				roomLo = mark
			}
			if lo-scroll+roomLo < roomLo {
				t.Errorf("graphical=%v caret=%d: the caret starts at %d, left of the room at %d",
					graphical, caret, lo-scroll+roomLo, roomLo)
			}
			if hi-scroll+roomLo > roomLo+usable {
				t.Errorf("graphical=%v caret=%d: the caret ends at %d, right of the room at %d",
					graphical, caret, hi-scroll+roomLo, roomLo+usable)
			}

			// The arrows tell the truth about what is out of sight.
			if want := scroll > 0; left != want {
				t.Errorf("graphical=%v caret=%d: left arrow %v at scroll %d", graphical, caret, left, scroll)
			}
			if want := scroll+usable < g.total; right != want {
				t.Errorf("graphical=%v caret=%d: right arrow %v at scroll %d of %d",
					graphical, caret, right, scroll, g.total)
			}
		}

		// At the very start nothing is hidden to the left, and at the very end
		// nothing is hidden to the right.
		if _, _, left, _ := ti.window(g, 0, ti.Bounds().Width, ti.blankWidth(), 0, ti.scrollQuantum()); left {
			t.Errorf("graphical=%v: a caret at the start still hid text to its left", graphical)
		}
		if _, _, _, right := ti.window(g, len(runes), ti.Bounds().Width, ti.blankWidth(), 0, ti.scrollQuantum()); right {
			t.Errorf("graphical=%v: a caret past the end still hid text to its right", graphical)
		}
	}
}

// The field looks ahead of the caret: what is being typed toward stays in
// sight rather than the caret sitting on the edge it is walking to.
func TestAFieldLooksAheadOfTheCaret(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	const cell = core.Unit(8)
	long := strings.Repeat("abcdefghij", 6)
	runes := []rune(long)

	ti := NewTextInput()
	ti.SetText(long)
	ti.SetBounds(core.UnitRect{Width: 10 * cell, Height: 16})
	g := ti.runGeometry(runes, ti.EffectiveFont(), false, false, 0)

	// Walking forward from the left edge: with three characters of look-ahead
	// the caret stops three characters short of the room's right edge.
	ti.SetShowAhead(3)
	scroll, usable, left, _ := ti.window(g, 30, ti.Bounds().Width, cell, 0, ti.scrollQuantum())
	room := core.Unit(0)
	if left {
		room = ti.markWidth()
	}
	_, hi := g.caretBox(30, cell)
	if gap := room + usable - (hi - scroll + room); gap < 3*cell {
		t.Errorf("look-ahead of 3 left %d units past the caret, want at least %d", gap, 3*cell)
	}

	// None at all, and the caret goes right up to the edge.
	ti.SetShowAhead(0)
	scroll, usable, left, _ = ti.window(g, 30, ti.Bounds().Width, cell, 0, ti.scrollQuantum())
	room = 0
	if left {
		room = ti.markWidth()
	}
	_, hi = g.caretBox(30, cell)
	if gap := room + usable - (hi - scroll + room); gap != 0 {
		t.Errorf("no look-ahead left %d units past the caret, want none", gap)
	}
}

// A field that draws cells puts its own left edge BETWEEN two characters, not
// through one: a cell target cannot show half a glyph, so a scroll that landed
// mid-character would draw the whole of it a fraction of a cell off.
func TestACellFieldScrollsWholeCells(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	long := strings.Repeat("abcdefghij", 6)
	ti := NewTextInput()
	ti.SetText(long)
	cw := ti.EffectiveCellMetrics().UnitsPerCellWidth
	// A width that is NOT a whole number of cells: the room has to be rounded
	// down before the scroll is worked out, or the edge lands inside a cell.
	ti.SetBounds(core.UnitRect{Width: 10*cw + 3, Height: 16})
	g := ti.runGeometry([]rune(long), ti.EffectiveFont(), false, false, 0)

	for _, caret := range []int{5, 17, 33, 59} {
		scroll, _, _, _ := ti.window(g, caret, ti.Bounds().Width, cw, 0, ti.scrollQuantum())
		if scroll%cw != 0 {
			t.Errorf("caret=%d: the field's left edge fell %d units into a cell", caret, scroll%cw)
		}
	}
}

// A line whose reading ENDS on the left -- a right-to-left word finishing the
// text -- has its last caret position out past the leftmost letter. The run
// keeps room there for it, so the caret has somewhere to be drawn instead of
// standing off the edge of the field.
func TestTheCaretPastAWordThatReadsLeftHasSomewhereToGo(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	const shalom = "שלום"
	runes := []rune(shalom)

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
		ti.SetText(shalom)
		ti.SetBounds(core.UnitRect{Width: 30 * 8, Height: 16})
		blank := ti.blankWidth()
		g := ti.runGeometry(runes, ti.EffectiveFont(), graphical, false, 0)

		if g.head <= 0 {
			t.Errorf("graphical=%v: the run reserved %d at its left end", graphical, g.head)
		}
		lo, hi := g.caretBox(len(runes), blank)
		if lo < 0 || hi <= lo {
			t.Errorf("graphical=%v: the last caret box is [%d,%d)", graphical, lo, hi)
		}
		// It sits in the reserved room, not on the last letter.
		if last, _, _ := g.boxOf(len(runes) - 1); hi > last {
			t.Errorf("graphical=%v: the last caret box [%d,%d) runs into the leftmost "+
				"letter at %d", graphical, lo, hi, last)
		}

		// And the window keeps it: scrolled to 0, the caret is on screen.
		scroll, usable, _, _ := ti.window(g, len(runes), ti.Bounds().Width, blank, 0,
			ti.scrollQuantum())
		if lo-scroll < 0 || hi-scroll > usable {
			t.Errorf("graphical=%v: the last caret box [%d,%d) at scroll %d falls outside "+
				"the %d of room", graphical, lo, hi, scroll, usable)
		}

		// A line that reads to the RIGHT wants nothing reserved: its last caret
		// position is past the last letter, where the field already has room.
		plain := ti.runGeometry([]rune("abc"), ti.EffectiveFont(), graphical, false, 0)
		if plain.head != 0 {
			t.Errorf("graphical=%v: a left-to-right line reserved %d it does not use",
				graphical, plain.head)
		}
	}
}
