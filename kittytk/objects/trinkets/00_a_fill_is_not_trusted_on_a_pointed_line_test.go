package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// A terminal that reorders what it is sent counts codepoints where the grid
// counts cells, so a background fill over a line still carrying combining marks
// lands on the wrong cells and half-vanishes -- on a pointed Hebrew line, most
// of the selection gone. Foreground colour and weight ride each glyph through
// that reordering intact, so such a line wears those instead.
//
// It is settled per LINE, because folding settles it: a point that folds into
// its base no longer inflates the count.
func TestAFillIsNotTrustedOnAPointedLine(t *testing.T) {
	t.Cleanup(func() {
		core.SetTextMeasurer(nil)
		core.ForgetHostBidi()
		core.SetRtlMarkMode("")
	})
	core.SetTextMeasurer(nil)

	// Selected text, and what the field stamped it in.
	selectionInk := func(text string) (style.CellStyle, bool) {
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
		riding := ti.GetScheme().GetEditBoxSelectionRiding(true, style.PaneDefault)
		for i, st := range ink.styles {
			_ = i
			if st.Fg == riding.Fg && st.Bg == riding.Bg && st.Attrs&style.StyleBold != 0 {
				return riding, true
			}
			if st.Fg == plain.Fg && st.Bg == plain.Bg {
				return plain, false
			}
		}
		return style.CellStyle{}, false
	}

	// A vowel has no presentation form to fold into, so it survives.
	const pointed = "לִ"
	// A shin dot folds into its base, so that line comes out even.
	const folds = "שׁ"
	// And English carries nothing zero-width at all.
	const plain = "hello"

	// On a stream-order host nothing is in question: the ordinary bar throughout.
	core.SetHostAppliesBidi(false, false, false)
	for _, text := range []string{pointed, folds, plain} {
		if _, riding := selectionInk(text); riding {
			t.Errorf("on a stream-order host %q gave up the selection bar", text)
		}
	}

	// On a host that miscounts a fill, a line whose marks survive wears the
	// riding selection and the others keep the bar.
	core.SetHostAppliesBidi(true, false, true)
	core.SetRtlMarkMode("") // nothing folds: the point survives too
	if _, riding := selectionInk(pointed); !riding {
		t.Error("a pointed line kept a fill the host cannot place")
	}
	if _, riding := selectionInk(plain); riding {
		t.Error("a line with nothing zero-width gave up the selection bar")
	}

	// With folding on, a point that HAS a presentation form no longer counts.
	core.SetRtlMarkMode("compose")
	if _, riding := selectionInk(folds); riding {
		t.Error("a line whose points fold into their base gave up the selection bar")
	}
	if _, riding := selectionInk(pointed); !riding {
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
