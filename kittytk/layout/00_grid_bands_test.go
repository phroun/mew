package layout

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// bandChild is a BLOCK child with a size of its own: a block carries no
// side-bearings, so a column's edges are exactly where its bands put them and
// a test can say so in numbers.
type bandChild struct {
	dirContainer
	own core.UnitSize
}

func newBandChild(w, h core.Unit) *bandChild {
	c := &bandChild{own: core.UnitSize{Width: w, Height: h}}
	c.TrinketBase = *core.NewTrinketBase()
	c.Init(c)
	return c
}

func (c *bandChild) SizeHint() core.UnitSize { return c.own }

// placedBlock returns a block child carrying a grid placement.
func placedBlock(w, h core.Unit, p core.GridPlacement) *bandChild {
	c := newBandChild(w, h)
	if p.RowSpan == 0 {
		p.RowSpan = 1
	}
	if p.ColumnSpan == 0 {
		p.ColumnSpan = 1
	}
	c.SetLayoutGridPlacement(p)
	return c
}

// bandGrid lays the children out in a grid with the given columns and returns
// where each landed.
func bandGrid(columns []Band, kids ...core.Trinket) []core.UnitRect {
	l := NewGridLayout()
	l.SetSpacing(0)
	for _, b := range columns {
		l.AddColumn(b)
	}
	c := newDirContainer(core.DirLTR)
	for _, k := range kids {
		c.AddChild(k)
		l.AddTrinket(k)
	}
	l.Layout(c, core.UnitRect{Width: 400, Height: 100})
	out := make([]core.UnitRect, len(kids))
	for i, k := range kids {
		out[i] = k.Bounds()
	}
	return out
}

// A band is a column because of where it was written, not because it was
// numbered: the first band given is column zero and the next is column one.
func TestBandsTakeTheIndexTheirPositionGives(t *testing.T) {
	got := bandGrid(
		[]Band{{Minimum: 40}, {Minimum: 60}, {Minimum: 20}},
		placedBlock(10, 20, core.GridPlacement{Row: 0, Column: 0}),
		placedBlock(10, 20, core.GridPlacement{Row: 0, Column: 1}),
		placedBlock(10, 20, core.GridPlacement{Row: 0, Column: 2}),
	)

	for i, want := range []core.UnitRect{
		{X: 0, Width: 40}, {X: 40, Width: 60}, {X: 100, Width: 20},
	} {
		if got[i].X != want.X || got[i].Width != want.Width {
			t.Errorf("child %d is at x=%d w=%d, want x=%d w=%d",
				i, got[i].X, got[i].Width, want.X, want.Width)
		}
	}
}

// A child places itself by naming a band, and lands in that band rather than
// in the column its index would have named.
func TestAChildPlacesItselfInTheBandItNames(t *testing.T) {
	got := bandGrid(
		[]Band{{ID: "labels", Minimum: 40}, {ID: "fields", Minimum: 60}},
		placedBlock(10, 20, core.GridPlacement{Row: 0, ColumnID: "fields"}),
		placedBlock(10, 20, core.GridPlacement{Row: 0, ColumnID: "labels"}),
	)

	if got[0].X != 40 || got[0].Width != 60 {
		t.Errorf(`column="fields" landed at x=%d w=%d, want x=40 w=60`, got[0].X, got[0].Width)
	}
	if got[1].X != 0 || got[1].Width != 40 {
		t.Errorf(`column="labels" landed at x=%d w=%d, want x=0 w=40`, got[1].X, got[1].Width)
	}

	// A name nothing answers to leaves the index standing, so a misspelling
	// does not silently move a child somewhere else.
	got = bandGrid(
		[]Band{{ID: "labels", Minimum: 40}, {ID: "fields", Minimum: 60}},
		placedBlock(10, 20, core.GridPlacement{Row: 0, Column: 1, ColumnID: "feilds"}),
	)
	if got[0].X != 40 {
		t.Errorf("an unanswered name landed the child at x=%d, want the index's 40", got[0].X)
	}
}

// The reason to name a band: inserting one ahead of it renumbers every column
// after, and a child that named its band does not move with the numbers.
func TestANamedBandSurvivesAnInsertionThatANumberDoesNot(t *testing.T) {
	byName := core.GridPlacement{Row: 0, ColumnID: "fields"}
	byNumber := core.GridPlacement{Row: 0, Column: 1}

	before := bandGrid(
		[]Band{{ID: "labels", Minimum: 40}, {ID: "fields", Minimum: 60}},
		placedBlock(10, 20, byName), placedBlock(10, 20, byNumber),
	)
	if before[0].X != before[1].X {
		t.Fatalf("with no band inserted, the two children start at %d and %d; they name the same column",
			before[0].X, before[1].X)
	}

	after := bandGrid(
		[]Band{{ID: "extra", Minimum: 20}, {ID: "labels", Minimum: 40}, {ID: "fields", Minimum: 60}},
		placedBlock(10, 20, byName), placedBlock(10, 20, byNumber),
	)
	if after[0].X != 60 || after[0].Width != 60 {
		t.Errorf(`column="fields" landed at x=%d w=%d after an insertion, want x=60 w=60`,
			after[0].X, after[0].Width)
	}
	if after[1].X != 20 || after[1].Width != 40 {
		t.Errorf("column=1 landed at x=%d w=%d after an insertion, want the second band's x=20 w=40",
			after[1].X, after[1].Width)
	}
}

// A child may be given before the bands are: what it asks of the band it
// names is folded in once the grid has that band, not when the child arrived.
func TestAChildsStretchReachesABandGivenAfterIt(t *testing.T) {
	l := NewGridLayout()
	l.SetSpacing(0)

	stretching := placedBlock(10, 20, core.GridPlacement{
		Row: 0, ColumnID: "fields", ColumnStretch: 1,
	})
	fixed := placedBlock(10, 20, core.GridPlacement{Row: 0, ColumnID: "labels"})

	c := newDirContainer(core.DirLTR)
	for _, k := range []core.Trinket{stretching, fixed} {
		c.AddChild(k)
		l.AddTrinket(k)
	}
	// The bands arrive after the children, which is the order a build script
	// is free to write them in.
	l.AddColumn(Band{ID: "labels", Minimum: 40})
	l.AddColumn(Band{ID: "fields", Minimum: 60})

	l.Layout(c, core.UnitRect{Width: 400, Height: 100})

	if fixed.Bounds().Width != 40 {
		t.Errorf("the band nobody stretched is %d wide, want its minimum of 40", fixed.Bounds().Width)
	}
	if w := stretching.Bounds().Width; w != 360 {
		t.Errorf("the stretching band is %d wide, want the remaining 360", w)
	}
}
