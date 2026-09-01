package layout

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// An item gets fill unless something sets align, which is what the property's
// documentation says and what AlignFill being the zero value of Alignment
// already says. NewLayoutItem said left instead, so every trinket in a box was
// laid out at its own width in a vertical box and its own height in a
// horizontal one, and align=fill had to be written on anything meant to take
// the room it was given.
func TestAnItemFillsUnlessItSaysOtherwise(t *testing.T) {
	if got := NewLayoutItem(nil).Align; got != core.AlignFill {
		t.Errorf("a fresh layout item aligns %v, want fill", got)
	}

	// And it reaches the layout: the cross axis is the box's, not the item's.
	// A trinket of its own, because the one the other tests use asks for left.
	for _, o := range []core.Orientation{core.Vertical, core.Horizontal} {
		small := newUnalignedTrinket(24, 16)
		got := boxWith(o, small)[0]
		if o == core.Vertical && got.Width != 400 {
			t.Errorf("vertical box: an unaligned item is %d wide, want the box's 400", got.Width)
		}
		if o == core.Horizontal && got.Height != 400 {
			t.Errorf("horizontal box: an unaligned item is %d tall, want the box's 400", got.Height)
		}
	}
}

// unalignedTrinket sets no align of its own, so a box gives it the default.
type unalignedTrinket struct {
	core.TrinketBase
	own core.UnitSize
}

func newUnalignedTrinket(w, h core.Unit) *unalignedTrinket {
	u := &unalignedTrinket{own: core.UnitSize{Width: w, Height: h}}
	u.TrinketBase = *core.NewTrinketBase()
	u.Init(u)
	return u
}

func (u *unalignedTrinket) SizeHint() core.UnitSize { return u.own }
