package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// A caret's SHAPE says what the field is: a bar between two characters, where
// text goes in, and a block on the character you are at, where it does not.
// A field is always in insert mode, so it takes the bar; a read-only one has a
// position but no insertion point, so it takes the block.
//
// On a cell surface both are the terminal's OWN caret, asked for by DECSCUSR --
// the terminal draws it, blinks it on the reader's settings, and puts it on the
// cell grid. A field painting its own there would stand a second caret beside
// the real one.
func TestTheCaretSaysWhatTheFieldIs(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	ask := func(readOnly bool) core.TextCaret {
		px, err := raster.New(600, 200)
		if err != nil {
			t.Fatal(err)
		}
		ink := &cellInk{RenderBackend: px}
		form := NewPanel()
		ti := NewTextInput()
		form.AddChild(ti)
		ti.SetText("hello")
		ti.SetReadOnly(readOnly)
		ti.SetCursorPosition(2)
		ti.SetBounds(core.UnitRect{Width: 30 * 8, Height: 16})
		ti.SetFocus()
		p := core.NewPainter(ink)
		ti.Paint(p)
		return p.TextCaretRequest()
	}

	editable := ask(false)
	if !editable.Visible {
		t.Fatal("an editable field asked for no caret at all")
	}
	if editable.Style != decscusrBar {
		t.Errorf("an editable field asked for shape %d, want the bar (%d)",
			editable.Style, decscusrBar)
	}

	reading := ask(true)
	if !reading.Visible {
		t.Fatal("a read-only field asked for no caret at all")
	}
	if reading.Style != decscusrBlock {
		t.Errorf("a read-only field asked for shape %d, want the block (%d)",
			reading.Style, decscusrBlock)
	}

	// Either way it is also where typing goes, so an input method has an
	// insertion point to anchor on.
	if !editable.InputArea || !reading.InputArea {
		t.Error("the caret was placed without saying it is the insertion point")
	}

	// And it says what colour to be, from the theme. A terminal's caret colour
	// is one global preference, chosen against the terminal's own background,
	// and a thin bar in it disappears into the ground a field paints for
	// itself -- which the theme that chose that ground is the thing that knows.
	scheme := NewTextInput().GetScheme()
	if want := scheme.GetFocusedEditBoxCaret(); editable.Color != want {
		t.Errorf("the bar asked for colour %v, want the theme's caret ink %v",
			editable.Color, want)
	}
	if want := scheme.GetFocusedEditBoxCursor().Bg; reading.Color != want {
		t.Errorf("the block asked for colour %v, want the theme's block ground %v",
			reading.Color, want)
	}
	if editable.Color == style.ColorDefault {
		t.Error("the field left the caret in whatever colour the terminal had")
	}
}

// The caret's ink is the THEME's to set: a bar is a few pixels wide against
// whatever ground the field is painted in, and the scheme that chose that
// ground is the thing that knows what shows up on it.
func TestAThemeSetsTheCaretsInk(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	const id = style.SchemeID(4210)
	mine := style.DefaultScheme()
	mine.FocusedEditBoxCaret = nil
	fallback := mine.GetFocusedEditBoxCaret()
	if want := mine.GetFocusedEditBoxCursor().Bg; fallback != want {
		t.Errorf("a scheme naming no caret falls back to %v, want the block's ground %v",
			fallback, want)
	}

	want := style.RGB(0x33, 0xCC, 0x99)
	mine.FocusedEditBoxCaret = &want
	if got := mine.GetFocusedEditBoxCaret(); got != want {
		t.Fatalf("the scheme's own caret ink came back as %v, want %v", got, want)
	}
	style.GlobalSchemeRegistry().Register(id, mine)
	t.Cleanup(func() { style.GlobalSchemeRegistry().Register(id, style.DefaultScheme()) })

	px, err := raster.New(600, 200)
	if err != nil {
		t.Fatal(err)
	}
	ink := &cellInk{RenderBackend: px}
	form := NewPanel()
	ti := NewTextInput()
	form.AddChild(ti)
	ti.SetScheme(id)
	ti.SetText("hello")
	ti.SetCursorPosition(2)
	ti.SetBounds(core.UnitRect{Width: 30 * 8, Height: 16})
	ti.SetFocus()
	p := core.NewPainter(ink)
	ti.Paint(p)

	if got := p.TextCaretRequest().Color; got != want {
		t.Errorf("the field asked for caret ink %v, want the scheme's %v", got, want)
	}
}

