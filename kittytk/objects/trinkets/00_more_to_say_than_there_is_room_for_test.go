package trinkets

// Offering what a trinket could not show, and what happens to the offer on the
// way up.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
)

// catcher is a container that takes tooltips, standing in for a window that
// routes them somewhere of its own.
type catcher struct {
	Panel
	took    []core.TooltipRequest
	hid     []core.Trinket
	refuses bool
}

func newCatcher() *catcher {
	c := &catcher{}
	c.Panel = *NewPanel()
	c.Init(c)
	c.SetLayoutManager(layout.NewVBoxLayout())
	return c
}

func (c *catcher) ShowTooltip(req core.TooltipRequest) bool {
	if c.refuses {
		return false
	}
	c.took = append(c.took, req)
	return true
}

func (c *catcher) HideTooltip(from core.Trinket) { c.hid = append(c.hid, from) }

// hovering builds a label too narrow for its text inside a catcher, paints it
// so the cut is known, and returns both.
func hovering(t *testing.T, text string, width core.Unit) (*catcher, *Label) {
	t.Helper()
	px, err := raster.New(900, 96)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	// The catcher is the PARENT of the panel holding the label, not the panel
	// itself: a container's own AddChild parents a child to the embedded panel,
	// and the walk would then never see the type that handles tooltips.
	c := newCatcher()
	inner := NewPanel()
	inner.SetLayoutManager(layout.NewVBoxLayout())
	l := NewLabel(text)
	inner.AddChild(l)
	inner.SetParent(c)
	inner.SetBounds(core.UnitRect{Width: width, Height: 48})
	inner.Paint(core.NewPainter(px))
	return c, l
}

// A trinket that had to cut its text offers the whole of it when the pointer
// arrives -- at once, since what is being offered is the rest of a word the
// reader is already looking at.
func TestWhatWasCutIsOfferedOnHover(t *testing.T) {
	c, l := hovering(t, longCaption, 120)
	l.HandleMouseMove(core.MouseMoveEvent{X: 10, Y: 4})

	if len(c.took) != 1 {
		t.Fatalf("the pointer arriving produced %d offers", len(c.took))
	}
	if c.took[0].Text != longCaption {
		t.Errorf("it offered %q, not the whole of what it could not show", c.took[0].Text)
	}
	if c.took[0].From != core.Trinket(l) {
		t.Error("the offer did not name the trinket it came from")
	}
}

// And withdraws it when the pointer leaves. A container forwards every move to
// every child, so a point outside is how a trinket hears the pointer go.
func TestTheOfferIsWithdrawnWhenThePointerLeaves(t *testing.T) {
	c, l := hovering(t, longCaption, 120)
	l.HandleMouseMove(core.MouseMoveEvent{X: 10, Y: 4})
	l.HandleMouseMove(core.MouseMoveEvent{X: -40, Y: 4})

	if len(c.hid) == 0 {
		t.Fatal("the pointer left and the offer stood")
	}
	if c.hid[len(c.hid)-1] != core.Trinket(l) {
		t.Error("something else was withdrawn")
	}
}

// Text that fits has nothing to add, so nothing is offered.
func TestNothingIsOfferedWhenItAllFits(t *testing.T) {
	c, l := hovering(t, "short", 400)
	l.HandleMouseMove(core.MouseMoveEvent{X: 10, Y: 4})
	if len(c.took) != 0 {
		t.Errorf("a label showing all of its text offered %q", c.took[0].Text)
	}
}

// A trinket told what to say says it whether or not anything was cut.
func TestATrinketCanBeToldWhatToSay(t *testing.T) {
	c, l := hovering(t, "short", 400)
	l.SetTooltip("the whole story")
	l.HandleMouseMove(core.MouseMoveEvent{X: 10, Y: 4})

	if len(c.took) != 1 || c.took[0].Text != "the whole story" {
		t.Fatalf("it offered %+v", c.took)
	}
}

