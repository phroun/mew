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

// placeInHBox lays items out in a horizontal box and returns where each
// landed. The main axis is horizontal, which is the one the sizing pass
// settles.
func placeInHBox(c *dirContainer, width core.Unit, items ...core.Trinket) []core.UnitRect {
	l := NewBoxLayout(core.Horizontal)
	l.SetSpacing(0)
	for _, it := range items {
		c.AddChild(it)
		l.AddTrinket(it)
	}
	l.Layout(c, core.UnitRect{Width: width, Height: 100})
	out := make([]core.UnitRect, len(items))
	for i, it := range items {
		out[i] = it.Bounds()
	}
	return out
}

// What a trinket asks for is what it would LIKE, not the least it can do
// with. A row whose wishes come to more than it has brings them down together
// rather than handing each its wish and drawing the last of them past the end
// -- which shows two panels and a sliver of a third instead of three narrow
// ones, and leaves a caption believing it had room it never had.
func TestARowTooNarrowForItsWishesBringsThemDown(t *testing.T) {
	const rowW = core.Unit(400)
	a := newAlignedTrinket(500, 16, core.Alignment{})
	b := newAlignedTrinket(300, 16, core.Alignment{})

	got := placeInHBox(newDirContainer(core.DirLTR), rowW, a, b)

	var total core.Unit
	for i, r := range got {
		if r.Width <= 0 {
			t.Errorf("item %d was squeezed out of the row entirely", i)
		}
		total += r.Width
	}
	if total > rowW {
		t.Errorf("the row handed out %d across a row %d wide", total, rowW)
	}
	// Each keeps the same SHARE of what it asked for: 500 and 300 come back
	// in the ratio 5:3. Taking the shortfall equally instead would leave the
	// narrower one a third of its wish while the wider kept three fifths,
	// cutting most from the item that had least to spare.
	if got[0].Width*3 != got[1].Width*5 {
		t.Errorf("500 and 300 came back as %d and %d, not in proportion",
			got[0].Width, got[1].Width)
	}
	if last := got[len(got)-1]; last.X+last.Width > rowW {
		t.Errorf("the row ends at %d, past its own %d", last.X+last.Width, rowW)
	}
}

// A row with room for every wish grants them all untouched.
func TestARowWithRoomGrantsEveryWish(t *testing.T) {
	a := newAlignedTrinket(100, 16, core.Alignment{})
	b := newAlignedTrinket(80, 16, core.Alignment{})

	got := placeInHBox(newDirContainer(core.DirLTR), 400, a, b)
	if got[0].Width != 100 || got[1].Width != 80 {
		t.Errorf("a row with room gave out %d and %d, not 100 and 80", got[0].Width, got[1].Width)
	}
}

// A row with something elastic in it takes the deficit out of THAT, not out
// of the captions beside it: an expanding item is there to absorb, and cutting
// a caption while a spacer sits at full size loses words nobody needed to.
func TestARowSpendsItsElasticBeforeItsCaptions(t *testing.T) {
	caption := newAlignedTrinket(300, 16, core.Alignment{})
	elastic := newAlignedTrinket(300, 16, core.Alignment{})
	elastic.SetSizePolicy(core.NewSizePolicy(core.SizeExpanding, core.SizeFixed))

	got := placeInHBox(newDirContainer(core.DirLTR), 400, caption, elastic)

	if got[0].Width != 300 {
		t.Errorf("the caption was cut to %d with an expanding item beside it", got[0].Width)
	}
	if got[1].Width >= 300 {
		t.Errorf("the expanding item kept %d and gave up nothing", got[1].Width)
	}
}
