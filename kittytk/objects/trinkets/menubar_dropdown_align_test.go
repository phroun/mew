package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A menu-bar dropdown normally left-aligns to its item, but right-aligns
// to the item when a left-aligned dropdown would run past the surface's
// right edge, and pins to the left edge when even right-aligning would
// fall off the left (a very narrow surface).
func TestMenuBarDropdownRightAlignsNearRightEdge(t *testing.T) {
	mb := NewMenuBar()
	// Several short menus so the rightmost one sits far from the left.
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		m := NewMenu(name)
		m.AddItem(NewMenuItem("x"))
		mb.AddMenu(m)
	}
	// The rightmost menu with a modest dropdown (fits when right-aligned).
	edit := NewMenu("Edit")
	edit.AddItem(NewMenuItem("Undo"))
	mb.AddMenu(edit)
	editIdx := len(mb.menus) - 1
	// Hide the clock only so the menu BAR's own horizontal scroll (which
	// reserves clock width) doesn't shift the item positions; the dropdown
	// limit itself is the full window surface, independent of the clock.
	mb.SetHideCalendar(true)

	itemX := mb.calculateMenuX(editIdx)
	itemW := mb.menuTitleWidth("Edit")
	dropW := edit.calculateSize().Width
	barW := mb.SizeHint().Width

	// --- Left-aligned: a surface wide enough for the whole dropdown. ---
	mb.SetBounds(core.UnitRect{Width: itemX + dropW + 40, Height: 16})
	mb.OpenMenu(editIdx)
	if got := edit.DropdownBounds().X; got != itemX {
		t.Errorf("wide surface: dropdown X = %d, want left-aligned to item %d", got, itemX)
	}
	mb.CloseMenu()

	// --- Right-aligned: surface right edge at the bar's end, so the
	// left-aligned dropdown would overflow. It aligns its right edge to
	// the item's right edge instead. (Requires dropW in (itemW, itemX+itemW].)
	if dropW <= itemW || dropW > itemX+itemW {
		t.Fatalf("test setup: need itemW(%d) < dropW(%d) <= itemX+itemW(%d)", itemW, dropW, itemX+itemW)
	}
	mb.SetBounds(core.UnitRect{Width: barW, Height: 16})
	mb.OpenMenu(editIdx)
	wantX := itemX + itemW - dropW
	if got := edit.DropdownBounds().X; got != wantX {
		t.Errorf("near right edge: dropdown X = %d, want right-aligned %d", got, wantX)
	}
	if got := edit.DropdownBounds().X + dropW; got != itemX+itemW {
		t.Errorf("dropdown right edge = %d, want item right edge %d", got, itemX+itemW)
	}
	mb.CloseMenu()
}

// When even a right-aligned dropdown would fall off the left edge (its
// content is wider than the space to the item's right edge), it pins its
// left edge to the surface's left edge instead of going negative.
func TestMenuBarDropdownPinsToLeftWhenTooNarrow(t *testing.T) {
	mb := NewMenuBar()
	first := NewMenu("File")
	first.AddItem(NewMenuItem("x"))
	mb.AddMenu(first)
	// A second menu whose dropdown is wider than its right edge, so
	// right-aligning would push its left edge below zero.
	wide := NewMenu("Edit")
	wide.AddItem(NewMenuItem("An exceptionally long menu entry indeed"))
	mb.AddMenu(wide)
	wideIdx := 1
	// Hide the clock only so the menu BAR's own horizontal scroll (which
	// reserves clock width) doesn't shift the item positions; the dropdown
	// limit itself is the full window surface, independent of the clock.
	mb.SetHideCalendar(true)

	itemX := mb.calculateMenuX(wideIdx)
	itemW := mb.menuTitleWidth("Edit")
	dropW := wide.calculateSize().Width
	if dropW <= itemX+itemW {
		t.Fatalf("test setup: need dropW(%d) > itemX+itemW(%d) to force a left pin", dropW, itemX+itemW)
	}
	if itemX == 0 {
		t.Fatal("test setup: item must not already be at the left edge")
	}

	// Surface wide enough to show the bar (no bar scroll) but far narrower
	// than the dropdown, so left-alignment overflows and right-alignment
	// underflows.
	mb.SetBounds(core.UnitRect{Width: mb.SizeHint().Width, Height: 16})
	mb.OpenMenu(wideIdx)
	if got := wide.DropdownBounds().X; got != 0 {
		t.Errorf("too-narrow surface: dropdown X = %d, want pinned to left edge 0", got)
	}
}
