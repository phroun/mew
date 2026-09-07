package core

import "testing"

// The four logical alignments swap sides with the direction they are stated
// against, and the two optical ones never move.
//
// Which direction each is stated against is the whole point: textnatural follows
// the trinket's own text, layoutnatural follows the room it sits in, and a form
// mixing the two -- an English caption in a right-to-left dialog -- is where
// they come apart.
func TestResolveHAlignSpendsTheRightDirection(t *testing.T) {
	for _, c := range []struct {
		align        HAlign
		text, layout Direction
		want         HSide
		note         string
	}{
		// Both directions agree: everything logical begins on that side.
		{AlignTextNatural, DirLTR, DirLTR, SideLeft, "English caption in an English form"},
		{AlignTextOpposite, DirLTR, DirLTR, SideRight, ""},
		{AlignLayoutNatural, DirLTR, DirLTR, SideLeft, ""},
		{AlignLayoutOpposite, DirLTR, DirLTR, SideRight, ""},
		{AlignTextNatural, DirRTL, DirRTL, SideRight, "Hebrew caption in a Hebrew form"},
		{AlignTextOpposite, DirRTL, DirRTL, SideLeft, ""},
		{AlignLayoutNatural, DirRTL, DirRTL, SideRight, ""},
		{AlignLayoutOpposite, DirRTL, DirRTL, SideLeft, ""},

		// They disagree, which is what tells the two pairs apart.
		{AlignTextNatural, DirLTR, DirRTL, SideLeft, "English caption in a Hebrew form"},
		{AlignTextOpposite, DirLTR, DirRTL, SideRight, ""},
		{AlignLayoutNatural, DirLTR, DirRTL, SideRight, ""},
		{AlignLayoutOpposite, DirLTR, DirRTL, SideLeft, ""},
		{AlignTextNatural, DirRTL, DirLTR, SideRight, "Hebrew caption in an English form"},
		{AlignLayoutNatural, DirRTL, DirLTR, SideLeft, ""},

		// Centre and the optical pair, which no direction touches.
		{AlignCenter, DirRTL, DirRTL, SideCenter, ""},
		{AlignOpticalLeft, DirRTL, DirRTL, SideLeft, ""},
		{AlignOpticalRight, DirRTL, DirRTL, SideRight, ""},
		{AlignOpticalLeft, DirLTR, DirLTR, SideLeft, ""},
		{AlignOpticalRight, DirLTR, DirLTR, SideRight, ""},

		// An unresolved text direction takes the layout's, which is what makes
		// textnatural land where layoutnatural does for a caption of digits.
		{AlignTextNatural, DirInherit, DirRTL, SideRight, "a number in a Hebrew form"},
		{AlignTextOpposite, DirInherit, DirRTL, SideLeft, ""},
		{AlignTextNatural, DirInherit, DirLTR, SideLeft, ""},

		// An unresolved layout direction is left to right.
		{AlignLayoutNatural, DirInherit, DirInherit, SideLeft, ""},
		{AlignTextNatural, DirInherit, DirInherit, SideLeft, ""},
		{AlignTextNatural, DirRTL, DirInherit, SideRight, ""},
	} {
		got := ResolveHAlign(c.align, c.text, c.layout)
		if got != c.want {
			t.Errorf("ResolveHAlign(%v, text=%v, layout=%v) = %v, want %v %s",
				c.align, c.text, c.layout, got, c.want, c.note)
		}
	}
}

// The default fills both axes and centres on either one with nothing to fill,
// which is what every item gets when nothing says otherwise.
func TestDefaultAlignmentFillsAndCentres(t *testing.T) {
	d := DefaultAlignment()
	if !d.FillH || !d.FillV {
		t.Errorf("the default fills %v/%v, want both axes", d.FillH, d.FillV)
	}
	if d.H != AlignCenter || d.V != AlignMiddle {
		t.Errorf("the default sits at %v/%v, want centre and middle", d.H, d.V)
	}
}
