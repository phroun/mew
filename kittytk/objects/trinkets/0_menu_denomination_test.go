package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// popupInk returns the device-pixel columns carrying item ink: pixels that
// differ from the popup's own background fill, sampled inside it.
//
// The popup's fill would otherwise swallow the comparison, the way the menu
// bar's did -- every column it covers differs from the surface background,
// so two renders come out identical whatever the items do.
func popupInk(b *raster.Backend, w, h, sampleX, sampleY int) []int {
	img := b.Image()
	popupBG := img.RGBAAt(sampleX, sampleY)
	var cols []int
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			if img.RGBAAt(x, y) != popupBG {
				cols = append(cols, x)
				break
			}
		}
	}
	return cols
}

// A dropdown, a submenu and a Menu-based context menu are all one type, so
// one rule covers them: a menu states no width, no padding and no margin of
// its own, so changing the denomination of the window it belongs to must
// leave its picture alone.
//
// Its decorative geometry -- the 3-cell gutter, the 5 cells of padding, the
// 3-cell shortcut gap and arrow -- was already stated in cells and needed
// nothing. Its item text and shortcuts were measured at the DEFAULT
// denomination, so they were counted in units of the wrong size and the
// popup came out too narrow or too wide around them.
func TestMenuPaintsTheSameAtEveryDenomination(t *testing.T) {
	const W, H = 320, 120
	outer := core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}

	var base []int
	var baseW, baseH core.Unit
	for _, interior := range []core.CellMetrics{
		{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16},
		{UnitsPerCellWidth: 16, UnitsPerCellHeight: 32},
		{UnitsPerCellWidth: 4, UnitsPerCellHeight: 8},
	} {
		b, err := raster.New(W, H)
		if err != nil {
			t.Fatal(err)
		}
		b.SetCellMetrics(outer)
		b.Clear(style.DefaultStyle())

		m := NewMenu("File")
		m.AddItem(NewMenuItem("New").SetShortcutText("^N"))
		m.AddItem(NewMenuItem("Open Recent"))
		m.AddItem(NewSeparator())
		// A long shortcut on a short label, so the shortcut's own
		// measurement is what decides the popup's width.
		m.AddItem(NewMenuItem("Go").SetShortcutText("Ctrl+Shift+G"))
		m.SetCellMetrics(&interior)

		// The popup opens at its own origin, in its own currency.
		m.Show(0, 0)
		p := core.NewPainter(b).WithDenomination(outer, interior)
		m.Paint(p)

		// The popup's width, stated in its own units, is the same physical
		// width at every denomination -- that is the claim, in the currency
		// the caller would read it in.
		gotW := core.ExchangeX(m.calculateSize().Width, interior, outer)
		if baseW == 0 {
			baseW = gotW
		} else if d := gotW - baseW; d > 1 || d < -1 {
			t.Errorf("interior %dx%d: popup is %d outer units wide, want %d",
				interior.UnitsPerCellWidth, interior.UnitsPerCellHeight, gotW, baseW)
		}

		// Height too: it carries the separator band, which is a fraction of
		// a cell rather than a raw unit count for the same reason.
		gotH := core.ExchangeY(m.calculateSize().Height, interior, outer)
		if baseH == 0 {
			baseH = gotH
		} else if d := gotH - baseH; d > 1 || d < -1 {
			// One unit of slack: a fraction of a cell is not always a whole
			// number of units. The separator band is 3/16 of a cell, which
			// at an 8-unit cell is 1.5 -- unrepresentable either way. The
			// faults this guards against are tens of units, not one.
			t.Errorf("interior %dx%d: popup is %d outer units tall, want %d",
				interior.UnitsPerCellWidth, interior.UnitsPerCellHeight, gotH, baseH)
		}

		got := popupInk(b, W, H, 4, 4)
		if base == nil {
			base = got
			continue
		}
		if len(got) != len(base) {
			t.Errorf("interior %dx%d painted %d ink columns, want %d (the 8x16 picture)",
				interior.UnitsPerCellWidth, interior.UnitsPerCellHeight, len(got), len(base))
			continue
		}
		for i := range base {
			if got[i] != base[i] {
				t.Errorf("interior %dx%d: ink column %d at px %d, want %d",
					interior.UnitsPerCellWidth, interior.UnitsPerCellHeight, i, got[i], base[i])
				break
			}
		}
	}
}
