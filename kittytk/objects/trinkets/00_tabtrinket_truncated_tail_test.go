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

// A tab has no strip showing through its middle.
//
// The run between a tab and its own dots is filled before the dots are placed,
// when whose ground it will turn out to be is not yet known, so it went down in
// the strip's colour. Where the dots then turned out to be the tab's, the
// silhouette closed around the lot and that run became a notch of bar cut out
// of the tab's interior.
func TestTabInteriorHasNoStripShowingThrough(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for _, bottom := range []bool{false, true} {
		name := "top"
		if bottom {
			name = "bottom"
		}
		tw, px := truncatedTailStrip(t, bottom, 2, 214, 0)
		px.Clear(style.DefaultStyle().WithBg(style.RGB(0, 255, 0)))
		p := core.NewPainter(px)
		tw.Paint(p)

		rowH := tw.tabBarHeight()
		rowPx := p.UnitSpanPxY(0, rowH)
		top := 0
		if bottom {
			top = p.UnitSpanPxY(0, tw.Bounds().Height-rowH)
		}
		midY := top + rowPx/2
		buttonsPx := p.UnitSpanPxX(0, tw.Bounds().Width-tw.scrollButtonWidth()*2)

		img := px.Image()
		// The selected tab's own ground, and the strip's. A focused strip
		// paints its selected tab in the focused colour.
		tabBg := tw.GetScheme().GetActiveTab().Bg
		if tw.HasFocus() {
			tabBg = tw.GetScheme().GetFocusedTab().Bg
		}
		fr, fg, fb := tabBg.RGBComponents()
		barBg := img.RGBAAt(0, top)

		// The tab's extent, from the first to the last column carrying its
		// ground. Its label and its dots break the run, but they are ink, not
		// strip.
		first, last := -1, -1
		for x := 0; x < buttonsPx; x++ {
			if c := img.RGBAAt(x, midY); c.R == fr && c.G == fg && c.B == fb {
				if first < 0 {
					first = x
				}
				last = x
			}
		}
		if first < 0 || last-first < 8 {
			t.Fatalf("%s: precondition -- no tab of any width found at mid-height (first %d, last %d)",
				name, first, last)
		}

		for x := first; x <= last; x++ {
			if img.RGBAAt(x, midY) == barBg {
				t.Fatalf("%s: the strip's own colour shows at column %d, inside a tab running from %d to %d",
					name, x, first, last)
			}
		}

		// And the dots the tab is showing instead of its name are still
		// there: the fill that closes the notch stops at them rather than
		// running on over them.
		tabFg := tw.GetScheme().GetActiveTab().Fg
		if tw.HasFocus() {
			tabFg = tw.GetScheme().GetFocusedTab().Fg
		}
		ir, ig, ib := tabFg.RGBComponents()
		ink := 0
		for x := first; x <= last; x++ {
			for y := top + 1; y < top+rowPx-1; y++ {
				if c := img.RGBAAt(x, y); c.R == ir && c.G == ig && c.B == ib {
					ink++
					break
				}
			}
		}
		if ink == 0 {
			t.Errorf("%s: the tab from %d to %d carries none of its own ink; its dots were painted over",
				name, first, last)
		}
	}
}

// ellipsisCounter remembers where every run was drawn, so a test can tell the
// ellipsis at the LEFT of the strip -- which says there are tabs before these
// -- from any drawn further along.
type ellipsisCounter struct {
	*raster.Backend
	runs []string
	xs   []core.Unit
}

func (r *ellipsisCounter) DrawText(x, y core.Unit, text string, s style.CellStyle, font *core.Font) core.Unit {
	r.runs = append(r.runs, text)
	r.xs = append(r.xs, x)
	return r.Backend.DrawText(x, y, text, s, font)
}

// trailing counts the ellipses drawn anywhere but the very left of the strip.
func (r *ellipsisCounter) trailing() int {
	n := 0
	for i, s := range r.runs {
		if s == "..." && r.xs[i] > 0 {
			n++
		}
	}
	return n
}

// ONE mark at the right-hand end of the run, never two.
//
// A clipped tab says its name is cut with its own dots; the strip says there
// are more tabs with dots of its own against the scroll buttons. Both at once
// is the same end of the same line saying it twice -- and it happened, because
// the strip decided whether to add its own by asking only whether it fitted.
//
// Stated over a sweep rather than at one width, since the widths where the two
// coincided were never the ones anybody thought to look at.
func TestNeverTwoEllipsesAtTheEndOfAStrip(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	px, err := raster.New(900, 200)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(px)
	names := []string{"Alphabet", "Nested", "Vertical Tabs", "Details", "MDI Demo", "Extra", "Final"}

	painted, sawOne := 0, 0
	for _, bottom := range []bool{false, true} {
		for cur := 0; cur < len(names); cur++ {
			for w := core.Unit(90); w <= 420; w += 6 {
				for off := 0; off < len(names); off++ {
					rec := &ellipsisCounter{Backend: px}

					d := NewDesktop()
					d.SetBackend(px)
					d.SetBounds(core.UnitRect{Width: 900, Height: 200})
					win := window.NewWindow("w")
					d.WindowManager().AddWindow(win)
					win.SetBounds(core.UnitRect{Width: 900, Height: 200})

					tw := NewTabTrinket()
					win.AddChild(tw)
					for _, n := range names {
						tw.AddTab(n, NewLabel(n))
					}
					tw.SetCurrentIndex(cur)
					if bottom {
						tw.SetTabPosition(TabsBottom)
					}
					tw.SetBounds(core.UnitRect{Width: w, Height: 96})
					tw.tabScrollOffset = off

					px.Clear(style.DefaultStyle())
					tw.Paint(core.NewPainter(rec))
					painted++

					switch n := rec.trailing(); {
					case n > 1:
						t.Fatalf("bottom=%v cur=%d w=%d off=%d: %d ellipses at the end of the run: %v",
							bottom, cur, w, off, n, rec.runs)
					case n == 1:
						sawOne++
					}
				}
			}
		}
	}
	if painted == 0 || sawOne == 0 {
		t.Fatalf("precondition: %d strips painted, %d of them ending in an ellipsis at all", painted, sawOne)
	}
}
