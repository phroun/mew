package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// rightAlignedBar builds a bar whose last menu sits far enough right that its
// dropdown cannot open under it and right-aligns instead: the dropdown's right
// edge is placed on the item's right edge.
func rightAlignedBar(t *testing.T, scale int) (*MenuBar, *raster.Backend) {
	t.Helper()
	b, err := raster.NewScaled(400*scale, 300*scale, scale)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)

	d := NewDesktop()
	d.SetBackend(b)
	d.SetBounds(core.UnitRect{Width: 400, Height: 300})
	bar := NewMenuBar()
	// No clock, so the titles have the whole bar and none of them elides.
	bar.SetHideCalendar(true)
	d.AddChild(bar)
	for _, title := range []string{"File", "Edit", "View", "Insert", "Format", "Tools"} {
		bar.AddMenu(NewMenu(title))
	}
	last := NewMenu("Window")
	// Wide enough that opening under its own item would run off the surface,
	// and narrow enough that right-aligning it still starts past the left edge.
	last.AddItem(NewMenuItem("Bring absolutely everything to the front"))
	last.AddItem(NewMenuItem("Tile horizontally"))
	bar.AddMenu(last)
	bar.SetBounds(core.UnitRect{Width: 400, Height: bar.menuMetrics().RowH})
	return bar, b
}

// A dropdown that opens to the LEFT of its item -- right-aligned, because
// opening under the item would run off the surface -- puts its right edge on
// the item's right edge. The two lines drawn there must be one line.
//
// The item's frame draws its right line ON the item's last pixel column, so a
// dropdown that drew its own just outside the menu came up a pixel to the
// right of the item it hangs from, at every scale.
func TestRightAlignedDropdownEdgeMeetsItsItem(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for _, scale := range []int{1, 2, 3} {
		bar, b := rightAlignedBar(t, scale)
		last := len(bar.menus) - 1
		bar.OpenMenu(last)

		itemX := bar.calculateMenuX(last)
		itemW := bar.menuTitleWidth(bar.menus[last].title)
		popup := bar.activeMenu
		if popup.popupX+popup.calculateSize().Width != itemX+itemW {
			t.Fatalf("scale %d: precondition -- the dropdown at %d+%d is not right-aligned to the item at %d+%d",
				scale, popup.popupX, popup.calculateSize().Width, itemX, itemW)
		}
		if popup.popupX >= itemX {
			t.Fatalf("scale %d: precondition -- the dropdown should have opened to the LEFT of its item (popup %d, item %d)",
				scale, popup.popupX, itemX)
		}

		b.Clear(style.DefaultStyle().WithBg(style.RGB(0, 255, 0)))
		p := core.NewPainter(b)
		bar.Paint(p)
		bar.PaintDropdown(p)

		mm := bar.menuMetrics()
		img := b.Image()
		rowPx := p.UnitSpanPxY(0, mm.RowH)

		// Each line is found the same way: from a pixel inside the box's own
		// fill, walk right to the first column that is not that fill. Reading
		// the paint, since the widths are what put the two a pixel apart.
		lineRightOf := func(x0, y int) int {
			fill := img.RGBAAt(x0, y)
			for x := x0; x < x0+p.UnitSpanPxX(0, 400); x++ {
				c := img.RGBAAt(x, y)
				if abs8(c.R, fill.R)+abs8(c.G, fill.G)+abs8(c.B, fill.B) > 24 {
					return x
				}
			}
			return -1
		}

		// The item, read along the top of the bar's row, clear of its title.
		itemLine := lineRightOf(p.UnitSpanPxX(0, itemX)+1, 0)
		// The dropdown, read along the top of its first row, past the gutter
		// and above where any label inks.
		menuLine := lineRightOf(p.UnitSpanPxX(0, popup.popupX+mm.GutterWidth())+2, rowPx+1)
		if itemLine < 0 || menuLine < 0 {
			t.Fatalf("scale %d: no line found (item %d, dropdown %d)", scale, itemLine, menuLine)
		}

		if itemLine != menuLine {
			t.Errorf("scale %d: the item's right line is at pixel column %d and its right-aligned dropdown's at %d; they should be one line",
				scale, itemLine, menuLine)
		}
	}
}
