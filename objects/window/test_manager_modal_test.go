package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A window in the manager is modally blocked when a modal sits above it: any
// modal blocks a non-modal window, and a later modal blocks an earlier one.
// The top modal is never blocked, and closing a modal unblocks down the stack.
func TestModalStackBlocks(t *testing.T) {
	m := NewWindowManager()
	a := NewWindow("A")
	b := NewWindow("B")
	m.AddWindow(a)
	m.AddWindow(b)

	if m.isModalBlocked(a) || m.isModalBlocked(b) {
		t.Fatal("no modal yet: nothing should be blocked")
	}

	modal := NewWindow("Modal")
	m.ShowModal(modal)
	if !m.isModalBlocked(a) || !m.isModalBlocked(b) {
		t.Error("a and b should be blocked by the modal")
	}
	if m.isModalBlocked(modal) {
		t.Error("the top modal must not be blocked")
	}

	// A second modal blocks the first.
	modal2 := NewWindow("Modal2")
	m.ShowModal(modal2)
	if !m.isModalBlocked(modal) {
		t.Error("a lower modal must be blocked by a later one")
	}
	if m.isModalBlocked(modal2) {
		t.Error("the top modal must not be blocked")
	}

	// Closing the top modal makes the previous one top (unblocked) again.
	m.CloseModal()
	if m.isModalBlocked(modal) {
		t.Error("modal is top again after CloseModal, must not be blocked")
	}
	if !m.isModalBlocked(a) {
		t.Error("a stays blocked while any modal remains")
	}

	// Closing the last modal unblocks everything.
	m.CloseModal()
	if m.isModalBlocked(a) || m.isModalBlocked(b) {
		t.Error("no modals left: nothing should be blocked")
	}
}

// A detached (torn-off) window lives on its own surface and is never blocked
// by the desktop manager's modal stack.
func TestDetachedWindowNotModalBlocked(t *testing.T) {
	m := NewWindowManager()
	torn := NewWindow("Torn")
	torn.SetDetached(true)
	m.AddWindow(torn)
	m.ShowModal(NewWindow("Modal"))

	if m.isModalBlocked(torn) {
		t.Error("a detached window must not be blocked by the desktop modal stack")
	}
}

// An application-level modal (owner unset, appID set) blocks every window of
// the same application, but not windows of another application.
func TestAppModalBlocksOnlySameApp(t *testing.T) {
	m := NewWindowManager()
	a1 := NewWindow("a1")
	a1.SetAppID(1)
	a2 := NewWindow("a2")
	a2.SetAppID(1)
	b1 := NewWindow("b1")
	b1.SetAppID(2)
	m.AddWindow(a1)
	m.AddWindow(a2)
	m.AddWindow(b1)

	modalA := NewWindow("modalA")
	modalA.SetType(WindowTypeModal)
	modalA.SetAppID(1)
	m.AddWindow(modalA) // application modal for app 1

	if !m.isModalBlocked(a1) || !m.isModalBlocked(a2) {
		t.Error("app 1 windows should be blocked by app 1's modal")
	}
	if m.isModalBlocked(modalA) {
		t.Error("the app modal itself must not be blocked")
	}
	if m.isModalBlocked(b1) {
		t.Error("app 2's window must not be blocked by app 1's modal")
	}
}

// A window-level modal (owner set) blocks its owner's group only - the owner,
// its descendants, and its owned overlays - not unrelated windows of the same
// application.
func TestWindowModalBlocksOwnerGroupOnly(t *testing.T) {
	m := NewWindowManager()
	base := NewWindow("base")
	base.SetAppID(1)
	dlg := NewWindow("dlg")
	dlg.SetType(WindowTypeDialog)
	dlg.SetAppID(1)
	other := NewWindow("other")
	other.SetAppID(1)
	m.AddWindow(base)
	m.AddWindow(dlg)
	m.AddWindow(other)
	dlg.SetOwner(base)

	modal := NewWindow("modal")
	modal.SetType(WindowTypeModal)
	modal.SetAppID(1)
	modal.SetOwner(base)
	m.AddWindow(modal) // window modal owned by base

	if !m.isModalBlocked(base) {
		t.Error("the owner window should be blocked by its window modal")
	}
	if !m.isModalBlocked(dlg) {
		t.Error("the owner's dialog should be blocked by the window modal")
	}
	if m.isModalBlocked(modal) {
		t.Error("the window modal itself must not be blocked")
	}
	if m.isModalBlocked(other) {
		t.Error("an unrelated same-app window must not be blocked by a window-level modal")
	}
}

