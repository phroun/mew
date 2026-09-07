package trinkets

import (
	"sort"
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// markerRow reads the marked layout back as a picture: the marks in the places
// they landed, with a dot for each character of text between them, left to
// right. It is the same shape mew's own marked run has, which is what makes the
// two comparable by eye and by test.
func markerRow(g *fieldGeometry, runes []rune) string {
	type slot struct {
		x    core.Unit
		text string
	}
	var slots []slot
	for _, m := range g.marks {
		slots = append(slots, slot{x: m.x, text: m.text})
	}
	for i := range runes {
		lo, hi, ok := g.boxOf(i)
		if !ok || hi <= lo {
			continue
		}
		slots = append(slots, slot{x: lo, text: "."})
	}
	sort.SliceStable(slots, func(i, j int) bool { return slots[i].x < slots[j].x })
	var b strings.Builder
	for _, s := range slots {
		b.WriteString(s.text)
	}
	return b.String()
}

// The direction marks say where the line's reading turns and which way each
// piece of it goes -- and they do it on the graphical path, where the shaper
// owns the ordering, as much as on the cell path where the field does.
//
// A fragment carries the arrow it reads AWAY from at its reading start and a
// bar where its reading stops, so a right-to-left piece wears "<" on its right
// and "|" on its left.
//
// The line's own ends carry no notation: the piece a reader starts at goes bare
// when it reads the way the whole line does, and the piece it stops at closes
// without a bar for the same reason -- there is nowhere for the eye to jump.
func TestTheMarkersShowWhereTheReadingTurns(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 200)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(px)

	for _, c := range []struct {
		name string
		text string
		want string
	}{
		// Nothing turns: one piece, read the way the line is.
		{"english", "abc", "..."},
		{"hebrew", "שלום", "...."},
		// English, then a Hebrew word, then English again. The Hebrew reads
		// from its right, so its arrow is on its right and its bar on its left.
		// The English after it starts a new piece, so it takes an arrow -- but
		// it ends the line reading the way the line does, so no bar closes it.
		{"hebrew inside english", "abc שלום xyz", "....|....<>...."},
		// The mirror: the line begins right-to-left, so the first piece goes
		// bare, the English island inside it is marked at both ends, and the
		// Hebrew that ends the line closes without a bar.
		{"english inside hebrew", "אבג abc דהו", "....<>...|...."},
	} {
		t.Run(c.name, func(t *testing.T) {
			ti := NewTextInput()
			ti.SetText(c.text)
			ti.SetBounds(core.UnitRect{Width: 60 * 8, Height: 16})
			runes := []rune(c.text)
			g := ti.runGeometry(runes, ti.EffectiveFont(), true, true, 0)
			if got := markerRow(g, runes); got != c.want {
				t.Errorf("%q laid out as %q, want %q", c.text, got, c.want)
			}

			// And the same marks, in the same places, as the cell path puts on
			// the same line read the same way. Two paths that marked one piece
			// of text differently would be two accounts of it, and a reader
			// moving between them would have to learn both.
			//
			// The line's own direction has to be said out loud here: the cell
			// path takes it from the field, and the shaper takes it from the
			// first strong character.
			dir := core.DirLTR
			if g.rtl[0] {
				dir = core.DirRTL
			}
			form := NewPanel()
			form.SetDirection(dir)
			cellField := NewTextInput()
			form.AddChild(cellField)
			cellField.SetText(c.text)
			cellField.SetBounds(ti.Bounds())
			cells := cellField.runGeometry(runes, cellField.EffectiveFont(), false, true, 0)
			if got := markerRow(cells, runes); got != c.want {
				t.Errorf("%q on the cell path laid out as %q, want %q", c.text, got, c.want)
			}
		})
	}
}