// The bar and the block are asked for at DIFFERENT cells, because they mean
// different things about one position. A bar sits at the left edge of the cell
// it is given, so it goes at the caret's leading edge -- which on a
// right-to-left character is the cell AFTER it. A block covers a cell, so it
// goes on the cell the character itself occupies.
func TestTheBarAndTheBlockLandOnTheirOwnCells(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	const shalom = "שלום" // drawn ם ו ל ש: the first letter is the rightmost

	ask := func(readOnly bool, at int) core.Unit {
		px, err := raster.New(600, 200)
		if err != nil {
			t.Fatal(err)
		}
		ink := &cellInk{RenderBackend: px}
		form := NewPanel()
		ti := NewTextInput()
		form.AddChild(ti)
		ti.SetText(shalom)
		// The marks are their own layout; this is about where the caret lands
		// in one, so they are off and the run is the plain one.
		ti.SetShowBidiControls(false)
		ti.SetReadOnly(readOnly)
		ti.SetCursorPosition(at)
		ti.SetBounds(core.UnitRect{Width: 30 * 8, Height: 16})
		ti.SetFocus()
		p := core.NewPainter(ink)
		ti.Paint(p)
		return p.TextCaretRequest().X
	}

	// The geometry the two are read against.
	form := NewPanel()
	probe := NewTextInput()
	form.AddChild(probe)
	probe.SetText(shalom)
	probe.SetBounds(core.UnitRect{Width: 30 * 8, Height: 16})
	g := probe.runGeometry([]rune(shalom), probe.EffectiveFont(), false, false, 0)

	for _, at := range []int{0, 1, 2, 3} {
		lo, hi, _ := g.boxOf(at)
		if got := ask(true, at); got != lo {
			t.Errorf("the block before letter %d landed at %d, want its own cell at %d",
				at, got, lo)
		}
		// The letter reads right to left, so the caret before it is at its
		// RIGHT edge, and a bar there belongs to the cell starting there.
		if got := ask(false, at); got != hi {
			t.Errorf("the bar before letter %d landed at %d, want the right edge of "+
				"its cell at %d", at, got, hi)
		}
	}
}

// At a direction change one insertion point stands in two places on the line:
// what is typed next lands at whichever end matches its own direction. The
// second caret marks the other one -- one blank past the character BEFORE the
// caret, in that character's own direction.
func TestTheSecondCaretMarksTheOtherEndOfTheTurn(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	const mixed = "ab שלום cd"
	runes := []rune(mixed)

	form := NewPanel()
	ti := NewTextInput()
	form.AddChild(ti)
	ti.SetText(mixed)
	ti.SetBounds(core.UnitRect{Width: 40 * 8, Height: 16})
	g := ti.runGeometry(runes, ti.EffectiveFont(), false, false, 0)

	// Inside one direction there is no ambiguity and no second caret.
	for _, at := range []int{1, 2, 5, 6} {
		if _, _, ok := g.secondaryCaretAt(runes, at); ok && g.rtl[at] == g.rtl[at-1] {
			t.Errorf("a second caret at %d, which is not a boundary", at)
		}
	}

	// At a boundary there is one, and it rests past the character BEFORE the
	// caret rather than on it.
	found := 0
	for at := 1; at < len(runes); at++ {
		q, leftOf, ok := g.secondaryCaretAt(runes, at)
		if !ok {
			continue
		}
		found++
		if g.rtl[at] == g.rtl[at-1] {
			t.Errorf("a second caret at %d, which is not a boundary", at)
		}
		if q != at-1 {
			t.Errorf("the second caret at %d rests on %d, want the character before it", at, q)
		}
		if leftOf != g.rtl[q] {
			t.Errorf("the second caret at %d sits leftOf=%v of a character reading rtl=%v",
				at, leftOf, g.rtl[q])
		}
		// One blank past that character, on the side it reads away to.
		lo, hi, _ := g.boxOf(q)
		blank := ti.blankWidth()
		want := [2]core.Unit{hi, hi + blank}
		if leftOf {
			want = [2]core.Unit{lo - blank, lo}
		}
		if want[0] < 0 {
			t.Errorf("the second caret at %d falls off the left edge", at)
		}
		// It never lands on the character the primary is on.
		pLo, pHi := g.caretBox(at, blank)
		if want[0] < pHi && pLo < want[1] {
			t.Errorf("the second caret at %d covers [%d,%d), overlapping the primary at [%d,%d)",
				at, want[0], want[1], pLo, pHi)
		}
	}
	if found == 0 {
		t.Fatal("a line that turns over twice offered no second caret anywhere")
	}
}

