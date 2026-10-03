package trinkets

// When a window closes, the focus goes back to the window that held it before
// -- its owner if it has one, otherwise the most recent window in the focus
// history that is still there -- whichever surface either of them lives on.
// Neither the solo host's primary window, nor the newest torn window, nor the
// topmost window on the desktop's own surface is that.

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// raisingSurface records which surfaces were raised, in order.
type raisingSurface struct {
	fakeNativeSurface
	name   string
	raised *[]string
}

func (s *raisingSurface) Raise() { *s.raised = append(*s.raised, s.name) }

type focusRig struct {
	d      *Desktop
	raised []string
}

func newFocusRig(t *testing.T) *focusRig {
	t.Helper()
	r := &focusRig{d: NewDesktop()}
	r.d.SetBackend(&nullBackend{})
	return r
}

// torn puts a window on a surface of its own and gives it the focus.
func (r *focusRig) torn(title string) *window.Window {
	w := window.NewWindow(title)
	w.SetDetached(true)
	s := &raisingSurface{fakeNativeSurface: fakeNativeSurface{pxW: 400, pxH: 300}, name: title, raised: &r.raised}
	h := window.NewTearOffHost(w, s, ppu1, func() (int, int) { return 0, 0 }, nil)
	r.d.mu.Lock()
	r.d.tornHosts = append(r.d.tornHosts, h)
	r.d.mu.Unlock()
	r.d.windowFocusChanged(w)
	return w
}

// docked puts a window on the desktop's own surface and activates it.
func (r *focusRig) docked(title string) *window.Window {
	w := window.NewWindow(title)
	w.SetBounds(core.UnitRect{Width: 160, Height: 96})
	r.d.windowManager.AddWindow(w)
	r.d.windowManager.ActivateWindow(w)
	return w
}

func (r *focusRig) lastRaised() string {
	if len(r.raised) == 0 {
		return ""
	}
	return r.raised[len(r.raised)-1]
}

// The case that was wrong: an application's dialog, on a surface of its own,
// closes and the solo host's primary window came forward instead of the
// application's window the dialog was asked for from.
func TestATornDialogGivesTheFocusBackToWhereItCameFrom(t *testing.T) {
	r := newFocusRig(t)
	mew := r.torn("mew")
	primary := r.d.tornHostForWindow(mew)
	r.d.mu.Lock()
	r.d.soloPrimaryHost = primary
	r.d.mu.Unlock()
	demo := r.torn("demo")
	r.torn("other") // opened later, and not the one the person was in
	r.d.windowFocusChanged(demo)
	dialog := r.torn("dialog")

	dialog.Close()
	r.d.refocusAfterTornClose(dialog)

	if got := r.lastRaised(); got != "demo" {
		t.Errorf("closing the dialog raised %q, want demo", got)
	}
	if r.d.tornFocusOwner != demo {
		t.Errorf("the focus went to %v, want demo", r.d.tornFocusOwner)
	}
}

// A window that has gone, or is minimized, is passed over for the one before.
func TestTheFocusPassesOverAWindowThatCannotTakeIt(t *testing.T) {
	r := newFocusRig(t)
	r.torn("first")
	gone := r.torn("gone")
	shelved := r.torn("shelved")
	dialog := r.torn("dialog")

	gone.Close()
	shelved.Minimize()
	dialog.Close()
	r.d.refocusAfterTornClose(dialog)

	if got := r.lastRaised(); got != "first" {
		t.Errorf("closing the dialog raised %q, want first", got)
	}
}

// An owner is where its dialog came from, whatever was focused in between.
func TestAnOwnedDialogGoesBackToItsOwner(t *testing.T) {
	r := newFocusRig(t)
	owner := r.torn("owner")
	r.torn("elsewhere")
	dialog := r.torn("dialog")
	dialog.SetOwner(owner)

	dialog.Close()
	r.d.refocusAfterTornClose(dialog)

	if got := r.lastRaised(); got != "owner" {
		t.Errorf("closing an owned dialog raised %q, want its owner", got)
	}
}

