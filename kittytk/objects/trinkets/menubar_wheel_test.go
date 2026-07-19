package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// newOverflowingMenuBar builds a bar narrow enough that its menus don't
// all fit, so the [<]/[>] buttons and wheel panning apply.
func newOverflowingMenuBar() *MenuBar {
	m := NewMenuBar()
	// Width leaves only a little room past the reserved date/time area,
	// so a handful of menus overflow.
	m.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 320, Height: 16})
	for _, title := range []string{"File", "Edit", "View", "Insert", "Format", "Tools", "Window", "Help"} {
		m.AddMenu(NewMenu(title))
	}
	return m
}

// A wheel or two-finger pan over an overflowing bar steps the first
// visible menu left/right, just like the [<]/[>] buttons.
func TestMenuBarWheelPansOverflow(t *testing.T) {
	m := newOverflowingMenuBar()
	if !m.menusNeedScrolling() {
		t.Fatal("precondition: bar should overflow")
	}
	if m.scrollOffset != 0 {
		t.Fatalf("initial scrollOffset = %d, want 0", m.scrollOffset)
	}

	// Wheel right (vertical notch, no horizontal axis) steps right.
	if !m.HandleMouseWheel(core.MouseWheelEvent{DeltaY: 1}) {
		t.Fatal("wheel right not consumed")
	}
	if m.scrollOffset != 1 {
		t.Errorf("after wheel right: scrollOffset = %d, want 1", m.scrollOffset)
	}

	// Horizontal axis takes priority and steps left back to 0.
	if !m.HandleMouseWheel(core.MouseWheelEvent{DeltaX: -1, DeltaY: 1}) {
		t.Fatal("wheel left not consumed")
	}
	if m.scrollOffset != 0 {
		t.Errorf("after wheel left: scrollOffset = %d, want 0", m.scrollOffset)
	}

	// A precise (trackpad) pan contributes sign only: pan right steps once.
	if !m.HandleMouseWheel(core.MouseWheelEvent{PreciseX: 3.5}) {
		t.Fatal("precise pan not consumed")
	}
	if m.scrollOffset != 1 {
		t.Errorf("after precise pan: scrollOffset = %d, want 1", m.scrollOffset)
	}
}

// While the bar is scrollable the wheel stays consumed (matching the
// tab strip), but the offset clamps at each end - a further step past
// the edge is a no-op, not an out-of-range scroll.
func TestMenuBarWheelClampsAtEnds(t *testing.T) {
	m := newOverflowingMenuBar()

	// Already fully left: the gesture is consumed but the offset holds.
	if !m.HandleMouseWheel(core.MouseWheelEvent{DeltaX: -1}) {
		t.Error("scrollable bar should consume the wheel even at offset 0")
	}
	if m.scrollOffset != 0 {
		t.Errorf("wheel left at offset 0 changed offset to %d, want 0", m.scrollOffset)
	}

	// Step right until the last menu is fully visible.
	guard := 0
	for m.canScrollRight() && guard < 100 {
		m.HandleMouseWheel(core.MouseWheelEvent{DeltaX: 1})
		guard++
	}
	end := m.scrollOffset

	// A further right step is consumed but holds at the end.
	if !m.HandleMouseWheel(core.MouseWheelEvent{DeltaX: 1}) {
		t.Error("scrollable bar should consume the wheel at the right end")
	}
	if m.scrollOffset != end {
		t.Errorf("wheel right at the end changed offset to %d, want %d", m.scrollOffset, end)
	}
}

// An open dropdown owns the wheel: scrolling it (even a short one that
// can't scroll) must never pan the bar underneath - those are separate
// gestures.
func TestMenuBarWheelDropdownDoesNotPanBar(t *testing.T) {
	m := newOverflowingMenuBar()
	m.OpenMenu(0) // a short dropdown (no items) that can't scroll
	if m.ActiveMenu() == nil {
		t.Fatal("menu did not open")
	}
	before := m.scrollOffset

	if !m.HandleMouseWheel(core.MouseWheelEvent{DeltaX: 1}) {
		t.Error("open dropdown should consume the wheel")
	}
	if m.scrollOffset != before {
		t.Errorf("wheel over open dropdown panned the bar: offset %d -> %d", before, m.scrollOffset)
	}
}

// A bar that fits entirely never consumes wheel events - they belong to
// whatever is under the pointer.
func TestMenuBarWheelIgnoredWhenNoOverflow(t *testing.T) {
	m := NewMenuBar()
	m.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 16})
	m.AddMenu(NewMenu("File"))
	m.AddMenu(NewMenu("Help"))
	if m.menusNeedScrolling() {
		t.Fatal("precondition: bar should fit")
	}
	if m.HandleMouseWheel(core.MouseWheelEvent{DeltaX: 1}) {
		t.Error("non-overflowing bar should not consume wheel")
	}
}
