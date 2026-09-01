package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// blockFillRecorder keeps the device-pixel fills a paint made, so a test can
// find the block caret among them. Reading the image instead cannot: a focused
// field paints its speckle across every column, so every column carries ink and
// the block's own edges are invisible in it.
type blockFillRecorder struct {
	*raster.Backend
	widths []int
}

func (b *blockFillRecorder) FillRectPx(x, y, w, h int, st style.CellStyle) {
	b.widths = append(b.widths, w)
	b.Backend.FillRectPx(x, y, w, h, st)
}

// The block caret past the end of the text is one space wide on the glass,
// whatever the interior is denominated in.
//
// It is sized from a measured width, and a measured width is in the trinket's
// OWN units: a space measures 3 units at 8x16, 6 at 16x32 and 2 at 4x8, all of
// them the same 3 device pixels. Converting one with UnitsToPx converts from
// the DEFAULT denomination, so it painted 6px and 2px for the same space --
// while the text beside it, laid out in pixels, stayed put.
func TestEndOfTextBlockCaretIsTheSameWidthAtEveryDenomination(t *testing.T) {
	outer := core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}

	want := -1
	for _, interior := range []core.CellMetrics{
		{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16},
		{UnitsPerCellWidth: 16, UnitsPerCellHeight: 32},
		{UnitsPerCellWidth: 4, UnitsPerCellHeight: 8},
	} {
		base, err := raster.New(400, 40)
		if err != nil {
			t.Fatal(err)
		}
		base.SetCellMetrics(outer)
		core.SetTextMeasurer(base)
		base.Clear(style.DefaultStyle())
		rec := &blockFillRecorder{Backend: base}

		ti := NewTextInput()
		m := interior
		ti.SetCellMetrics(&m)
		ti.SetReadOnly(true) // a read-only field paints the steady block
		ti.SetText("Hi")
		ti.SetCursorPosition(2) // past the end: the block takes a space's worth
		ti.SetFocus()
		ti.caretOn = true
		ti.SetBounds(core.UnitRect{
			Width:  30 * interior.UnitsPerCellWidth,
			Height: interior.UnitsPerCellHeight,
		})
		ti.Paint(core.NewPainter(rec).WithDenomination(outer, interior))
		core.SetTextMeasurer(nil)

		// The block is the one narrow fill: the field's own ground covers the
		// whole width, and nothing else this field paints is a few pixels wide.
		got := -1
		for _, w := range rec.widths {
			if w > 0 && w < 20 {
				got = w
			}
		}
		if got < 0 {
			t.Fatalf("at %dx%d no block fill was painted; the test reads nothing",
				interior.UnitsPerCellWidth, interior.UnitsPerCellHeight)
		}
		if want < 0 {
			want = got
			continue
		}
		if got != want {
			t.Errorf("at %dx%d the block caret is %dpx wide, want %d",
				interior.UnitsPerCellWidth, interior.UnitsPerCellHeight, got, want)
		}
	}
}