// On the desktop's own surface, the window that held the focus is not always
// the topmost: here Beta was raised without being focused.
func TestADockedWindowGivesTheFocusBackByHistoryNotZOrder(t *testing.T) {
	r := newFocusRig(t)
	wm := r.d.windowManager
	alpha := r.docked("alpha")
	beta := r.docked("beta")
	wm.ActivateWindow(alpha)
	wm.RaiseWindow(beta)
	dialog := r.docked("dialog")

	dialog.Close()

	if got := wm.ActiveWindow(); got != alpha {
		t.Errorf("closing the dialog activated %v, want alpha", got)
	}
	if ws := wm.Windows(); ws[len(ws)-1] != alpha {
		t.Errorf("alpha was activated but left under %v", ws[len(ws)-1])
	}
}

// A docked dialog asked for from a torn window gives the focus back to the
// torn window, and leaves nothing on the desktop's surface lit in its place.
func TestADockedDialogGivesTheFocusBackToATornWindow(t *testing.T) {
	r := newFocusRig(t)
	wm := r.d.windowManager
	r.docked("background")
	torn := r.torn("torn")
	dialog := r.docked("dialog")

	dialog.Close()

	if got := wm.ActiveWindow(); got != nil {
		t.Errorf("closing the dialog activated %v on the desktop's surface, want nothing", got)
	}
	if got := r.lastRaised(); got != "torn" {
		t.Errorf("closing the dialog raised %q, want torn", got)
	}
	if r.d.tornFocusOwner != torn {
		t.Errorf("the focus went to %v, want torn", r.d.tornFocusOwner)
	}
}

// Tearing a window off removes it from the manager too, and is not a close:
// the torn window keeps the focus, and nothing is chosen.
func TestTearingOffIsNotAClose(t *testing.T) {
	r := newFocusRig(t)
	wm := r.d.windowManager
	r.docked("background")
	leaving := r.docked("leaving")
	asked := 0
	wm.SetNextActiveChooser(func(*window.Window) *window.Window { asked++; return nil })
	wm.RemoveWindow(leaving)
	if asked != 0 {
		t.Errorf("removing a window that did not close asked for the next one %d times", asked)
	}
}

// A torn dialog asked for from a docked window gives the focus back to it,
// lit again: the desktop's surface went dark while the dialog had the focus.
func TestATornDialogGivesTheFocusBackToADockedWindow(t *testing.T) {
	r := newFocusRig(t)
	wm := r.d.windowManager
	docked := r.docked("docked")
	r.docked("above")
	wm.ActivateWindow(docked)
	docked.SetActive(false) // the desktop's surface lost the focus to the dialog
	dialog := r.torn("dialog")

	dialog.Close()
	r.d.refocusAfterTornClose(dialog)

	if wm.ActiveWindow() != docked || !docked.IsActive() {
		t.Errorf("closing the dialog left %v active (docked lit: %v), want docked", wm.ActiveWindow(), docked.IsActive())
	}
}

// The window the focus would go back to is blocked by a modal still open: the
// focus goes to the modal, where the person can act, and not to the window it
// blocks.
func TestTheFocusGivesWayToAModalStillOpen(t *testing.T) {
	r := newFocusRig(t)
	r.d.surface = &raisingSurface{name: "desktop", raised: &r.raised}
	wm := r.d.windowManager
	owner := r.torn("owner")
	modal := window.NewWindow("modal")
	modal.SetType(window.WindowTypeModal)
	modal.SetOwner(owner)
	modal.SetBounds(core.UnitRect{Width: 160, Height: 96})
	wm.AddWindow(modal)
	r.d.windowFocusChanged(owner) // the owner was the last to hold the focus
	dialog := r.torn("dialog")

	dialog.Close()
	r.d.refocusAfterTornClose(dialog)

	if got := r.lastRaised(); got != "desktop" {
		t.Errorf("closing the dialog raised %q, want the desktop, where the modal is", got)
	}
	if got := wm.ActiveWindow(); got != modal {
		t.Errorf("closing the dialog activated %v, want the modal blocking owner", got)
	}
}

