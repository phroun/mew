package window

import "testing"

// AddOnClosed observers fire on Close even after the single onCloseComplete
// slot has been reassigned (as the manager and tear-off host do), so the
// owning Application always learns a window closed and can drop it from its
// list.
func TestCloseObserversSurviveReassignment(t *testing.T) {
	w := NewWindow("dialog")

	var managerRemoved, appForgot bool
	// Manager wiring at AddWindow time.
	w.SetOnCloseComplete(func() { managerRemoved = true })
	// Application observer.
	w.AddOnClosed(func() { appForgot = true })
	// Tear-off host later reassigns the single slot for its own cleanup.
	tearoffRan := false
	w.SetOnCloseComplete(func() { tearoffRan = true })

	if !w.Close() {
		t.Fatal("Close returned false")
	}
	if managerRemoved {
		t.Error("the reassigned slot should have replaced the manager handler")
	}
	if !tearoffRan {
		t.Error("the current onCloseComplete (tear-off) should run")
	}
	if !appForgot {
		t.Error("the app observer must still run after slot reassignment")
	}
}