// An application modal keeps blocking its app across surfaces: after the modal
// is torn off (removed from the manager's window list), it still blocks the
// app's in-surface windows and any torn same-app window, until it is closed.
func TestAppModalSurvivesTearOff(t *testing.T) {
	m := NewWindowManager()
	a1 := NewWindow("a1")
	a1.SetAppID(1)
	m.AddWindow(a1)

	modal := NewWindow("modal")
	modal.SetType(WindowTypeModal)
	modal.SetAppID(1)
	m.AddWindow(modal)
	if !m.isModalBlocked(a1) {
		t.Fatal("precondition: a1 should be blocked by the app modal")
	}

	// Tear the modal off: it leaves the manager's window list.
	m.RemoveWindow(modal)
	modal.SetDetached(true)

	if !m.isModalBlocked(a1) {
		t.Error("the app modal must keep blocking the app's in-surface window after tear-off")
	}
	a2 := NewWindow("a2")
	a2.SetAppID(1)
	a2.SetDetached(true)
	if !m.IsTornWindowBlocked(a2) {
		t.Error("a torn same-app window must be blocked by the app modal")
	}
	if m.IsTornWindowBlocked(modal) {
		t.Error("the modal itself must not be blocked")
	}
	b := NewWindow("b")
	b.SetAppID(2)
	b.SetDetached(true)
	if m.IsTornWindowBlocked(b) {
		t.Error("a torn window of another app must not be blocked")
	}

	// Closing the modal unregisters it via the close observer.
	modal.Close()
	if m.isModalBlocked(a1) {
		t.Error("closing the modal must unblock the app")
	}
}

// Adding a window to the desktop while a modal is up must leave the modal on
// top with focus, not the newly added window.
func TestAddWindowKeepsModalOnTop(t *testing.T) {
	m := NewWindowManager()
	modal := NewWindow("Modal")
	m.ShowModal(modal)

	later := NewWindow("Later")
	m.AddWindow(later)

	if m.ActiveWindow() != modal {
		t.Errorf("active window = %v, want the modal to stay on top", m.ActiveWindow())
	}
}

// Adding the modal itself (the ShowModal path) must not demote it: the raise
// is exempt when the added window is the top modal.
func TestShowModalActivatesTheModal(t *testing.T) {
	m := NewWindowManager()
	base := NewWindow("Base")
	m.AddWindow(base)

	modal := NewWindow("Modal")
	m.ShowModal(modal)

	if m.ActiveWindow() != modal {
		t.Errorf("active window = %v, want the freshly shown modal", m.ActiveWindow())
	}
}

// A click on a modally-blocked window or the wallpaper restores the top modal
// when it is minimized, firing the restore callback (dock removal) just like a
// dock-item click.
func TestRestoreMinimizedTopModal(t *testing.T) {
	m := NewWindowManager()
	modal := NewWindow("Modal")
	m.ShowModal(modal)

	restored := 0
	m.SetOnWindowRestored(func(*Window) { restored++ })
	m.MinimizeWindow(modal)
	if !modal.IsMinimized() {
		t.Fatal("modal should be minimized")
	}

	if !m.restoreMinimizedTopModal() {
		t.Fatal("restoreMinimizedTopModal should report it restored the modal")
	}
	if modal.IsMinimized() {
		t.Error("modal should be restored (not minimized)")
	}
	if restored != 1 {
		t.Errorf("restore callback fired %d times, want 1", restored)
	}

	// No modal minimized now: nothing to restore.
	if m.restoreMinimizedTopModal() {
		t.Error("restoreMinimizedTopModal should be a no-op when the modal is not minimized")
	}
}

// RaiseTopModalOver does nothing while the top modal is minimized - a click,
// not an automatic raise, is what surfaces a minimized modal.
func TestRaiseTopModalOverSkipsMinimized(t *testing.T) {
	m := NewWindowManager()
	modal := NewWindow("Modal")
	m.ShowModal(modal)
	m.MinimizeWindow(modal)

	later := NewWindow("Later")
	m.AddWindow(later)

	if !modal.IsMinimized() {
		t.Error("a minimized modal must not be auto-restored when a new window is added")
	}
}

// The wallpaper dim (and wallpaper-click surface) applies only when the
// desktop itself is blocked: a system modal, or a modal owned by the app whose
// menu bar is showing. A modal in a background app must not shade the wallpaper.
func TestWallpaperModalActiveScopedToActiveApp(t *testing.T) {
	m := NewWindowManager()
	m.SetActiveAppIDFunc(func() core.ObjectID { return core.ObjectID(1) })

	// A background app (2) has a modal; the active app (1) does not.
	a2mod := NewWindow("a2mod")
	a2mod.SetType(WindowTypeModal)
	a2mod.SetAppID(2)
	m.AddWindow(a2mod)
	if m.wallpaperModalActive() {
		t.Error("a background app's modal must not shade the wallpaper")
	}

	// The active app (1) now has a modal: the wallpaper is shaded.
	a1mod := NewWindow("a1mod")
	a1mod.SetType(WindowTypeModal)
	a1mod.SetAppID(1)
	m.AddWindow(a1mod)
	if !m.wallpaperModalActive() {
		t.Error("the active app's modal should shade the wallpaper")
	}
}

// A system modal always shades the wallpaper, regardless of the active app.
func TestWallpaperModalActiveSystemModal(t *testing.T) {
	m := NewWindowManager()
	m.SetActiveAppIDFunc(func() core.ObjectID { return core.ObjectID(1) })
	m.ShowModal(NewWindow("sys")) // system modal (no app)
	if !m.wallpaperModalActive() {
		t.Error("a system modal should shade the wallpaper")
	}
}