// The marks cost room and nothing else: the pieces of the line keep the glyphs
// and the order the shaper gave them, moved along to make space. So a marked
// run is wider than the plain one by exactly what its marks take, and every
// piece of it has moved right, never left.
func TestMarkingTheLineOnlyMovesIt(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 200)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(px)

	const mixed = "abc שלום xyz"
	runes := []rune(mixed)

	ti := NewTextInput()
	ti.SetText(mixed)
	ti.SetBounds(core.UnitRect{Width: 60 * 8, Height: 16})

	plain := ti.runGeometry(runes, ti.EffectiveFont(), true, false, 0)
	marked := ti.runGeometry(runes, ti.EffectiveFont(), true, true, 0)

	var marks core.Unit
	for _, m := range marked.marks {
		marks += m.w
	}
	if marks == 0 {
		t.Fatal("a line that turns over twice carries no marks")
	}
	if marked.total != plain.total+marks {
		t.Errorf("the marked run is %d wide and the plain one %d plus %d of marks",
			marked.total, plain.total, marks)
	}

	// Every box keeps its width and moves only to the right.
	for i := range runes {
		pl, ph, _ := plain.boxOf(i)
		ml, mh, _ := marked.boxOf(i)
		if mh-ml != ph-pl {
			t.Errorf("rune %d is %d wide marked and %d plain", i, mh-ml, ph-pl)
		}
		if ml < pl {
			t.Errorf("rune %d moved left, from %d to %d", i, pl, ml)
		}
	}

	// And the pieces cover the whole of the shaper's line between them.
	var covered core.Unit
	for _, pc := range marked.pieces {
		if pc.shift < 0 {
			t.Errorf("a piece moved left by %d", -pc.shift)
		}
		covered += pc.hi - pc.lo
	}
	if covered != plain.total {
		t.Errorf("the pieces cover %d of the shaper's %d-wide line", covered, plain.total)
	}

	// In PIXELS too, which is the denomination they are drawn in: each piece is
	// a stretch of the run with room of its own, and the pieces come in order
	// with no two over the same ground. A piece measured across its neighbour
	// stamps the whole run twice into one place, which shows as a second,
	// half-clipped copy of the text.
	pixels := ti.runGeometry(runes, ti.EffectiveFont(), true, true, 2)
	if len(pixels.pieces) < 2 {
		t.Fatalf("a line that turns over twice was drawn in %d piece(s)", len(pixels.pieces))
	}
	for i, pc := range pixels.pieces {
		if pc.hiPx <= pc.loPx {
			t.Errorf("piece %d spans [%d,%d) pixels", i, pc.loPx, pc.hiPx)
		}
		if i > 0 && pc.loPx < pixels.pieces[i-1].hiPx {
			t.Errorf("piece %d starts at %d, inside piece %d which runs to %d",
				i, pc.loPx, i-1, pixels.pieces[i-1].hiPx)
		}
	}
}

// A marked field is still a field: the caret sits on the character it precedes,
// and the marks around it do not move it onto one of themselves.
func TestTheCaretKeepsItsPlaceAmongTheMarkers(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 200)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(px)

	const mixed = "abc שלום xyz"
	runes := []rune(mixed)

	ti := NewTextInput()
	ti.SetText(mixed)
	ti.SetBounds(core.UnitRect{Width: 60 * 8, Height: 16})
	g := ti.runGeometry(runes, ti.EffectiveFont(), true, true, 0)

	for i := range runes {
		lo, hi := g.caretBox(i, 8)
		bLo, bHi, _ := g.boxOf(i)
		if lo != bLo || hi != bHi {
			t.Errorf("the caret before rune %d covers [%d,%d), want its box [%d,%d)",
				i, lo, hi, bLo, bHi)
		}
		for _, m := range g.marks {
			if lo < m.x+m.w && m.x < hi {
				t.Errorf("the caret before rune %d at [%d,%d) overlaps the mark %q at [%d,%d)",
					i, lo, hi, m.text, m.x, m.x+m.w)
			}
		}
	}
}
