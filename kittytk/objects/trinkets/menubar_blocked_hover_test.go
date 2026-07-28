package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A modally-blocked menu bar must not highlight the item under the pointer:
// HandleMouseMove leaves hoverIndex clear while the blocked predicate is true,
// and resumes tracking once it clears.
func TestMenuBarBlockedSuppressesHover(t *testing.T) {
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("File"))
	mb.AddMenu(NewMenu("Edit"))
	mb.SetBounds(core.UnitRect{Width: mb.SizeHint().Width + 40, Height: 16})

	blocked := false
	mb.SetModalBlockedChecker(func() bool { return blocked })

	// A point inside the first top-level item.
	inFirst := core.MouseMoveEvent{X: mb.calculateMenuX(0) + core.DefaultCellMetrics().CellWidth, Y: 0}

	// Not blocked: hovering highlights the item.
	mb.HandleMouseMove(inFirst)
	if mb.hoverIndex < 0 {
		t.Fatalf("precondition: an unblocked bar should highlight the hovered item, got hoverIndex=%d", mb.hoverIndex)
	}

	// Blocked: a move clears the hover and tracks nothing.
	blocked = true
	mb.HandleMouseMove(inFirst)
	if mb.hoverIndex != -1 {
		t.Errorf("a blocked bar should not highlight an item, got hoverIndex=%d", mb.hoverIndex)
	}

	// Unblocked again: hovering highlights once more.
	blocked = false
	mb.HandleMouseMove(inFirst)
	if mb.hoverIndex < 0 {
		t.Errorf("hover should resume once the bar is unblocked, got hoverIndex=%d", mb.hoverIndex)
	}
}