// The first handler up the chain that takes it ends the walk: a window routing
// tooltips its own way is not second-guessed by what is behind it.
func TestTheFirstHandlerThatTakesItEndsTheWalk(t *testing.T) {
	inner, l := hovering(t, longCaption, 120)
	outer := newCatcher()
	inner.SetParent(outer)
	outer.SetBounds(core.UnitRect{Width: 400, Height: 96})

	l.HandleMouseMove(core.MouseMoveEvent{X: 10, Y: 4})
	if len(inner.took) != 1 {
		t.Fatalf("the nearer handler took %d", len(inner.took))
	}
	if len(outer.took) != 0 {
		t.Errorf("the further handler was offered it as well: %+v", outer.took)
	}

	// And one that refuses passes it on.
	inner.refuses = true
	inner.took = nil
	l.HandleMouseMove(core.MouseMoveEvent{X: -1, Y: -1})
	l.HandleMouseMove(core.MouseMoveEvent{X: 10, Y: 4})
	if len(outer.took) != 1 {
		t.Errorf("a handler that refused did not pass it on: %+v", outer.took)
	}
}

// A tree answers for the cell under the pointer, not for the tree: that is the
// part being read, and the part whose text was cut.
func TestATreeOffersTheCellUnderThePointer(t *testing.T) {
	px, err := raster.New(900, 200)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	tree := NewTreeView()
	if err := tree.AddColumn(&TreeColumn{ID: "identity", Caption: "Identity", Width: 80}); err != nil {
		t.Fatal(err)
	}
	short := NewTreeItem("aa")
	short.SetValue("identity", "x")
	long := NewTreeItem("bb")
	long.SetValue("identity", "sha256:d223d09d37f7a426b58eff67d4bb3b7bd57000000000000")
	tree.AddRootItem(short)
	tree.AddRootItem(long)
	tree.SetBounds(core.UnitRect{Width: 300, Height: 120})

	metrics := tree.EffectiveCellMetrics()
	row := func(i int) core.Unit {
		return tree.headerHeight() + core.Unit(i)*metrics.UnitsPerCellHeight + 1
	}
	lay := tree.columnLayout()
	var identityX core.Unit
	for _, sp := range lay.spans {
		if sp.col != nil && sp.col.ID == "identity" {
			identityX = sp.x + 2
		}
	}

	text, at, ok := tree.TooltipAt(core.UnitPoint{X: identityX, Y: row(1)})
	if !ok {
		t.Fatal("the long cell offered nothing")
	}
	if !strings.HasPrefix(text, "sha256:") {
		t.Errorf("it offered %q, not the cell under the pointer", text)
	}
	if at.Height != metrics.UnitsPerCellHeight {
		t.Errorf("the rect it named is %d tall, not one row", at.Height)
	}
	if _, _, ok := tree.TooltipAt(core.UnitPoint{X: identityX, Y: row(0)}); ok {
		t.Error("a cell showing all of its text offered something anyway")
	}
	// A cell reads on in place rather than being repeated somewhere else.
	if tree.TooltipSide() != core.TooltipOver {
		t.Errorf("a tree asks for its tooltips %v", tree.TooltipSide())
	}
}

// A splitter's caption is drawn in the middle of its divider band, dressed in
// dots. When the band is too narrow for it, the band offers the title -- the
// dots are decoration, and the reader is not missing them.
func TestASplitterOffersItsTitleFromTheBand(t *testing.T) {
	px, err := raster.New(900, 200)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	c := newCatcher()
	sp := NewSplitter(core.Vertical)
	sp.SetTitle(longCaption)
	sp.SetParent(c)
	sp.SetBounds(core.UnitRect{Width: 120, Height: 160})
	sp.Paint(core.NewPainter(px))

	band := sp.dividerBounds()
	mid := core.UnitPoint{X: band.X + band.Width/2, Y: band.Y + band.Height/2}
	text, at, ok := sp.TooltipAt(mid)
	if !ok {
		t.Fatal("a band too narrow for its caption offered nothing")
	}
	if text != longCaption {
		t.Errorf("it offered %q, not its title", text)
	}
	if at != band {
		t.Errorf("it named %+v, not the band at %+v", at, band)
	}

	// Off the band, the panes answer for themselves.
	if _, _, ok := sp.TooltipAt(core.UnitPoint{X: band.X, Y: band.Y + band.Height + 8}); ok {
		t.Error("the splitter answered for a pane that is not its own caption")
	}

	// And the pointer arriving on the band makes the offer.
	sp.HandleMouseMove(core.MouseMoveEvent{X: mid.X, Y: mid.Y})
	if len(c.took) != 1 || c.took[0].Text != longCaption {
		t.Errorf("the pointer on the band produced %+v", c.took)
	}
}
