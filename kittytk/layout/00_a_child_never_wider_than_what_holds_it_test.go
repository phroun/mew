package layout

// A box hands a child no more room than it has itself.

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A child that asks for more width than the box has is given the box's width,
// not its own wish. Handed more, it spills past the edge -- and since a
// trinket cuts its text to its OWN bounds, it also concludes it had room for
// all of it, so a caption too long for the panel runs off the side instead of
// eliding and has nothing to offer on hover either.
func TestAChildIsNeverWiderThanTheBoxHoldingIt(t *testing.T) {
	const boxW = core.Unit(400)
	for _, a := range []struct {
		name string
		h    core.HAlign
	}{
		{"textnatural", core.AlignTextNatural},
		{"textopposite", core.AlignTextOpposite},
		{"layoutnatural", core.AlignLayoutNatural},
		{"centre", core.AlignCenter},
		{"opticalright", core.AlignOpticalRight},
	} {
		item := newAlignedTrinket(4000, 16, core.Alignment{H: a.h, V: core.AlignMiddle})
		got := placeInVBox(newDirContainer(core.DirLTR), item)

		if got.Width > boxW {
			t.Errorf("%s: the child was given %d in a box %d wide", a.name, got.Width, boxW)
		}
		if got.X < 0 || got.X+got.Width > boxW {
			t.Errorf("%s: the child occupies %d..%d, outside the box", a.name, got.X, got.X+got.Width)
		}
	}
}

// And a child that fits keeps exactly what it asked for, placed by its
// alignment rather than stretched to the box.
func TestAChildThatFitsKeepsItsOwnWidth(t *testing.T) {
	const w = core.Unit(80)
	item := newAlignedTrinket(w, 16, core.Alignment{H: core.AlignLayoutNatural, V: core.AlignMiddle})
	got := placeInVBox(newDirContainer(core.DirLTR), item)

	if got.Width != w {
		t.Errorf("a child that fits was given %d, not the %d it asked for", got.Width, w)
	}
	if got.X != 0 {
		t.Errorf("a child aligned to the leading edge sits at %d", got.X)
	}
}
