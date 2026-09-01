package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// A CHOICE cell shows the tree's down-arrow hint at rest and a real ComboBox
// once the editor is up. Both hold room back at the right for that arrow, and
// the two have to hold back the SAME room: a value that fits under one and
// not the other is a value that changes width when the editor opens.
//
// The tree held back the arrow plus a whole cell where a ComboBox holds back
// the arrow plus a space, so "ARJ Archive" came out "ARJ Archi…" beside the
// hint and whole under the editor.
func TestChoiceHintHoldsBackWhatAComboBoxDoes(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.New(200, 64)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(b)

	tv := NewTreeView()
	tv.SetParent(d)
	cb := NewComboBox()
	cb.SetParent(d)

	// What a ComboBox reserves, read off the same expression its paint uses.
	want := cb.MeasureText(" " + choiceArrowGlyph)
	if got := tv.choiceArrowRoom(); got != want {
		t.Errorf("a tree holds back %d units for the choice arrow, a combo box %d",
			got, want)
	}
	if want <= 0 {
		t.Fatal("the arrow measures nothing; the comparison says nothing")
	}
}

// A sortable header holds back the same room for its sort indicator, and
// neededCells holds back the same again when it sizes a column to its
// content -- so a caption the column was measured to hold is not then elided
// by the paint. The header held back the arrow plus a whole CELL where the
// width had been sized for the arrow plus a space, about a character more:
// "Kind" came out "Ki…" in a column measured to show "Kin…" at worst.
func TestSortArrowRoomMatchesWhatTheColumnWasSizedFor(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.New(200, 64)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(b)

	tv := NewTreeView()
	tv.SetParent(d)

	col := NewTreeColumn("kind", "Kind", 4)
	col.Sortable = true
	tv.AddColumn(col)
	tv.SetSorted(true, 0, false)

	// The width neededCells asks for, and the room the header paint leaves
	// the caption inside it: the reservation, then drawAligned's own pad.
	cw := tv.EffectiveCellMetrics().UnitsPerCellWidth
	span := core.Unit(tv.neededCells(col)) * cw
	room := span - tv.arrowRoom("▲") - cw/2

	if got := ellipsizeText(tv.EffectiveFont(), tv.EffectiveCellMetrics(),
		col.Caption, room); got != col.Caption {
		t.Errorf("a column sized at %d units for %q shows %q: the paint holds back "+
			"more than the width was measured for", span, col.Caption, got)
	}
}

// And it follows the denomination, like every other width the tree counts.
func TestChoiceArrowRoomFollowsTheDenomination(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.New(200, 64)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(b)

	base := core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}
	var want core.Unit
	for _, m := range []core.CellMetrics{base, {UnitsPerCellWidth: 16, UnitsPerCellHeight: 32}, {UnitsPerCellWidth: 4, UnitsPerCellHeight: 8}} {
		tv := NewTreeView()
		tv.SetParent(d)
		cm := m
		tv.SetCellMetrics(&cm)
		got := core.ExchangeX(tv.choiceArrowRoom(), m, base)
		if want == 0 {
			want = got
			continue
		}
		if got != want {
			t.Errorf("at %dx%d the arrow room is %d units at 8x16, want %d",
				m.UnitsPerCellWidth, m.UnitsPerCellHeight, got, want)
		}
	}
}