// And it is painted. The terminal draws the one caret it has, so the second is
// the field's own work on either path -- a reverse-video cell where the primary
// is a terminal block, a short bar where the primary is a bar.
func TestTheSecondCaretIsPainted(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	const mixed = "ab שלום cd"
	runes := []rune(mixed)

	// The boundary to stand on: the first position whose reading differs from
	// the character before it.
	probe := NewTextInput()
	probe.SetShowBidiControls(false)
	probe.SetText(mixed)
	probe.SetBounds(core.UnitRect{Width: 40 * 8, Height: 16})
	core.SetTextMeasurer(nil)
	pg := probe.runGeometry(runes, probe.EffectiveFont(), false, false, 0)
	at := -1
	for i := 1; i < len(runes); i++ {
		if _, _, ok := pg.secondaryCaretAt(runes, i); ok {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatal("no boundary in a line that turns over")
	}

	// The cell path: the second caret is a cell drawn in the cursor's colours.
	px, err := raster.New(600, 200)
	if err != nil {
		t.Fatal(err)
	}
	ink := &noteInk{RenderBackend: px}
	ti := NewTextInput()
	ti.SetShowBidiControls(false)
	ti.SetText(mixed)
	ti.SetBounds(core.UnitRect{Width: 40 * 8, Height: 16})
	ti.SetFocus()
	ti.SetCursorPosition(at)
	ti.Paint(core.NewPainter(ink))

	want := ti.GetScheme().GetFocusedEditBoxCursor()
	second := false
	for i := range ink.texts {
		if ink.styles[i].Fg == want.Fg && ink.styles[i].Bg == want.Bg {
			second = true
		}
	}
	if !second {
		t.Errorf("at the boundary the cell path painted no second caret (stamped %v)", ink.texts)
	}

	// Away from a boundary it paints none.
	ink2 := &noteInk{RenderBackend: px}
	ti.SetCursorPosition(1)
	ti.Paint(core.NewPainter(ink2))
	for i := range ink2.texts {
		if ink2.styles[i].Fg == want.Fg && ink2.styles[i].Bg == want.Bg {
			t.Errorf("inside one direction the cell path painted a second caret (%q)", ink2.texts[i])
		}
	}

	// The pixel path: one more fill than the same field draws away from a
	// boundary, which is the short bar.
	core.SetTextMeasurer(px)
	count := func(pos int) int {
		base, err := raster.New(600, 200)
		if err != nil {
			t.Fatal(err)
		}
		rec := &edgeFillRecorder{Backend: base}
		f := NewTextInput()
		f.SetShowBidiControls(false)
		f.SetText(mixed)
		f.SetBounds(core.UnitRect{Width: 40 * 8, Height: 16})
		f.SetFocus()
		f.caretOn = true
		f.SetCursorPosition(pos)
		f.Paint(core.NewPainter(rec))
		return len(rec.fills)
	}
	if boundary, inside := count(at), count(1); boundary <= inside {
		t.Errorf("at the boundary the pixel path made %d fills and inside a run %d; "+
			"the second caret is one more", boundary, inside)
	}
}
