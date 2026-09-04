package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
)

// truncatedTailStrip builds a strip whose last visible tab is clipped and has
// drawn its own ellipsis, inside a window so the strip measures its "..."
// proportionally the way a running host does.
func truncatedTailStrip(t *testing.T, bottom bool, cur int, w core.Unit, off int) (*TabTrinket, *raster.Backend) {
	t.Helper()
	px, err := raster.New(900, 200)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(px)

	d := NewDesktop()
	d.SetBackend(px)
	d.SetBounds(core.UnitRect{Width: 900, Height: 200})
	win := window.NewWindow("w")
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{Width: 900, Height: 200})

	tw := NewTabTrinket()
	win.AddChild(tw)
	for _, name := range []string{"Alphabet", "Nested", "Windows", "Vertical Tabs", "More", "Extra", "Final"} {
		tw.AddTab(name, NewLabel(name))
	}
	tw.SetCurrentIndex(cur)
	if bottom {
		tw.SetTabPosition(TabsBottom)
	}
	tw.SetBounds(core.UnitRect{Width: w, Height: 96})
	tw.tabScrollOffset = off
	return tw, px
}

// tailGeometry paints the strip and reports where the tab's material stops,
// where the scroll buttons begin, and the bar's own background.
//
// Read on the row of the strip the bar leaves plain: a top strip underlines
// its cells, so its top row is clean; a bottom strip overlines them, so its
// bottom row is. Either way the tab's own material covers the whole row
// height and the ellipsis drawn beside it inks only around the baseline, so
// what this finds is the tab and not the dots.
func tailGeometry(t *testing.T, tw *TabTrinket, px *raster.Backend, bottom bool) (tabRight, buttonsPx, rowPx, top int, barBg, cleanY int) {
	t.Helper()
	px.Clear(style.DefaultStyle().WithBg(style.RGB(0, 255, 0)))
	p := core.NewPainter(px)
	tw.Paint(p)

	rowH := tw.tabBarHeight()
	rowPx = p.UnitSpanPxY(0, rowH)
	top = 0
	if bottom {
		top = p.UnitSpanPxY(0, tw.Bounds().Height-rowH)
	}
	cleanY = top
	if bottom {
		cleanY = top + rowPx - 1
	}
	buttonsPx = p.UnitSpanPxX(0, tw.Bounds().Width-tw.scrollButtonWidth()*2)

	img := px.Image()
	bg := img.RGBAAt(0, cleanY)
	barBg = int(bg.R)<<16 | int(bg.G)<<8 | int(bg.B)
	tabRight = -1
	for x := 0; x < buttonsPx; x++ {
		c := img.RGBAAt(x, cleanY)
		if int(c.R)<<16|int(c.G)<<8|int(c.B) != barBg {
			tabRight = x
		}
	}
	return tabRight, buttonsPx, rowPx, top, barBg, cleanY
}

// A tab that has drawn its own ellipsis has said everything it can, so it
// closes there and the rest of the run belongs to the strip.
//
// It used to run its own colour on to the scroll buttons, which read as a tab
// stretching the width of the strip with its name trailing off at the left of
// it.
func TestTruncatedTabClosesAfterItsEllipsis(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for _, bottom := range []bool{false, true} {
		name := "top"
		if bottom {
			name = "bottom"
		}
		tw, px := truncatedTailStrip(t, bottom, 1, 140, 1)
		tabRight, buttonsPx, _, _, _, _ := tailGeometry(t, tw, px, bottom)
		if tabRight < 0 {
			t.Fatalf("%s: precondition -- nothing but bar across the whole strip", name)
		}

		p := core.NewPainter(px)
		ellipsisPx := p.UnitSpanPxX(0, tw.overflowEllipsisWidth())
		if gap := buttonsPx - 1 - tabRight; gap < ellipsisPx {
			t.Errorf("%s: the tab's material stops at column %d, %d px short of the scroll buttons at %d; it should end after its own ellipsis and leave the rest to the strip (an ellipsis is %d px)",
				name, tabRight, gap, buttonsPx, ellipsisPx)
		}
	}
}

// The strip's own "more tabs" ellipsis goes in that space when the whole of it
// fits, and is left out when it does not: the tab's own dots at the end of the
// run already say there is more than this.
func TestTruncatedTabTailCarriesTheStripsEllipsisOnlyWhenItFits(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for _, bottom := range []bool{false, true} {
		name := "top"
		if bottom {
			name = "bottom"
		}

		// Room for it: the dots stand in the slot against the scroll buttons.
		tw, px := truncatedTailStrip(t, bottom, 1, 140, 1)
		tabRight, buttonsPx, rowPx, top, barBg, _ := tailGeometry(t, tw, px, bottom)
		p := core.NewPainter(px)
		ellipsisPx := p.UnitSpanPxX(0, tw.overflowEllipsisWidth())
		if buttonsPx-1-tabRight < ellipsisPx {
			t.Fatalf("%s: precondition -- no room for the strip's ellipsis after the tab", name)
		}
		img := px.Image()
		inked := 0
		for x := buttonsPx - ellipsisPx; x < buttonsPx; x++ {
			for y := top + 1; y < top+rowPx-2; y++ {
				c := img.RGBAAt(x, y)
				if int(c.R)<<16|int(c.G)<<8|int(c.B) != barBg {
					inked++
					break
				}
			}
		}
		if inked == 0 {
			t.Errorf("%s: nothing is drawn in the %d px against the scroll buttons; the strip's own ellipsis should be there",
				name, ellipsisPx)
		}

		// No room for it: nothing is drawn in the sliver that is left.
		tw, px = truncatedTailStrip(t, bottom, 0, 110, 0)
		tabRight, buttonsPx, rowPx, top, barBg, _ = tailGeometry(t, tw, px, bottom)
		p = core.NewPainter(px)
		ellipsisPx = p.UnitSpanPxX(0, tw.overflowEllipsisWidth())
		gap := buttonsPx - 1 - tabRight
		if gap <= 0 || gap >= ellipsisPx {
			t.Fatalf("%s: precondition -- the tail is %d px, wanted a sliver narrower than the %d px an ellipsis needs",
				name, gap, ellipsisPx)
		}
		img = px.Image()
		for x := tabRight + 1; x < buttonsPx; x++ {
			for y := top + 1; y < top+rowPx-2; y++ {
				c := img.RGBAAt(x, y)
				if int(c.R)<<16|int(c.G)<<8|int(c.B) != barBg {
					t.Fatalf("%s: something is drawn at column %d in a tail too narrow (%d px) for the strip's ellipsis (%d px)",
						name, x, gap, ellipsisPx)
				}
			}
		}
	}
}
