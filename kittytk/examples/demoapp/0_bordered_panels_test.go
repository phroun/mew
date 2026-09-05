package main

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
)

// A bordered panel's frame is drawn inside its own bounds and its children are
// laid out in what is left, so nothing it holds may reach the frame.
//
// A panel asked only for what its content needed and not for the two rows and
// two columns its frame takes, so a panel sitting at its own hint drew its
// border through its children -- visibly, in every tab that puts a bordered box
// at its natural size.
func TestABorderedPanelHoldsItsChildren(t *testing.T) {
	for _, tab := range []string{"Grid", "Flex", "Basic Trinkets", "Selection"} {
		tabs := openTab(t, tab)
		checked := 0
		for _, p := range panelsUnder(tabs) {
			if !p.Border() || p.LayoutManager() == nil || len(p.Children()) == 0 {
				continue
			}
			b := p.Bounds()
			if b.Width <= 0 || b.Height <= 0 {
				continue
			}
			checked++
			m := core.FindEffectiveCellMetrics(p.Self())
			// The interior, in the panel's own coordinates: one cell in on
			// every side, which is where Panel.Layout starts its children.
			left, top := m.UnitsPerCellWidth, m.UnitsPerCellHeight
			right, bottom := b.Width-m.UnitsPerCellWidth, b.Height-m.UnitsPerCellHeight

			for _, k := range p.Children() {
				r := k.Bounds()
				if r.Width <= 0 || r.Height <= 0 {
					continue
				}
				if r.X < left || r.Y < top || r.X+r.Width > right || r.Y+r.Height > bottom {
					t.Errorf("%s: a %T in a bordered panel occupies %+v, outside the interior %d,%d..%d,%d",
						tab, k, r, left, top, right, bottom)
				}
			}
		}
		if checked == 0 {
			t.Logf("%s: no bordered panel with children", tab)
		}
	}
}

// Nothing in a tab's own column overlaps anything else in it. A layout that
// reports a size smaller than it lays out puts its neighbours underneath it.
func TestATabsColumnDoesNotOverlapItself(t *testing.T) {
	for _, tab := range []string{"Grid", "Flex"} {
		tabs := openTab(t, tab)
		for _, p := range panelsUnder(tabs) {
			// Vertical boxes only: children of a horizontal one share a y by
			// design, and a grid's cells are checked by their own tests.
			box, ok := p.LayoutManager().(interface{ Orientation() core.Orientation })
			if !ok || box.Orientation() != core.Vertical {
				continue
			}
			kids := p.Children()
			for i := 1; i < len(kids); i++ {
				prev, cur := kids[i-1].Bounds(), kids[i].Bounds()
				if prev.Height <= 0 || cur.Height <= 0 {
					continue
				}
				if cur.Y < prev.Y+prev.Height {
					t.Errorf("%s: a %T at %+v starts before the %T above it ends at %d",
						tab, kids[i], cur, kids[i-1], prev.Y+prev.Height)
				}
			}
		}
	}
}

// A wrapping flex run reports the height the wrapping actually needs, so the
// panel holding it is built tall enough. Reporting one line's height and then
// laying out three is what put the run through everything below it.
func TestAWrappingFlexPanelIsBuiltTallEnough(t *testing.T) {
	tabs := openTab(t, "Flex")

	var run *trinkets.Panel
	for _, p := range panelsUnder(tabs) {
		buttons := 0
		for _, k := range p.Children() {
			if _, ok := k.(*trinkets.Button); ok {
				buttons++
			}
		}
		if buttons == 8 {
			run = p
		}
	}
	if run == nil {
		t.Fatal("the Flex tab has no panel of eight buttons")
	}

	lowest := core.Unit(0)
	for _, k := range run.Children() {
		if b := k.Bounds(); b.Y+b.Height > lowest {
			lowest = b.Y + b.Height
		}
	}
	m := core.FindEffectiveCellMetrics(run.Self())
	if want := run.Bounds().Height - m.UnitsPerCellHeight; lowest > want {
		t.Errorf("the wrapped run reaches y=%d in a panel whose interior ends at %d", lowest, want)
	}
}
