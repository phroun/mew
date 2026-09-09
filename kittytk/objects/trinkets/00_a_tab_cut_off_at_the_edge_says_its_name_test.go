package trinkets

// A strip too narrow for its tabs cuts the run short. The tab it stops on is
// drawn as much of as there was room for, so the one thing a reader cannot do
// is read its name.

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// crowdedTabs is a strip with more tabs than it has room for, painted so the
// run knows where it gave out.
func crowdedTabs(t *testing.T, width core.Unit) *TabTrinket {
	t.Helper()
	px, err := raster.New(900, 200)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	tabs := NewTabTrinket()
	for _, name := range []string{
		"Connections", "Authorizations", "Certificates", "Preferences",
	} {
		tabs.AddTab(name, NewPanel())
	}
	tabs.SetBounds(core.UnitRect{Width: width, Height: 120})
	tabs.Paint(core.NewPainter(px))
	return tabs
}

// The tab the run was cut short at offers its name; the ones standing whole
// say their own already.
func TestATabCutOffAtTheEdgeOffersItsName(t *testing.T) {
	tabs := crowdedTabs(t, 200)

	var clipped stripSpan
	var whole stripSpan
	for _, sp := range tabs.stripSpans {
		if sp.owner < 0 {
			continue
		}
		if sp.clipped {
			clipped = sp
		} else if whole.w == 0 {
			whole = sp
		}
	}
	if clipped.w == 0 {
		t.Fatal("no tab was cut short, so nothing is being tested")
	}

	band := tabs.stripHoverBounds()
	mid := core.UnitPoint{X: clipped.x + clipped.w/2, Y: band.Y + band.Height/2}
	text, at, ok := tabs.TooltipAt(mid)
	if !ok {
		t.Fatal("the tab the run stopped on offered nothing")
	}
	if text != tabs.tabs[clipped.owner].Text {
		t.Errorf("it offered %q, not the name of the tab it stopped on", text)
	}
	if at != band {
		t.Errorf("it named %+v rather than the strip at %+v", at, band)
	}

	// A tab drawn in full is not missing anything.
	if whole.w > 0 {
		at := core.UnitPoint{X: whole.x + whole.w/2, Y: mid.Y}
		if _, _, ok := tabs.TooltipAt(at); ok {
			t.Error("a tab standing whole offered its name anyway")
		}
	}

	// And nothing outside the strip is the strip's to answer for.
	if _, _, ok := tabs.TooltipAt(core.UnitPoint{X: mid.X, Y: band.Y + band.Height + 8}); ok {
		t.Error("the strip answered for the content below it")
	}
}

// The pointer arriving on such a tab makes the offer up the chain.
func TestHoveringACutOffTabMakesTheOffer(t *testing.T) {
	c := newCatcher()
	tabs := crowdedTabs(t, 200)
	tabs.SetParent(c)

	var clipped stripSpan
	for _, sp := range tabs.stripSpans {
		if sp.owner >= 0 && sp.clipped {
			clipped = sp
		}
	}
	if clipped.w == 0 {
		t.Fatal("no tab was cut short, so nothing is being tested")
	}
	band := tabs.stripHoverBounds()
	tabs.HandleMouseMove(core.MouseMoveEvent{
		X: clipped.x + clipped.w/2,
		Y: band.Y + band.Height/2,
	})

	if len(c.took) != 1 {
		t.Fatalf("the pointer on a cut-off tab produced %d offers", len(c.took))
	}
	if c.took[0].Text != tabs.tabs[clipped.owner].Text {
		t.Errorf("it offered %q", c.took[0].Text)
	}
}
