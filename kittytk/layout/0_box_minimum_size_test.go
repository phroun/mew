package layout

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// sizedTrinket answers a size of its own and carries a minimum, which is what
// min_width and min_height set.
type sizedTrinket struct {
	core.TrinketBase
	own core.UnitSize
}

func newSizedTrinket(w, h core.Unit) *sizedTrinket {
	s := &sizedTrinket{own: core.UnitSize{Width: w, Height: h}}
	s.TrinketBase = *core.NewTrinketBase()
	s.Init(s)
	return s
}

func (s *sizedTrinket) SizeHint() core.UnitSize { return s.own }

// LayoutAlignment is the align property, and these ask for left rather than
// the fill every item gets by default: a filled item takes the whole box
// across the cross axis, which is a size the box chose and not one the minimum
// had any say in. Left is the alignment that leaves the item its own size, so
// it is the one that can show a minimum being applied.
func (s *sizedTrinket) LayoutAlignment() (core.Alignment, bool) {
	return core.AlignLeft, true
}

// boxWith lays two items out in one box and returns their bounds.
func boxWith(o core.Orientation, items ...core.Trinket) []core.UnitRect {
	l := NewBoxLayout(o)
	l.SetSpacing(0)
	for _, it := range items {
		l.AddTrinket(it)
	}
	l.Layout(nil, core.UnitRect{Width: 400, Height: 400})
	out := make([]core.UnitRect, len(items))
	for i, it := range items {
		out[i] = it.Bounds()
	}
	return out
}

// min_width and min_height are properties a designer sets, in units, on any
// trinket. A box never read them -- it took only what each trinket answered
// for itself -- so setting them on anything laid out in a box did nothing at
// all: applied, then never consulted. GridLayout has always read them.
func TestBoxHonorsAChildsMinimumSize(t *testing.T) {
	for _, o := range []core.Orientation{core.Vertical, core.Horizontal} {
		small := newSizedTrinket(24, 16)
		small.SetMinimumSize(core.UnitSize{Width: 160, Height: 160})
		plain := newSizedTrinket(24, 16)

		got := boxWith(o, small, plain)

		if got[0].Width != 160 || got[0].Height != 160 {
			t.Errorf("orientation %v: a child asking 24x16 with a 160x160 minimum "+
				"was laid out %dx%d, want 160x160",
				o, got[0].Width, got[0].Height)
		}
		// A child with no minimum is untouched by this.
		if got[1].Width < 24 || got[1].Height < 16 {
			t.Errorf("orientation %v: the child with no minimum came out %dx%d, "+
				"smaller than the %dx%d it asked for",
				o, got[1].Width, got[1].Height, 24, 16)
		}
	}
}

// A minimum is a floor, not a size: a trinket that asks for more keeps it.
func TestBoxLeavesAChildBiggerThanItsMinimumAlone(t *testing.T) {
	big := newSizedTrinket(200, 200)
	big.SetMinimumSize(core.UnitSize{Width: 160, Height: 160})

	got := boxWith(core.Vertical, big)
	if got[0].Width != 200 || got[0].Height != 200 {
		t.Errorf("a child asking 200x200 over a 160x160 minimum was laid out %dx%d, want 200x200",
			got[0].Width, got[0].Height)
	}
}
