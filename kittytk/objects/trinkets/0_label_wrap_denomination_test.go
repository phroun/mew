package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// The caption in the demo's bordered panel, which is where this showed up.
const wrapCaption = "The quick brown fox jumps over the lazy dog and then keeps trotting along the whole fence"

// boxUnits is the panel's width at the default denomination -- the demo's
// fixed_width=256. Each render below gets the same physical width, stated in
// whatever denomination that render counts in.
const boxUnits core.Unit = 256

var wrapOuter = core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}

var wrapDenominations = []core.CellMetrics{
	{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16},
	{UnitsPerCellWidth: 16, UnitsPerCellHeight: 32},
	{UnitsPerCellWidth: 32, UnitsPerCellHeight: 64},
	{UnitsPerCellWidth: 4, UnitsPerCellHeight: 8},
}

func newWrappedLabel(interior core.CellMetrics) (*Label, core.Unit) {
	l := NewLabel(wrapCaption)
	l.SetWordWrap(true)
	l.SetCellMetrics(&interior)
	return l, core.ExchangeX(boxUnits, wrapOuter, interior)
}

// Word wrap compares a width against measured text. The width arrives in the
// label's own units, from whatever box holds it; the measurement went through
// Font.MeasureText, which answers in default-denomination units. At an
// interior denomination of 16 the label was handed twice the number for the
// same physical box and measured the caption at half rate, so it packed four
// times as much onto a line and the text ran past the border.
//
// A line count does not depend on the denomination, so the same caption in the
// same physical width has to come to the same number of lines whatever the
// label counts in.
func TestWrappedLabelBreaksTheSameAtEveryDenomination(t *testing.T) {
	base := -1
	for _, interior := range wrapDenominations {
		l, width := newWrappedLabel(interior)
		lines := int(l.HeightForWidth(width) / interior.UnitsPerCellHeight)

		if base == -1 {
			base = lines
			if base < 3 {
				t.Fatalf("the caption came to %d lines at 8x16; the box is too wide to wrap", base)
			}
			continue
		}
		if lines != base {
			t.Errorf("interior %dx%d wrapped the caption to %d lines, want %d (what 8x16 gives)",
				interior.UnitsPerCellWidth, interior.UnitsPerCellHeight, lines, base)
		}
	}
}

// The width a label asks for is the width of its longest line, so it is the
// same physical width whatever the label counts in.
func TestLabelSizeHintWidthIsTheSameAtEveryDenomination(t *testing.T) {
	base := core.Unit(0)
	for _, interior := range wrapDenominations {
		l := NewLabel(wrapCaption)
		l.SetCellMetrics(&interior)
		got := core.ExchangeX(l.SizeHint().Width, interior, wrapOuter)

		if base == 0 {
			base = got
			continue
		}
		if got != base {
			t.Errorf("interior %dx%d asks for %d units at 8x16, want %d",
				interior.UnitsPerCellWidth, interior.UnitsPerCellHeight, got, base)
		}
	}
}

// What the eye catches: a wrapped line running past the right edge of the box
// it was given. Painted glyphs are read off the surface rather than counted,
// because it is the ink that overflows.
func TestWrappedLabelStaysInsideItsBoxAtEveryDenomination(t *testing.T) {
	const surfaceW, surfaceH = 480, 200
	boxPx := int(boxUnits) // one device pixel per unit at 8x16

	for _, interior := range wrapDenominations {
		b, err := raster.New(surfaceW, surfaceH)
		if err != nil {
			t.Fatal(err)
		}
		b.SetCellMetrics(wrapOuter)
		b.Clear(style.DefaultStyle())
		empty := b.Image().RGBAAt(surfaceW-1, surfaceH-1)

		l, width := newWrappedLabel(interior)
		l.SetBounds(core.UnitRect{Width: width, Height: l.HeightForWidth(width)})
		l.Paint(core.NewPainter(b).WithDenomination(wrapOuter, interior))

		img := b.Image()
		overflow := 0
		firstX := -1
		for x := boxPx; x < surfaceW; x++ {
			for y := 0; y < surfaceH; y++ {
				if img.RGBAAt(x, y) != empty {
					overflow++
					if firstX < 0 {
						firstX = x
					}
					break
				}
			}
		}
		if overflow > 0 {
			t.Errorf("interior %dx%d painted %d columns past the box's %dpx right edge, from px %d",
				interior.UnitsPerCellWidth, interior.UnitsPerCellHeight, overflow, boxPx, firstX)
		}
	}
}
