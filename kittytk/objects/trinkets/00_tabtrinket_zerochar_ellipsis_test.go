package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// zeroCharTabStrip builds the strip with the selected tab squeezed until not
// one character of its label fits, so all it can show of itself is an
// ellipsis -- and with room for those dots inside it.
//
// The width is one the sweep found for that state; the sweep covered current
// tab, strip width and scroll offset.
func zeroCharTabStrip(t *testing.T, bottom bool) (*TabTrinket, *raster.Backend) {
	t.Helper()
	px, err := raster.New(700, 96)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(px)

	tw := NewTabTrinket()
	for _, name := range []string{"Alphabet", "Nested", "Windows", "Vertical Tabs", "More", "Extra", "Final"} {
		tw.AddTab(name, NewLabel(name))
	}
	tw.SetCurrentIndex(3)
	if bottom {
		tw.SetTabPosition(TabsBottom)
	}
	tw.SetBounds(core.UnitRect{Width: 112, Height: 96})
	tw.tabScrollOffset = 3
	return tw, px
}

// A tab squeezed until none of its label fits still gets a tab: its own
// ellipsis draws in its own colours, and the silhouette closes around them.
//
// The dots were placed and coloured as the tab's, but the shape was only ever
// stretched over them when the ellipsis had been pulled back over a separator.
// A tab that drew NO text took neither path, so its shape ended at the lead-in
// it had managed to draw and its own ellipsis sat beyond the closing edge: the
// tab opened, stopped, and the dots stood outside it.
//
// Read off the paint, as where the tab's material stops against where its
// outline closes. Those are the two things that disagreed.
func TestTabEllipsisOnlyTabIsClosedAroundItsDots(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for _, bottom := range []bool{false, true} {
		name := "top"
		if bottom {
			name = "bottom"
		}
		tw, px := zeroCharTabStrip(t, bottom)
		px.Clear(style.DefaultStyle().WithBg(style.RGB(0, 255, 0)))
		p := core.NewPainter(px)
		tw.Paint(p)

		rowH := tw.tabBarHeight()
		rowPx := p.UnitSpanPxY(0, rowH)
		top := 0
		if bottom {
			top = p.UnitSpanPxY(0, tw.Bounds().Height-rowH)
		}
		// The strip's edge line runs along the bar's CONTENT side: the bottom
		// of a top strip, the top of a bottom one.
		edgeY := top + rowPx - 1
		if bottom {
			edgeY = top
		}
		middleY := top + rowPx/2
		buttonsPx := p.UnitSpanPxX(0, tw.Bounds().Width-tw.scrollButtonWidth()*2)

		img := px.Image()
		// The bar's two colours, from the far left of the strip where there is
		// nothing but bar: its background and its edge line.
		barBg := img.RGBAAt(0, middleY)
		lineC := img.RGBAAt(0, edgeY)
		if barBg == lineC {
			t.Fatalf("%s: precondition -- the bar's background and its edge line are both %v", name, barBg)
		}

		// Where the tab's material stops: the last column before the scroll
		// buttons that is not simply bar.
		tabRight := -1
		for x := 0; x < buttonsPx; x++ {
			if img.RGBAAt(x, middleY) != barBg {
				tabRight = x
			}
		}
		if tabRight < 0 {
			t.Fatalf("%s: precondition -- nothing but bar across the whole strip", name)
		}

		// The outline closes there: a column of the line colour standing most
		// of the row's height, within a pixel of where the material stops.
		closes := false
		for x := tabRight; x <= tabRight+1 && !closes; x++ {
			n := 0
			for y := top; y < top+rowPx; y++ {
				if img.RGBAAt(x, y) == lineC {
					n++
				}
			}
			closes = n >= rowPx/2
		}
		if !closes {
			t.Errorf("%s: the tab's material stops at column %d with nothing closing it there; the shape ends somewhere else and the dots are outside it",
				name, tabRight)
		}
	}
}

// A tab with room for neither a character nor its own ellipsis is not shown at
// all: the strip stops before it and the strip's own "more tabs" ellipsis
// stands there instead, in the strip's colours.
//
// Drawing it anyway gave a stub with nothing in it -- a shape opening onto
// three dots that had to be squeezed in beside it, and at the widths where
// they would not fit beside it either, spilling back over the run that led
// into the tab.
func TestTabTooNarrowForItsEllipsisIsNotShown(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for _, bottom := range []bool{false, true} {
		name := "top"
		if bottom {
			name = "bottom"
		}
		px, err := raster.New(700, 96)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)

		tw := NewTabTrinket()
		for _, label := range []string{"Alphabet", "Nested", "Windows", "Vertical Tabs", "More", "Extra", "Final"} {
			tw.AddTab(label, NewLabel(label))
		}
		// The scroll offset at which the selected tab has room for neither: a
		// bottom strip's separators are a cell narrower, so it reaches that
		// state one tab further along than a top strip does.
		tw.SetCurrentIndex(0)
		offset := 0
		if bottom {
			tw.SetTabPosition(TabsBottom)
			tw.SetCurrentIndex(1)
			offset = 1
		}
		tw.SetBounds(core.UnitRect{Width: 100, Height: 96})
		tw.tabScrollOffset = offset

		px.Clear(style.DefaultStyle().WithBg(style.RGB(0, 255, 0)))
		p := core.NewPainter(px)
		tw.Paint(p)

		rowH := tw.tabBarHeight()
		rowPx := p.UnitSpanPxY(0, rowH)
		top := 0
		if bottom {
			top = p.UnitSpanPxY(0, tw.Bounds().Height-rowH)
		}
		edgeY := top + rowPx - 1
		if bottom {
			edgeY = top
		}
		buttonsPx := p.UnitSpanPxX(0, tw.Bounds().Width-tw.scrollButtonWidth()*2)

		img := px.Image()
		lineC := img.RGBAAt(0, edgeY)
		barBg := img.RGBAAt(0, top+rowPx/2)

		// No tab is shaped here, so the strip's edge line runs straight
		// across: a silhouette would carry it up and around the tab.
		broke := false
		for x := 0; x < buttonsPx && !broke; x++ {
			if got := img.RGBAAt(x, edgeY); got != lineC {
				t.Errorf("%s: the strip's edge line breaks at column %d (%v); a tab was shaped where none should be drawn",
					name, x, got)
				broke = true
			}
		}

		// Nor is any of the run that led into it left behind: the selected
		// tab's own colour appears nowhere in a strip that is not showing it.
		sel := tw.GetScheme().GetActiveTab().Bg
		r, g, b := sel.RGBComponents()
		left := false
		for x := 0; x < buttonsPx && !left; x++ {
			for y := top; y < top+rowPx; y++ {
				if c := img.RGBAAt(x, y); c.R == r && c.G == g && c.B == b {
					t.Errorf("%s: the selected tab's colour is painted at (%d,%d), left over from a tab the strip is not showing",
						name, x, y)
					left = true
					break
				}
			}
		}

		// And the dots that stand there are the strip's, on the strip's ground
		// -- read on the row just inside the strip's edge line, which is above
		// where the dots themselves ink.
		groundY := top
		if bottom {
			groundY = top + 1
		}
		dotsPx := p.UnitSpanPxX(0, tw.overflowEllipsisWidth())
		for x := buttonsPx - dotsPx; x < buttonsPx; x++ {
			if got := img.RGBAAt(x, groundY); got != barBg {
				t.Errorf("%s: column %d of the trailing ellipsis sits on %v, not the strip's own %v",
					name, x, got, barBg)
				break
			}
		}
	}
}
