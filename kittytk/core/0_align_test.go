package core

import "testing"

// The four logical alignments swap sides with the direction they are stated
// against, and the two optical ones never move.
//
// Which direction each is stated against is the whole point: textbegin follows
// the trinket's own text, layoutbegin follows the room it sits in, and a form
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
		{AlignTextBegin, DirLTR, DirLTR, SideLeft, "English caption in an English form"},
		{AlignTextEnd, DirLTR, DirLTR, SideRight, ""},
		{AlignLayoutBegin, DirLTR, DirLTR, SideLeft, ""},
		{AlignLayoutEnd, DirLTR, DirLTR, SideRight, ""},
		{AlignTextBegin, DirRTL, DirRTL, SideRight, "Hebrew caption in a Hebrew form"},
		{AlignTextEnd, DirRTL, DirRTL, SideLeft, ""},
		{AlignLayoutBegin, DirRTL, DirRTL, SideRight, ""},
		{AlignLayoutEnd, DirRTL, DirRTL, SideLeft, ""},

		// They disagree, which is what tells the two pairs apart.
		{AlignTextBegin, DirLTR, DirRTL, SideLeft, "English caption in a Hebrew form"},
		{AlignTextEnd, DirLTR, DirRTL, SideRight, ""},
		{AlignLayoutBegin, DirLTR, DirRTL, SideRight, ""},
		{AlignLayoutEnd, DirLTR, DirRTL, SideLeft, ""},
		{AlignTextBegin, DirRTL, DirLTR, SideRight, "Hebrew caption in an English form"},
		{AlignLayoutBegin, DirRTL, DirLTR, SideLeft, ""},

		// Centre and the optical pair, which no direction touches.
		{AlignCenter, DirRTL, DirRTL, SideCenter, ""},
		{AlignOpticalLeft, DirRTL, DirRTL, SideLeft, ""},
		{AlignOpticalRight, DirRTL, DirRTL, SideRight, ""},
		{AlignOpticalLeft, DirLTR, DirLTR, SideLeft, ""},
		{AlignOpticalRight, DirLTR, DirLTR, SideRight, ""},

		// An unresolved text direction takes the layout's, which is what makes
		// textbegin land where layoutbegin does for a caption of digits.
		{AlignTextBegin, DirInherit, DirRTL, SideRight, "a number in a Hebrew form"},
		{AlignTextEnd, DirInherit, DirRTL, SideLeft, ""},
		{AlignTextBegin, DirInherit, DirLTR, SideLeft, ""},

		// An unresolved layout direction is left to right.
		{AlignLayoutBegin, DirInherit, DirInherit, SideLeft, ""},
		{AlignTextBegin, DirInherit, DirInherit, SideLeft, ""},
		{AlignTextBegin, DirRTL, DirInherit, SideRight, ""},
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