// A modal closing over its owner is still on the modal stack while the focus
// is being given back, and must not be mistaken for what blocks the owner.
func TestAModalGivesTheFocusBackToTheWindowItBlocked(t *testing.T) {
	r := newFocusRig(t)
	wm := r.d.windowManager
	owner := r.docked("owner")
	above := r.docked("above")
	wm.ActivateWindow(owner)
	wm.RaiseWindow(above)
	modal := window.NewWindow("modal")
	modal.SetType(window.WindowTypeModal)
	modal.SetOwner(owner)
	modal.SetBounds(core.UnitRect{Width: 160, Height: 96})
	wm.AddWindow(modal)
	wm.ActivateWindow(modal)

	modal.Close()

	if got := wm.ActiveWindow(); got != owner {
		t.Errorf("closing the modal activated %v, want owner", got)
	}
}

// Two modals stacked over one owner: closing the top one gives the focus to
// the one under it, which still blocks the owner, and not to the owner.
func TestAModalGivesTheFocusToTheModalUnderIt(t *testing.T) {
	r := newFocusRig(t)
	wm := r.d.windowManager
	owner := r.docked("owner")
	wm.ActivateWindow(owner)
	modal := func(title string) *window.Window {
		m := window.NewWindow(title)
		m.SetType(window.WindowTypeModal)
		m.SetOwner(owner)
		m.SetBounds(core.UnitRect{Width: 160, Height: 96})
		wm.AddWindow(m)
		wm.ActivateWindow(m)
		return m
	}
	under := modal("under")
	top := modal("top")

	top.Close()

	if got := wm.ActiveWindow(); got != under {
		name := "nothing"
		if got != nil {
			name = got.Title()
		}
		t.Errorf("closing the top modal activated %s, want the modal under it", name)
	}
}

// An owner that has gone, or a window that is hidden, cannot take the focus:
// it goes to the next window back that can.
func TestAGoneOwnerOrAHiddenWindowIsPassedOver(t *testing.T) {
	r := newFocusRig(t)
	r.torn("first")
	hidden := r.torn("hidden")
	owner := r.torn("owner")
	dialog := r.torn("dialog")
	dialog.SetOwner(owner)

	owner.Close()
	hidden.Hide()
	dialog.Close()
	r.d.refocusAfterTornClose(dialog)

	if got := r.lastRaised(); got != "first" {
		t.Errorf("closing the dialog raised %q, want first", got)
	}
}

// A window closing that did not have the focus changes nobody's: the manager
// asks where the focus goes only when the active window closes.
func TestClosingAWindowInTheBackgroundAsksNothing(t *testing.T) {
	r := newFocusRig(t)
	wm := r.d.windowManager
	behind := r.docked("behind")
	front := r.docked("front")
	asked := 0
	wm.SetNextActiveChooser(func(*window.Window) *window.Window { asked++; return nil })
	behind.Close()
	if asked != 0 || wm.ActiveWindow() != front {
		t.Errorf("closing a background window asked %d times and left %v active, want 0 and front", asked, wm.ActiveWindow())
	}
}

// An owner blocked by another of its modals gives way to that modal too.
func TestAnOwnerBlockedByAnotherModalGivesWayToIt(t *testing.T) {
	r := newFocusRig(t)
	wm := r.d.windowManager
	owner := r.torn("owner")
	modal := window.NewWindow("modal")
	modal.SetType(window.WindowTypeModal)
	modal.SetOwner(owner)
	modal.SetBounds(core.UnitRect{Width: 160, Height: 96})
	wm.AddWindow(modal)
	dialog := r.torn("dialog")
	dialog.SetOwner(owner)

	dialog.Close()
	r.d.refocusAfterTornClose(dialog)

	if got := r.lastRaised(); got == "owner" {
		t.Error("closing the dialog raised its owner, which a modal is blocking")
	}
	if got := wm.ActiveWindow(); got != modal {
		t.Errorf("closing the dialog activated %v, want the modal blocking its owner", got)
	}
}
