package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// denominations covers the default plus a coarser and a finer one, which is
// the whole of what these two want to say.
var denominations = []core.CellMetrics{
	{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16},
	{UnitsPerCellWidth: 16, UnitsPerCellHeight: 32},
	{UnitsPerCellWidth: 4, UnitsPerCellHeight: 8},
	{UnitsPerCellWidth: 32, UnitsPerCellHeight: 32},
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
		ti.SetBounds(core.UnitRect{Width: core.ExchangeX(400, core.DefaultCellMetrics(), m), Height: m.UnitsPerCellHeight})

		font := ti.EffectiveFont()
		for _, want := range []int{0, 1, 5, 7, len(text)} {
			// The x that IS the boundary before character `want`, in this
			// field's currency: the width of everything ahead of it.
			x := ti.MeasureText(text[:want])
			if got := ti.findCharAtX(x, font); got != want {
				t.Errorf("at %dx%d: a click at x=%d (before %q) found index %d, want %d",
					m.UnitsPerCellWidth, m.UnitsPerCellHeight, x, text[:want], got, want)
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
				m.UnitsPerCellWidth, m.UnitsPerCellHeight, got, want)
		}
	}
}

// A button's drop shadow sits half a column across and a quarter of a row
// down. At the default 8x16 cell both are four units, the same physical
// distance on each axis, which is what makes the offset square -- and it has
// to stay square whatever the cell's shape.
//
// Taken from the column width alone, as it was, the vertical offset was a
// quarter of a row only while a cell was twice as tall as it is wide. On a
// square cell it threw the shadow twice as far down as across.
//
// Read off the paint rather than recomputed: a test that restates the
// formula agrees with any formula, including the wrong one.
func TestButtonShadowIsSquareAtEveryDenomination(t *testing.T) {
	const W, H = 160, 80
	outer := core.DefaultCellMetrics()

	var baseRight, baseBottom int
	for _, interior := range denominations {
		b, err := raster.New(W, H)
		if err != nil {
			t.Fatal(err)
		}
		b.SetCellMetrics(outer)
		b.Clear(style.DefaultStyle())

		d := NewDesktop()
		d.SetBackend(b)
		btn := NewButton("OK")
		btn.SetParent(d)
		btn.SetCellMetrics(&interior)
		btn.SetBounds(core.UnitRect{
			Width:  core.ExchangeX(80, outer, interior),
			Height: core.ExchangeY(40, outer, interior),
		})
		btn.Paint(core.NewPainter(b).WithDenomination(outer, interior))

		// The shadow is the furthest ink from the origin on each axis: the
		// face paints over it, so what remains beyond the face IS the offset.
		right, bottom := -1, -1
		img := b.Image()
		bg := img.RGBAAt(W-1, H-1)
		for x := 0; x < W; x++ {
			for y := 0; y < H; y++ {
				if img.RGBAAt(x, y) != bg {
					if x > right {
						right = x
					}
					if y > bottom {
						bottom = y
					}
				}
			}
		}
		if right < 0 || bottom < 0 {
			t.Fatalf("interior %dx%d painted nothing", interior.UnitsPerCellWidth, interior.UnitsPerCellHeight)
		}
		if baseRight == 0 {
			baseRight, baseBottom = right, bottom
			continue
		}
		// A pixel of slack each way: half a column of a 4-unit cell is 2
		// units, and these fractions do not always land whole. The fault
		// this guards against is a doubled offset, not a pixel.
		if dx, dy := right-baseRight, bottom-baseBottom; dx > 1 || dx < -1 || dy > 1 || dy < -1 {
			t.Errorf("interior %dx%d: button ink reaches (%d,%d) px, want (%d,%d)",
				interior.UnitsPerCellWidth, interior.UnitsPerCellHeight, right, bottom, baseRight, baseBottom)
		}
	}
}
