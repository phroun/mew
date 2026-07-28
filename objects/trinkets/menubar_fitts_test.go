package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// On a graphical surface the menu items are indented a little from the
// left edge so the active item's outline stroke isn't clipped. To
// compensate (Fitts's law), the first item's click target reaches the
// very left edge: a click in the indent - or the top-left corner - still
// activates it, so nothing on the edge is dead.
func TestMenuBarFirstItemFittsHitArea(t *testing.T) {
	mb := NewMenuBar()
	for _, name := range []string{"File", "Edit", "View"} {
		m := NewMenu(name)
		m.AddItem(NewMenuItem("x"))
		mb.AddMenu(m)
	}
	mb.SetHideCalendar(true)
	mb.graphicalCached = true // as if the last paint was on a pixel surface
	mb.SetBounds(core.UnitRect{Width: mb.SizeHint().Width + 40, Height: 16})

	// The first item is indented from the left edge.
	if got := mb.calculateMenuX(0); got != menuBarLeftInset {
		t.Fatalf("first item X = %d, want indented by %d", got, menuBarLeftInset)
	}

	// A click at the very left edge - inside the indent, left of the item -
	// still opens the first menu.
	mb.HandleMousePress(core.MousePressEvent{X: 0, Y: 0, Button: core.LeftButton})
	if mb.activeMenu == nil || mb.currentIndex != 0 {
		t.Errorf("leftmost-edge click did not open the first menu (idx=%d)", mb.currentIndex)
	}
	mb.CloseMenu()

	// A cell surface has no indent (nor stroke): the first item sits flush
	// at the left edge.
	mb.graphicalCached = false
	if got := mb.calculateMenuX(0); got != 0 {
		t.Errorf("cell-surface first item X = %d, want 0 (flush)", got)
	}
}
