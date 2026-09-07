package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// stripIn builds a tab strip inside a form reading the given way.
func stripIn(dir core.Direction, pos TabPosition) *TabTrinket {
	form := NewPanel()
	form.SetDirection(dir)
	tt := NewTabTrinket()
	tt.SetTabPosition(pos)
	form.AddChild(tt)
	tt.SetBounds(core.UnitRect{Width: 60 * 8, Height: 20 * 16})
	return tt
}

// top and bottom name an edge no direction moves. The other four are sides,
// and which side is the direction's answer -- except for the optical pair,
// which names a side of the screen and stays there.
func TestAStripStandsOnTheEdgeItsPositionResolvesTo(t *testing.T) {
	for _, tc := range []struct {
		pos      TabPosition
		ltr, rtl TabEdge
	}{
		{TabsTop, TabEdgeTop, TabEdgeTop},
		{TabsBottom, TabEdgeBottom, TabEdgeBottom},
		{TabsSide, TabEdgeLeft, TabEdgeRight},
		{TabsSideOpposite, TabEdgeRight, TabEdgeLeft},
		{TabsOpticalLeft, TabEdgeLeft, TabEdgeLeft},
		{TabsOpticalRight, TabEdgeRight, TabEdgeRight},
	} {
		if got := stripIn(core.DirLTR, tc.pos).tabEdge(); got != tc.ltr {
			t.Errorf("position %d in a form reading left to right stands on edge %d, want %d",
				tc.pos, got, tc.ltr)
		}
		if got := stripIn(core.DirRTL, tc.pos).tabEdge(); got != tc.rtl {
			t.Errorf("position %d in a form reading right to left stands on edge %d, want %d",
				tc.pos, got, tc.rtl)
		}
	}
}

// Standing on a side is what decides which axis the strip runs along, and all
// four side words put it on one.
func TestEverySideWordPutsTheStripOnASide(t *testing.T) {
	for _, pos := range []TabPosition{TabsSide, TabsSideOpposite, TabsOpticalLeft, TabsOpticalRight} {
		for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
			if !stripIn(dir, pos).onSide() {
				t.Errorf("%v: position %d does not read as a side", dir, pos)
			}
		}
	}
	for _, pos := range []TabPosition{TabsTop, TabsBottom} {
		if stripIn(core.DirLTR, pos).onSide() {
			t.Errorf("position %d reads as a side", pos)
		}
	}
}

// And the strip really moves: the content it leaves room for sits on the other
// side of it, so a form reading right to left opens its tabs on the right and
// its content on the left.
func TestTurningAFormMovesWhatASideStripLeavesRoomFor(t *testing.T) {
	for _, tc := range []struct {
		dir core.Direction
		// whether the content starts at x=0, i.e. the strip is past it
		contentFirst bool
	}{
		{core.DirLTR, false},
		{core.DirRTL, true},
	} {
		c := stripIn(tc.dir, TabsSide).contentBounds()
		if first := c.X == 0; first != tc.contentFirst {
			t.Errorf("%v: a side strip leaves content at x=%d; it stands on the wrong edge",
				tc.dir, c.X)
		}
		if c.Width <= 0 || c.Width >= 60*8 {
			t.Errorf("%v: the content is %d wide, so no strip took any room", tc.dir, c.Width)
		}
	}
}
