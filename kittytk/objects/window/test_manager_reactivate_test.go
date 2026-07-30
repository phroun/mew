package window

import "testing"

// A window can be the manager's active window yet visually inactive - e.g. a
// torn window took surface focus and the in-surface active window was
// SetActive(false)'d. Clicking it must re-activate it (restore its active
// look), not early-return just because it is still m.activeWindow.
func TestActivateReactivatesInactiveTopWindow(t *testing.T) {
	m := NewWindowManager()
	a := NewWindow("A")
	b := NewWindow("B")
	m.AddWindow(a)
	m.AddWindow(b)

	// b is on top and active after AddWindow.
	m.ActivateWindow(b)
	if m.ActiveWindow() != b || !b.IsActive() {
		t.Fatalf("precondition: b should be active")
	}

	// Simulate a torn window stealing surface focus: b stays m.activeWindow
	// but is visually deactivated.
	b.SetActive(false)
	if b.IsActive() {
		t.Fatal("precondition: b should be visually inactive now")
	}

	// Clicking b again must re-activate it even though it is still the
	// manager's active window.
	m.ActivateWindow(b)
	if !b.IsActive() {
		t.Error("clicking a topmost-but-inactive window should re-activate it")
	}
	if m.ActiveWindow() != b {
		t.Errorf("active window = %v, want b", m.ActiveWindow())
	}
}

// FocusWindow has the same guard: it must re-focus a window that is
// m.activeWindow but no longer visually active.
func TestFocusReactivatesInactiveTopWindow(t *testing.T) {
	m := NewWindowManager()
	a := NewWindow("A")
	b := NewWindow("B")
	m.AddWindow(a)
	m.AddWindow(b)
	m.ActivateWindow(b)

	b.SetActive(false)
	m.FocusWindow(b)
	if !b.IsActive() {
		t.Error("FocusWindow should re-activate a topmost-but-inactive window")
	}
}
