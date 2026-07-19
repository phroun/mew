package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// closeButtonCenter returns the manager-space point at the center of a
// window's close button ([x]) on a normal frame.
func closeButtonCenter(w *Window) (core.Unit, core.Unit) {
	metrics := w.frameCellMetrics()
	inset := core.FindFrameBorderUnits(w)
	b := w.Bounds()
	x := b.X + inset + metrics.CellWidth + metrics.CellWidth/2
	y := b.Y + inset + metrics.CellHeight/2
	return x, y
}

// The manager forwards plain hover moves to the topmost window under the
// pointer, so a window highlights its titlebar button even when it isn't
// the active window; the highlight does not leak to other windows.
func TestManagerHoverOnInactiveWindow(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 1000, Height: 700})

	w1 := NewWindow("one")
	w1.SetBounds(core.UnitRect{X: 40, Y: 40, Width: 200, Height: 120})
	m.AddWindow(w1)

	w2 := NewWindow("two")
	w2.SetBounds(core.UnitRect{X: 400, Y: 300, Width: 200, Height: 120})
	m.AddWindow(w2) // w2 becomes active; w1 is now inactive

	x, y := closeButtonCenter(w1)
	m.HandleMouseMove(core.MouseMoveEvent{X: x, Y: y})

	if w1.hoveredButton != TitleButtonClose {
		t.Errorf("inactive window close hover = %v, want close", w1.hoveredButton)
	}
	if w2.hoveredButton != TitleButtonNone {
		t.Errorf("other window hover = %v, want none (no pass-through)", w2.hoveredButton)
	}

	// Move the pointer to the other window: the first clears, the second
	// lights up.
	x2, y2 := closeButtonCenter(w2)
	m.HandleMouseMove(core.MouseMoveEvent{X: x2, Y: y2})
	if w1.hoveredButton != TitleButtonNone {
		t.Errorf("first window hover = %v after leaving, want none", w1.hoveredButton)
	}
	if w2.hoveredButton != TitleButtonClose {
		t.Errorf("second window close hover = %v, want close", w2.hoveredButton)
	}
}

// While a window is being dragged, nothing keeps a hover highlight.
func TestManagerHoverClearedDuringDrag(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 1000, Height: 700})

	w := NewWindow("w")
	w.SetBounds(core.UnitRect{X: 40, Y: 40, Width: 200, Height: 120})
	m.AddWindow(w)

	x, y := closeButtonCenter(w)
	m.HandleMouseMove(core.MouseMoveEvent{X: x, Y: y})
	if w.hoveredButton != TitleButtonClose {
		t.Fatalf("precondition: close hover = %v, want close", w.hoveredButton)
	}

	// Simulate an in-progress window drag, then move: the hover clears.
	m.mu.Lock()
	m.dragging = w
	m.mu.Unlock()
	m.HandleMouseMove(core.MouseMoveEvent{X: x + 5, Y: y + 5, Buttons: core.LeftButton})

	if w.hoveredButton != TitleButtonNone {
		t.Errorf("hover = %v during drag, want none", w.hoveredButton)
	}
}
