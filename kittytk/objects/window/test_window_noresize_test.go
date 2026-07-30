package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A window without resize capability can't be maximized (maximizing is a
// resize), its maximize button is dropped from the title focus order, and
// the window manager's MaximizeWindow leaves it untouched.
func TestNoResizeWindowCannotMaximize(t *testing.T) {
	w := NewWindow("dlg")
	w.SetFlags(WindowFlagNoResize)

	w.Maximize()
	if w.IsMaximized() {
		t.Error("NoResize window should not maximize")
	}

	// Title focus order skips the maximize button.
	if got := w.nextTitleFocus(TitleFocusMinimize); got == TitleFocusMaximize {
		t.Error("focus order should skip the maximize button for a NoResize window")
	}

	// The window manager's snap/double-click path is a no-op too.
	m := NewWindowManager()
	w.SetBounds(core.UnitRect{X: 10, Y: 10, Width: 200, Height: 100})
	m.AddWindow(w)
	start := w.Bounds()
	m.MaximizeWindow(w)
	if w.IsMaximized() {
		t.Error("MaximizeWindow should not maximize a NoResize window")
	}
	if w.Bounds() != start {
		t.Errorf("MaximizeWindow resized a NoResize window: %v -> %v", start, w.Bounds())
	}
}

// A NoResize window ignores keyboard resize (Shift is the resize
// modifier) but still moves with plain / Meta arrows.
func TestNoResizeWindowKeyboardMoveNotResize(t *testing.T) {
	w := NewWindow("dlg")
	w.SetFlags(WindowFlagNoResize)
	w.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 200, Height: 100})
	w.SetTitleFocus(TitleFocusTitle)

	start := w.Bounds()

	// Shift+arrow (resize) is ignored.
	w.handleTitleBarKey(core.KeyPressEvent{Key: "Right", Modifiers: core.ShiftModifier})
	if w.Bounds() != start {
		t.Errorf("Shift+arrow resized a NoResize window: %v", w.Bounds())
	}
	// Meta+Shift+arrow (large resize) is ignored too.
	w.handleTitleBarKey(core.KeyPressEvent{Key: "Down", Modifiers: core.ShiftModifier | core.MetaModifier})
	if w.Bounds() != start {
		t.Errorf("Meta+Shift+arrow resized a NoResize window: %v", w.Bounds())
	}

	// A plain arrow still moves it, without changing its size.
	w.handleTitleBarKey(core.KeyPressEvent{Key: "Right"})
	moved := w.Bounds()
	if moved == start {
		t.Error("plain arrow did not move the NoResize window")
	}
	if moved.Width != start.Width || moved.Height != start.Height {
		t.Errorf("move changed the window size: %v -> %v", start, moved)
	}
}
