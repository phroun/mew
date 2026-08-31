package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// denominations covers the default plus a coarser and a finer one, which is
// the whole of what these two want to say.
var denominations = []core.CellMetrics{
	{CellWidth: 8, CellHeight: 16},
	{CellWidth: 16, CellHeight: 32},
	{CellWidth: 4, CellHeight: 8},
	{CellWidth: 32, CellHeight: 32},
}

// A click puts the caret between the characters it landed between, whatever
// denomination the field is counted in.
//
// The position arrives in the field's own denomination; the prefixes it is
// compared against were measured at the DEFAULT one. Inside a re-denominated
// window those are different sizes, so a click resolved against the wrong
// ruler and the caret landed several characters from the pointer.
func TestTextInputClickFindsTheSameCharacterAtEveryDenomination(t *testing.T) {
	const text = "hello world"

	for _, m := range denominations {
		ti := NewTextInput()
		ti.SetCellMetrics(&m)
		ti.SetText(text)
		ti.SetBounds(core.UnitRect{Width: core.ExchangeX(400, core.DefaultCellMetrics(), m), Height: m.CellHeight})

		font := ti.EffectiveFont()
		for _, want := range []int{0, 1, 5, 7, len(text)} {
			// The x that IS the boundary before character `want`, in this
			// field's currency: the width of everything ahead of it.
			x := ti.MeasureText(text[:want])
			if got := ti.findCharAtX(x, font); got != want {
				t.Errorf("at %dx%d: a click at x=%d (before %q) found index %d, want %d",
					m.CellWidth, m.CellHeight, x, text[:want], got, want)
			}
		}
	}
}

// A button is as wide as its caption plus its decoration, and that is one
// physical width -- the caption does not grow or shrink because the window
// around it is counted more finely.
//
// The decoration was already right: brackets, shadow and icon are stated in
// cells. The caption was measured at the default denomination.
func TestButtonIsTheSameWidthAtEveryDenomination(t *testing.T) {
	var want core.Unit
	for _, m := range denominations {
		b := NewButton("Cancel")
		b.SetCellMetrics(&m)

		// Read the width back in one shared currency, so the numbers are
		// comparable rather than merely differently denominated.
		got := core.ExchangeX(b.SizeHint().Width, m, core.DefaultCellMetrics())
		if want == 0 {
			want = got
			continue
		}
		if d := got - want; d > 1 || d < -1 {
			t.Errorf("at %dx%d the button wants %d units (at the default), want %d",
				m.CellWidth, m.CellHeight, got, want)
		}
	}
}
