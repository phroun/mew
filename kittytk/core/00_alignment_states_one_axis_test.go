package core

import "testing"

// A child that asks about one axis has said nothing about the other. Under a
// single "was one set" flag there was no telling that from a child that stated
// everything, which is what stops a container answering for the axis nobody
// wrote -- a column band saying where its labels sit would be thrown away by a
// child that then wrote valign=top.
func TestAnAlignmentSaysWhichAxisItWasAskedAbout(t *testing.T) {
	a := Alignment{}.WithH(AlignOpticalRight)
	if !a.HSet {
		t.Error("WithH did not record that the horizontal was asked for")
	}
	if a.VSet || a.FillHSet || a.FillVSet {
		t.Errorf("halign alone stated more than the horizontal: %+v", a)
	}
	if !a.Stated() {
		t.Error("an alignment with one field asked for states nothing")
	}
	if (Alignment{}).Stated() {
		t.Error("an alignment nobody wrote states something")
	}

	// fill names both axes at once -- it is one word for the pair.
	f := Alignment{}.WithFill(true, false)
	if !f.FillHSet || !f.FillVSet {
		t.Errorf("fill did not state both axes: %+v", f)
	}
	if f.HSet || f.VSet {
		t.Errorf("fill said something about where the item sits: %+v", f)
	}
}

// Over is what a container's answer and a child's meet in: the child wins the
// fields it asked about, and the container keeps the rest.
func TestAlignmentOverKeepsWhatTheChildDidNotAskAbout(t *testing.T) {
	band := Alignment{H: AlignTextOpposite, V: AlignTop, FillH: false, FillV: false}.
		WithH(AlignTextOpposite).WithFill(false, false)

	child := Alignment{}.WithV(AlignBottom)
	got := child.Over(band)

	if got.V != AlignBottom || !got.VSet {
		t.Errorf("the child's own axis was lost: %+v", got)
	}
	if got.H != AlignTextOpposite {
		t.Errorf("the horizontal is %v, want the band's AlignTextOpposite -- the child said "+
			"nothing about it", got.H)
	}
	if got.FillH || got.FillV {
		t.Errorf("the band's fill was lost: %+v", got)
	}
}

// An alignment that states nothing is a whole answer written field by field by
// a Go caller, and is taken entire. Merging it would hand back a struct the
// caller never asked for.
func TestAlignmentOverTakesAnUnstatedAnswerEntire(t *testing.T) {
	whole := Alignment{H: AlignOpticalLeft, V: AlignTop}
	base := DefaultAlignment()
	if got := whole.Over(base); got != whole {
		t.Errorf("a hand-built alignment came back as %+v, want the %+v it was", got, whole)
	}
}
