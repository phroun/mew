package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// twoMenuBar builds a bar with two top-level menus and returns the x of a
// point inside each title.
func twoMenuBar(t *testing.T) (*MenuBar, core.Unit, core.Unit) {
	t.Helper()
	mb := NewMenuBar()
	mb.SetBounds(core.UnitRect{Width: 800, Height: 16})

	first, second := NewMenu("File"), NewMenu("Edit")
	first.AddItem(NewMenuItem("Open"))
	second.AddItem(NewMenuItem("Copy"))
	mb.AddMenu(first)
	mb.AddMenu(second)

	x := mb.leftInset() + mb.menuTitleWidth(first.title)/2
	y := x + mb.menuTitleWidth(first.title)/2 + mb.menuTitleWidth(second.title)/2
	return mb, x, y
}

// On a CELL surface, clicking a second menu's title while the first is open
// opens the second one.
//
// The position report that arrives before a click is the only "move" a
// terminal produces, so treating it as hover meant the bar opened the menu on
// the move and then read the click that followed as a toggle — closing the
// menu the click was meant to open, and leaving a hover highlight where the
// dropdown should have been.
func TestCellSurfaceClickOpensTheOtherMenu(t *testing.T) {
	mb, firstX, secondX := twoMenuBar(t)
	mb.graphicalCached = false // a terminal

	mb.HandleMousePress(core.MousePressEvent{X: firstX, Y: 0, Button: core.LeftButton})
	mb.HandleMouseRelease(core.MouseReleaseEvent{X: firstX, Y: 0, Button: core.LeftButton})
	if mb.activeMenu != mb.menus[0] {
		t.Fatalf("precondition: clicking the first title left activeMenu=%v", mb.activeMenu)
	}

	// The position report, then the click, exactly as the key layer sends them.
	mb.HandleMouseMove(core.MouseMoveEvent{X: secondX, Y: 0})
	if mb.activeMenu != mb.menus[0] {
		t.Error("a bare move opened a menu on a cell surface, where there is no hover")
	}
	mb.HandleMousePress(core.MousePressEvent{X: secondX, Y: 0, Button: core.LeftButton})

	if mb.activeMenu != mb.menus[1] {
		got := "nothing"
		if mb.activeMenu != nil {
			got = mb.activeMenu.title
		}
		t.Errorf("clicking the second title left %s open, want Edit", got)
	}
}

// On a GRAPHICAL surface the pointer really does travel, and a dropdown that
// is open follows it along the bar — the behavior every graphical menu bar
// has. That is not lost by fixing the cell case.
func TestGraphicalSurfaceStillOpensOnHover(t *testing.T) {
	mb, firstX, secondX := twoMenuBar(t)
	mb.graphicalCached = true // a window

	mb.HandleMousePress(core.MousePressEvent{X: firstX, Y: 0, Button: core.LeftButton})
	mb.HandleMouseRelease(core.MouseReleaseEvent{X: firstX, Y: 0, Button: core.LeftButton})
	if mb.activeMenu != mb.menus[0] {
		t.Fatal("precondition: the first menu did not open")
	}

	mb.HandleMouseMove(core.MouseMoveEvent{X: secondX, Y: 0})
	if mb.activeMenu != mb.menus[1] {
		t.Error("hovering the second title with a dropdown open did not switch to it")
	}
}

// Hover is still TRACKED on a cell surface — the bar just never paints it.
//
// Tracking costs nothing and keeps one code path for both surfaces; what makes
// a terminal a terminal is that no hover STYLE is drawn, the same rule the
// buttons, the tree view and the window title buttons already follow.
func TestCellSurfaceTracksHoverButPaintsNone(t *testing.T) {
	mb, _, secondX := twoMenuBar(t)
	mb.graphicalCached = false

	mb.HandleMouseMove(core.MouseMoveEvent{X: secondX, Y: 0})
	if mb.hoverIndex != 1 {
		t.Errorf("hoverIndex = %d after a move over the second title, want 1", mb.hoverIndex)
	}
}
