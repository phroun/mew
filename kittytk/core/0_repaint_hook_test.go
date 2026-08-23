package core

import "testing"

// Update() fires the repaint hook so the host can coalesce "a frame is needed"
// and skip work when nothing changed; clearing the hook stops the callbacks.
func TestRepaintHookFiresOnUpdate(t *testing.T) {
	var fired int
	SetRepaintHook(func() { fired++ })
	defer SetRepaintHook(nil)

	b := NewTrinketBase()
	b.Update()
	b.Update()
	if fired != 2 {
		t.Fatalf("repaint hook fired %d times, want 2", fired)
	}

	SetRepaintHook(nil)
	b.Update()
	if fired != 2 {
		t.Errorf("hook fired %d times after being cleared, want 2", fired)
	}
}
