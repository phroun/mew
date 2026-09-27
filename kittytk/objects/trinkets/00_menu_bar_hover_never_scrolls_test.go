package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// elidedTitleX reports a pointer position inside the last PARTIALLY visible
// title on an overflowing bar -- the one clipped by the right limit -- and
// which menu that is.
func elidedTitleX(t *testing.T, m *MenuBar) (core.Unit, int) {
	t.Helper()
	limit := m.menusRightLimit()
	x := m.leftInset()
	if m.scrollOffset > 0 {
		x += m.ellipsisWidth()
	}
	for i := m.scrollOffset; i < len(m.menus); i++ {
		w := m.menuTitleWidth(m.menus[i].title)
		if x+w > limit && x < limit {
			return x + 1, i
		}
		x += w
	}
	t.Fatal("precondition: no title is clipped by the bar's right limit")
	return 0, -1
}

// Hovering a menu title never scrolls the bar, however little of that title
// is showing.
//
// A scroll moves the titles out from under a pointer that has not moved, and
// the bar scrolls by whole menus: bringing an elided title fully into view
// overshoots by the width of whichever menu falls off the left, so the next
// title slides into the sliver at the right edge. The move event after that
// -- a pixel of jitter will do -- finds a different index and scrolls again.
// Pointer travel per menu advanced is nil, so a drag onto the last visible
// title used to run away to the end of the bar with nothing the hand could do
// about it.
func TestMenuBarHoverDoesNotScrollTheBar(t *testing.T) {
	m := newOverflowingMenuBar()
	if !m.menusNeedScrolling() {
		t.Fatal("precondition: bar should overflow")
	}
	x, elided := elidedTitleX(t, m)
	if elided <= 0 {
		t.Fatalf("precondition: the clipped title is menu %d, want one past the first", elided)
	}

	// Press the first menu open, then drag right onto the elided title.
	if !m.HandleMousePress(core.MousePressEvent{X: m.leftInset() + 1, Y: 0, Button: core.LeftButton}) {
		t.Fatal("press on the first menu not consumed")
	}
	if m.activeMenu != m.menus[0] {
		t.Fatalf("press opened %v, want the first menu", m.activeMenu)
	}

	// Ten move events at ONE position: a hand holding still while the
	// window system reports what it always reports.
	for i := 0; i < 10; i++ {
		m.HandleMouseMove(core.MouseMoveEvent{X: x, Y: 0})
		if m.scrollOffset != 0 {
			t.Fatalf("move %d: hovering the elided title scrolled the bar to offset %d",
				i+1, m.scrollOffset)
		}
	}
	// And what is open is the title under the pointer, not whatever the bar
	// would have walked to.
	if m.activeMenu != m.menus[elided] {
		t.Errorf("hovering menu %d (%q) left %v open",
			elided, m.menus[elided].title, m.activeMenu)
	}
}

// One hover, one menu: dragging along the bar opens each title the pointer
// actually crosses, and the pointer stays on the title it opened.
//
// The runaway showed up as a hover that advanced further than the hand did,
// so what pins it is the relationship between the two: what opens is what
// menuItemAt says is under the pointer, at every step.
func TestMenuBarDragOpensWhatIsUnderThePointer(t *testing.T) {
	m := newOverflowingMenuBar()
	x := m.leftInset()
	if !m.HandleMousePress(core.MousePressEvent{X: x + 1, Y: 0, Button: core.LeftButton}) {
		t.Fatal("press on the first menu not consumed")
	}

	for px := x + 1; px < m.menusRightLimit(); px++ {
		m.HandleMouseMove(core.MouseMoveEvent{X: px, Y: 0})
		want := m.menuItemAt(px, 0)
		if want < 0 {
			continue
		}
		if m.activeMenu != m.menus[want] {
			t.Fatalf("x=%d is over menu %d (%q), but %v is open",
				px, want, m.menus[want].title, m.activeMenu)
		}
		if m.scrollOffset != 0 {
			t.Fatalf("x=%d: the drag scrolled the bar to offset %d", px, m.scrollOffset)
		}
	}
}

// The deliberate acts still scroll. Hovering is the one that must not: a
// keystroke, an accelerator or a press is a discrete thing the hand asked
// for, and a menu it names off the right of the bar comes into view.
func TestMenuBarKeyboardStillScrollsIntoView(t *testing.T) {
	m := newOverflowingMenuBar()
	last := len(m.menus) - 1
	if m.menuItemAt(m.leftInset()+1, 0) == last {
		t.Fatal("precondition: the last menu should start off the right of the bar")
	}

	m.OpenMenu(last)
	if m.scrollOffset == 0 {
		t.Errorf("opening the last menu left the bar at offset 0; it should have scrolled to reach it")
	}
	if m.activeMenu != m.menus[last] {
		t.Errorf("OpenMenu(%d) opened %v", last, m.activeMenu)
	}
}
