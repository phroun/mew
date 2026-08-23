package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Modifier prefixes are carried in the key string, and the canonical order
// puts them in one arrangement whatever the backend - "M-S-Left" is Mega+Shift.
// The keymap turns that whole string into window_size_left, a resize (Shift)
// that is chunky (Mega), which is what this checks.
func TestTitleKeyModifierOrderResize(t *testing.T) {
	start := core.UnitRect{X: 100, Y: 100, Width: 200, Height: 100}

	// Mega+Shift arrives as "M-S-Left": a resize (Shift) that is chunky (Mega).
	chunky := NewWindow("chunky")
	chunky.SetBounds(start)
	chunky.SetTitleFocus(TitleFocusTitle)
	titleKey(chunky, "M-S-Left")
	got := chunky.Bounds()
	if got == start {
		t.Fatalf("M-S-Left did not resize the window (start %v)", start)
	}
	if got.Width <= start.Width {
		t.Errorf("M-S-Left should expand the left edge (grow width): %v -> %v", start, got)
	}

	// A plain single-step "S-Left" grows by one cell; the Mega-prefixed form
	// must grow by more, proving the Mega prefix registered as chunky.
	single := NewWindow("single")
	single.SetBounds(start)
	single.SetTitleFocus(TitleFocusTitle)
	titleKey(single, "S-Left")
	singleDelta := single.Bounds().Width - start.Width
	chunkyDelta := got.Width - start.Width
	if singleDelta <= 0 {
		t.Fatalf("S-Left did not resize (delta %d)", singleDelta)
	}
	if chunkyDelta <= singleDelta {
		t.Errorf("Mega prefix should make the resize chunky: chunky=%d single=%d", chunkyDelta, singleDelta)
	}
}

// A keyboard resize of a maximized window can only make it smaller, so
// the first Shift+arrow snaps it off the maximized state IN PLACE (the
// full-screen bounds become the floating size) and pulls in the edge
// opposite the arrow - Shift+Left narrows from the right.
func TestMaximizedKeyboardResizeShrinksInPlace(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 600})
	w := NewWindow("w")
	m.AddWindow(w)
	normal := core.UnitRect{X: 50, Y: 50, Width: 200, Height: 100}
	w.SetBounds(normal)
	m.MaximizeWindow(w)
	if !w.IsMaximized() {
		t.Fatal("precondition: window should be maximized")
	}
	maxed := w.Bounds()
	w.SetTitleFocus(TitleFocusTitle)

	titleKey(w, "S-Left")

	if w.IsMaximized() {
		t.Error("Shift+Left should snap the window off maximized")
	}
	got := w.Bounds()
	if got.Width >= maxed.Width {
		t.Errorf("width should shrink from the maximized size: %d -> %d", maxed.Width, got.Width)
	}
	if got.Width <= normal.Width {
		t.Errorf("should shrink from the maximized size, not jump back to the pre-maximize size: got %d, normal %d, maxed %d", got.Width, normal.Width, maxed.Width)
	}
	if got.X != maxed.X || got.Y != maxed.Y || got.Height != maxed.Height {
		t.Errorf("only the right edge should move: %v (was %v)", got, maxed)
	}
}

// A keyboard MOVE of a maximized window restores it to the pre-maximize
// size (like dragging its titlebar off the top) and then moves.
func TestMaximizedKeyboardMoveRestores(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 600})
	w := NewWindow("w")
	m.AddWindow(w)
	normal := core.UnitRect{X: 50, Y: 50, Width: 200, Height: 100}
	w.SetBounds(normal)
	m.MaximizeWindow(w)
	w.SetTitleFocus(TitleFocusTitle)

	titleKey(w, "Left")

	if w.IsMaximized() {
		t.Error("plain-arrow move should snap the window off maximized")
	}
	if got := w.Bounds(); got.Width != normal.Width || got.Height != normal.Height {
		t.Errorf("move should restore to the pre-maximize size: got %v, normal %v", got, normal)
	}
}

// Pressing Up while a window is already pressed against the top of the
// client area snaps it maximized - the keyboard equivalent of dragging
// the titlebar up into the menu bar. A window below the top just moves.
func TestKeyboardTopSnapMaximize(t *testing.T) {
	m := NewWindowManager()
	m.SetScreenBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 600})

	atTop := NewWindow("top")
	m.AddWindow(atTop)
	atTop.SetBounds(core.UnitRect{X: 100, Y: 0, Width: 200, Height: 100})
	atTop.SetTitleFocus(TitleFocusTitle)
	titleKey(atTop, "Up")
	if !atTop.IsMaximized() {
		t.Error("Up at the top edge should snap-maximize")
	}

	mid := NewWindow("mid")
	m.AddWindow(mid)
	mid.SetBounds(core.UnitRect{X: 100, Y: 200, Width: 200, Height: 100})
	mid.SetTitleFocus(TitleFocusTitle)
	titleKey(mid, "Up")
	if mid.IsMaximized() {
		t.Error("Up from mid-screen should move, not maximize")
	}
	if mid.Bounds().Y >= 200 {
		t.Errorf("Up from mid-screen should have moved the window up: Y=%d", mid.Bounds().Y)
	}
}
