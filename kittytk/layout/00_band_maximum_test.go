package layout

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A band asks for nothing until it is asked to: the zero value has no ceiling,
// so a band written as a literal does not collapse the track it stands for.
func TestABandWrittenAsALiteralHasNoCeiling(t *testing.T) {
	if got := (Band{ID: "fields", Stretch: 1}).Ceiling(); got != core.Unbounded {
		t.Errorf("a band nobody capped has a ceiling of %d, want unbounded", got)
	}
	// And a ceiling of zero is kept as written, because zero collapses a
	// track and is a thing an author may mean.
	if got := (Band{}).Capped(0).Ceiling(); got != 0 {
		t.Errorf("a band capped at zero has a ceiling of %d", got)
	}
	if got := (Band{}).Capped(64).Ceiling(); got != 64 {
		t.Errorf("a band capped at 64 has a ceiling of %d", got)
	}
}

// A track stops growing where its band says, and the tracks beside it take
// what it turned down -- the same rule a child follows, one level up.
func TestAColumnStopsAtItsBandsMaximum(t *testing.T) {
	got := bandGrid(
		[]Band{
			Band{Stretch: 1}.Capped(80),
			{Stretch: 1},
		},
		placedBlock(10, 20, core.GridPlacement{Row: 0, Column: 0}),
		placedBlock(10, 20, core.GridPlacement{Row: 0, Column: 1}),
	)
	if got[0].Width != 80 {
		t.Errorf("a column capped at 80 is %d wide", got[0].Width)
	}
	if w := got[0].Width + got[1].Width; w != 400 {
		t.Errorf("the two columns come to %d of the 400 they share", w)
	}
}

// Where a band's maximum and what its children need conflict, the children
// win: a minimum is the stronger statement here as everywhere.
func TestAColumnsChildrenBeatItsBandsMaximum(t *testing.T) {
	got := bandGrid(
		[]Band{
			Band{Stretch: 1}.Capped(20),
			{Stretch: 1},
		},
		placedBlock(120, 20, core.GridPlacement{Row: 0, Column: 0}),
		placedBlock(10, 20, core.GridPlacement{Row: 0, Column: 1}),
	)
	if got[0].Width != 120 {
		t.Errorf("a column capped at 20 holding a 120-wide child is %d wide, want 120", got[0].Width)
	}
}

// A band capped at zero collapses its track while the grid keeps the column.
func TestABandCappedAtZeroCollapsesItsColumn(t *testing.T) {
	got := bandGrid(
		[]Band{
			Band{Stretch: 1}.Capped(0),
			{Stretch: 1},
		},
		placedBlock(0, 20, core.GridPlacement{Row: 0, Column: 0}),
		placedBlock(0, 20, core.GridPlacement{Row: 0, Column: 1}),
	)
	if got[0].Width != 0 {
		t.Errorf("a column capped at zero is %d wide", got[0].Width)
	}
	if got[1].Width != 400 {
		t.Errorf("the column beside it is %d wide, want the whole 400", got[1].Width)
	}
}
