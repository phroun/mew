package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A plain pointer move over a button sets its hover state; moving off
// clears it. The button must not consume the move (return false) so
// sibling widgets can still clear their own hover.
func TestButtonPointerHover(t *testing.T) {
	b := NewButton("ok")
	b.SetBounds(core.UnitRect{Width: 200, Height: 40})

	if b.HandleMouseMove(core.MouseMoveEvent{X: 50, Y: 0}) {
		t.Error("hover move should not be consumed")
	}
	if !b.mouseOver {
		t.Error("pointer inside the button did not set hover")
	}

	b.HandleMouseMove(core.MouseMoveEvent{X: -5, Y: 0})
	if b.mouseOver {
		t.Error("pointer outside the button did not clear hover")
	}
}

// A move with a button held (a drag begun elsewhere passing over) must not
// set hover, and must clear hover set before the button went down.
func TestButtonHoverSuppressedWhileButtonHeld(t *testing.T) {
	b := NewButton("ok")
	b.SetBounds(core.UnitRect{Width: 200, Height: 40})

	// Hover set on a plain move.
	b.HandleMouseMove(core.MouseMoveEvent{X: 50, Y: 0})
	if !b.mouseOver {
		t.Fatal("plain move should set hover")
	}
	// A held-button move over the same spot clears it.
	b.HandleMouseMove(core.MouseMoveEvent{X: 50, Y: 0, Buttons: core.LeftButton})
	if b.mouseOver {
		t.Error("held-button move should not keep hover")
	}
}

// The dock highlights the entry under the pointer and clears it when the
// pointer moves to background.
func TestDockItemHover(t *testing.T) {
	d := NewDockRow()
	d.SetBounds(core.UnitRect{Width: 400, Height: 40})
	d.AddEntry(&DockEntry{Title: "one"})
	d.AddEntry(&DockEntry{Title: "two"})

	metrics := d.EffectiveCellMetrics()
	slot := core.Unit(d.entryWidth) * metrics.CellWidth

	// Middle of the second slot.
	d.HandleMouseMove(core.MouseMoveEvent{X: slot + slot/2, Y: 0})
	if d.hoverIndex != 1 {
		t.Errorf("hoverIndex = %d, want 1", d.hoverIndex)
	}

	// Below the row: no entry.
	d.HandleMouseMove(core.MouseMoveEvent{X: slot + slot/2, Y: metrics.CellHeight + 1})
	if d.hoverIndex != -1 {
		t.Errorf("hoverIndex = %d after leaving, want -1", d.hoverIndex)
	}
}

// The splitter tracks hover over its divider band so the grab handle can
// light up before a drag.
func TestSplitterDividerHover(t *testing.T) {
	sp := NewSplitter(core.Horizontal)
	sp.SetBounds(core.UnitRect{Width: 300, Height: 200})

	divider := sp.dividerBounds()
	sp.HandleMouseMove(core.MouseMoveEvent{X: divider.X + divider.Width/2, Y: 100})
	if !sp.hoveringDivider {
		t.Error("pointer over the divider did not set hover")
	}

	// Far to the left, inside the first pane, not the divider.
	sp.HandleMouseMove(core.MouseMoveEvent{X: 0, Y: 100})
	if sp.hoveringDivider {
		t.Error("pointer off the divider did not clear hover")
	}
}

// A widget in a splitter pane must not hover when the pointer is on the
// divider itself (the splitter chrome wins), nor when the pointer is in the
// other pane (obscured/clipped content must not light up).
func TestSplitterDividerSuppressesChildHover(t *testing.T) {
	sp := NewSplitter(core.Horizontal)
	sp.SetBounds(core.UnitRect{Width: 300, Height: 200})

	btn := NewButton("ok")
	sp.SetFirst(btn)
	sp.SetSecond(NewButton("other"))
	sp.Layout()

	firstBounds, secondBounds := sp.childBounds()
	// Grow the first button so it overhangs its pane (its full bounds reach
	// past the divider), mimicking clipped content.
	btn.SetBounds(core.UnitRect{Width: 300, Height: 40})

	// Over the first pane: the button hovers.
	sp.HandleMouseMove(core.MouseMoveEvent{X: firstBounds.X + 10, Y: 8})
	if !btn.mouseOver {
		t.Fatal("button did not hover when the pointer was over its pane")
	}

	// On the divider: the button must clear even though its bounds overhang
	// under the divider.
	div := sp.dividerBounds()
	sp.HandleMouseMove(core.MouseMoveEvent{X: div.X + div.Width/2, Y: 8})
	if btn.mouseOver {
		t.Error("button hovered while the pointer was on the divider")
	}

	// In the second pane: the (clipped) first button must not hover.
	sp.HandleMouseMove(core.MouseMoveEvent{X: secondBounds.X + 10, Y: 8})
	if btn.mouseOver {
		t.Error("clipped first-pane button hovered from the second pane")
	}
}

// The menu bar highlights the top-level item under the pointer even when
// no dropdown is open.
func TestMenuBarItemHover(t *testing.T) {
	m := NewMenuBar()
	m.SetBounds(core.UnitRect{Width: 400, Height: 30})
	m.AddMenu(NewMenu("File"))
	m.AddMenu(NewMenu("Edit"))

	// Somewhere well inside the first item's title.
	metrics := m.EffectiveCellMetrics()
	m.HandleMouseMove(core.MouseMoveEvent{X: m.leftInset() + metrics.CellWidth, Y: 0})
	if m.hoverIndex != 0 {
		t.Errorf("hoverIndex = %d, want 0", m.hoverIndex)
	}

	// Below the bar row: cleared.
	m.HandleMouseMove(core.MouseMoveEvent{X: m.leftInset() + metrics.CellWidth, Y: metrics.CellHeight + 1})
	if m.hoverIndex != -1 {
		t.Errorf("hoverIndex = %d after leaving the bar, want -1", m.hoverIndex)
	}
}

// With a dropdown already open, hovering (button up) over a different
// top-level menu drops that one down instead of merely highlighting it.
func TestMenuBarHoverSwitchesOpenMenu(t *testing.T) {
	m := NewMenuBar()
	m.SetBounds(core.UnitRect{Width: 400, Height: 30})
	file := NewMenu("File")
	file.AddItem(NewMenuItem("New"))
	edit := NewMenu("Edit")
	edit.AddItem(NewMenuItem("Copy"))
	m.AddMenu(file)
	m.AddMenu(edit)

	m.OpenMenu(0)
	if m.ActiveMenu() != file {
		t.Fatalf("precondition: File menu should be open")
	}

	// Hover (no button held) over the Edit title.
	editX := m.leftInset() + m.menuTitleWidth("File") + m.menuTitleWidth("Edit")/2
	m.HandleMouseMove(core.MouseMoveEvent{X: editX, Y: 0})
	if m.ActiveMenu() != edit {
		t.Errorf("hovering Edit with File open should drop down Edit, got %v", m.ActiveMenu())
	}

	// Hovering back over the same open menu doesn't churn it closed/reopened.
	fileX := m.leftInset() + m.menuTitleWidth("File")/2
	m.HandleMouseMove(core.MouseMoveEvent{X: fileX, Y: 0})
	if m.ActiveMenu() != file {
		t.Errorf("hovering File should switch back to File, got %v", m.ActiveMenu())
	}
}
