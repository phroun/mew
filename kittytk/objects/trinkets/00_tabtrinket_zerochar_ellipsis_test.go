package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
)

// zeroCharTabStrip builds a strip in the state where the selected tab is the
// last visible one, has more tabs after it, and is clipped so hard that not
// one character of its label is drawn -- so the only thing it shows of itself
// is the ellipsis standing for its name.
//
// Inside a window, so the strip measures its "..." proportionally the way it
// does in a running host rather than falling back to three whole cells. The
// width and offset are ones a sweep found for that state.
func zeroCharTabStrip(t *testing.T, bottom bool) (*TabTrinket, *raster.Backend) {
	t.Helper()
	px, err := raster.New(700, 200)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(px)

	d := NewDesktop()
	d.SetBackend(px)
	d.SetBounds(core.UnitRect{Width: 700, Height: 200})
	win := window.NewWindow("w")
	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{Width: 700, Height: 200})

	tw := NewTabTrinket()
	win.AddChild(tw)
	for _, name := range []string{"Alphabet", "Nested", "Windows", "Vertical Tabs", "More", "Extra", "Final"} {
		tw.AddTab(name, NewLabel(name))
	}
	tw.SetCurrentIndex(1)
	if bottom {
		tw.SetTabPosition(TabsBottom)
	}
	tw.SetBounds(core.UnitRect{Width: 156, Height: 96})
	tw.tabScrollOffset = 0
	if !core.FindSmoothPositioning(tw.Self()) {
		t.Fatal("precondition: this strip should measure its ellipsis proportionally")
	}
	return tw, px
}

// A tab clipped so hard that none of its label fits shows an ellipsis in its
// own colours, and the silhouette closes around those dots.
//
// The shape was stretched over a tab's own dots only where the ellipsis had
// been pulled back over a separator. A tab that drew NO text took neither
// path, so its shape ended at the lead-in it had managed to draw and the dots
// stood beyond its closing edge: the tab opened, and then stopped.
//
// Read off the paint, as where the tab's material stops against where its
// outline closes. Those are the two things that disagreed.
func TestTabClippedToItsEllipsisIsClosedAroundIt(t *testing.T) {
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
			t.Errorf("%s: the tab's material stops at column %d with nothing closing it there; the shape ends somewhere else and its own ellipsis is outside it",
				name, tabRight)
		}
	}
}
