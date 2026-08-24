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
