package trinkets

import (
	"image/color"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// paintField renders one focused field onto a pixel surface and returns the
// image, so what the caret actually looks like can be read off it.
func paintField(t *testing.T, prepare func(*TextInput)) (*raster.Backend, *TextInput) {
	t.Helper()
	b, err := raster.New(320, 32)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(b)
	ti := NewTextInput()
	ti.SetParent(d)
	ti.SetText("abcdef")
	ti.SetCursorPosition(2)
	prepare(ti)
	ti.SetBounds(core.UnitRect{Width: 320, Height: 16})
	ti.SetFocus()
	b.Clear(style.DefaultStyle())
	ti.Paint(core.NewPainter(b))
	return b, ti
}

func sameColor(c color.RGBA, want style.Color) bool {
	r, g, bl := want.RGBComponents()
	return c.R == r && c.G == g && c.B == bl
}

// A read-only field's caret is a BLOCK: the character it sits on is painted
// with the text's colours reversed, so the cell's background becomes the ink.
//
// It came out as a bar. Skipping the pixel-bar branch was not enough --
// DrawCaret draws a bar of its own on a pixel surface and reports that it
// did, so the block fallback below it was never reached. The block is painted
// outright now.
func TestReadOnlyCaretPaintsAnInvertedBlock(t *testing.T) {
	b, ti := paintField(t, func(ti *TextInput) { ti.SetReadOnly(true) })

	scheme := ti.GetScheme()
	ink := scheme.GetFocusedEditBoxText().Fg

	// The caret sits before the third character. Sample inside that
	// character's cell, off the glyph's own strokes by sitting high in the
	// cell where the block's fill shows.
	font := ti.EffectiveFont()
	x := int(font.MeasureText("ab")) + 1
	got := b.Image().RGBAAt(x, 2)
	if !sameColor(got, ink) {
		r, g, bl := ink.RGBComponents()
		t.Errorf("block background = %d,%d,%d, want the text ink %d,%d,%d",
			got.R, got.G, got.B, r, g, bl)
	}

	// ...and an editable field in the same place is NOT filled with the ink:
	// its caret is a thin bar between two characters, not a covered cell.
	b2, ti2 := paintField(t, func(*TextInput) {})
	font2 := ti2.EffectiveFont()
	x2 := int(font2.MeasureText("ab")) + 3 // past the bar, inside the character
	if got := b2.Image().RGBAAt(x2, 2); sameColor(got, ink) {
		t.Error("an editable field covered the character too; its caret should be a bar")
	}
}

// At the end of the text there is no character to cover, so the block takes
// one space's worth of the interior and comes out the same size.
func TestReadOnlyCaretAtTheEndPaintsABlock(t *testing.T) {
	b, ti := paintField(t, func(ti *TextInput) {
		ti.SetReadOnly(true)
		ti.SetCursorPosition(len("abcdef"))
	})

	ink := ti.GetScheme().GetFocusedEditBoxText().Fg
	font := ti.EffectiveFont()
	x := int(font.MeasureText("abcdef")) + 1
	if got := b.Image().RGBAAt(x, 2); !sameColor(got, ink) {
		r, g, bl := ink.RGBComponents()
		t.Errorf("end-of-text block = %d,%d,%d, want the text ink %d,%d,%d",
			got.R, got.G, got.B, r, g, bl)
	}
}

// Which palette the block inverts, asserted on the decision rather than on
// pixels: both foregrounds are black in the default scheme, so the block's
// FILL is the same either way and only the glyph colour differs -- and
// sampling a glyph's pixels is a test of the font, not of this.
//
// Over selected text it must be the selection's own pair. Inverting the
// ordinary pair inside a highlight either vanishes into it or clashes with it,
// and neither reads as "you are here".
func TestBlockCaretInvertsWhateverItSitsOn(t *testing.T) {
	text := style.DefaultStyle().WithFg(style.RGB(10, 10, 10)).WithBg(style.RGB(20, 20, 20))
	sel := style.DefaultStyle().WithFg(style.RGB(30, 30, 30)).WithBg(style.RGB(40, 40, 40))
	ground := style.RGB(50, 50, 50)

	plain := blockCaretStyle(text, sel, ground, false)
	if plain.Bg != text.Fg {
		t.Errorf("outside a selection the block fills with %v, want the text ink %v", plain.Bg, text.Fg)
	}
	if plain.Fg != ground {
		t.Errorf("outside a selection the glyph is %v, want the field ground %v", plain.Fg, ground)
	}

	inSel := blockCaretStyle(text, sel, ground, true)
	if inSel.Bg != sel.Fg {
		t.Errorf("inside a selection the block fills with %v, want the selection ink %v", inSel.Bg, sel.Fg)
	}
	if inSel.Fg != sel.Bg {
		t.Errorf("inside a selection the glyph is %v, want the selection ground %v", inSel.Fg, sel.Bg)
	}
	// ...and the two are genuinely different, which is the whole point.
	if plain == inSel {
		t.Error("the block looks the same inside and outside a selection")
	}
}

// The caret sits at one EDGE of a selection, never strictly inside it (the
// span runs anchor to cursor), so the block covers a selected character
// exactly when the caret is the LEFT edge. Selecting backwards puts it there.
func TestSelectingBackwardsPutsTheCaretOnASelectedCharacter(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("abcdef")
	ti.SetReadOnly(true)
	ti.SetCursorPosition(4)
	ti.HandleKeyPress(core.KeyPressEvent{Key: "S-Left"})
	ti.HandleKeyPress(core.KeyPressEvent{Key: "S-Left"})

	if got := ti.CursorPosition(); got != 2 {
		t.Fatalf("cursor = %d, want 2", got)
	}
	if got := ti.SelectedText(); got != "cd" {
		t.Fatalf("selection = %q, want cd", got)
	}
	lo, hi := ti.selStart, ti.selEnd
	if lo > hi {
		lo, hi = hi, lo
	}
	if !(ti.cursorPos >= lo && ti.cursorPos < hi) {
		t.Errorf("cursor %d is not inside the selection [%d,%d)", ti.cursorPos, lo, hi)
	}

	// Selecting FORWARDS leaves it at the right edge, where the character it
	// covers is past the selection and takes the ordinary colours.
	fwd := NewTextInput()
	fwd.SetText("abcdef")
	fwd.SetCursorPosition(2)
	fwd.HandleKeyPress(core.KeyPressEvent{Key: "S-Right"})
	fwd.HandleKeyPress(core.KeyPressEvent{Key: "S-Right"})
	lo, hi = fwd.selStart, fwd.selEnd
	if lo > hi {
		lo, hi = hi, lo
	}
	if fwd.cursorPos >= lo && fwd.cursorPos < hi {
		t.Errorf("selecting forwards left the cursor %d inside [%d,%d)", fwd.cursorPos, lo, hi)
	}
}
