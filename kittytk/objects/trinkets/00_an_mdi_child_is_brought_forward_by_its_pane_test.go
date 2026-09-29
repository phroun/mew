package trinkets

// A window inside an MDI pane is the pane's, not the desktop's. Bringing one to
// attention -- as a close it declined does, and as a close it is still being
// asked about does -- activates it in its pane and brings forward the window
// that holds the pane. Handed to the desktop's window manager instead, it became
// that manager's "active window": the real top-level window went inactive, and
// the next window to open deactivated the child as if it had been the one
// before, leaving it drawn inactive with nothing to bring it back.

import (
	"testing"

	"github.com/phroun/kittytk/objects/window"
)

func mdiDesktop(t *testing.T) (*Desktop, *window.Window, *MDIPane, *window.Window, *window.Window) {
	t.Helper()
	d := NewDesktop()
	d.SetBackend(&nullBackend{})
	pane := NewMDIPane()
	top := window.NewWindow("top")
	top.SetContent(pane)
	d.WindowManager().AddWindow(top)
	d.WindowManager().ActivateWindow(top)
	a, b := window.NewWindow("a"), window.NewWindow("b")
	a.SetContent(NewTextInput())
	b.SetContent(NewTextInput())
	pane.AddWindow(a)
	pane.AddWindow(b)
	pane.ActivateWindow(b)
	return d, top, pane, a, b
}

func TestAnMDIChildIsBroughtForwardByItsPane(t *testing.T) {
	d, top, pane, a, b := mdiDesktop(t)
	// Another window in front, so bringing the pane's window forward shows.
	other := window.NewWindow("other")
	d.WindowManager().AddWindow(other)
	d.WindowManager().ActivateWindow(other)
	d.SurfaceWindow(a)
	if got := d.WindowManager().ActiveWindow(); got != top {
		t.Errorf("the desktop's active window is %q, want the one holding the pane", got.Title())
	}
	if !top.IsActive() {
		t.Error("the window holding the pane went inactive")
	}
	if pane.ActiveWindow() != a || !a.IsActive() || b.IsActive() {
		t.Errorf("in the pane: active %q, a active %v, b active %v; want a alone",
			pane.ActiveWindow().Title(), a.IsActive(), b.IsActive())
	}
}

// The whole way: a close the child declines surfaces it, and the child is still
// the active one in its pane, in an active window, afterwards.
func TestADeclinedCloseLeavesTheChildActive(t *testing.T) {
	d, top, pane, _, b := mdiDesktop(t)
	b.SetOnClose(func() bool { return false })
	if b.Close() {
		t.Fatal("setup: the close was not declined")
	}
	if d.WindowManager().ActiveWindow() != top || !top.IsActive() {
		t.Error("declining the close took the desktop's activation away from the window holding the pane")
	}
	if pane.ActiveWindow() != b || !b.IsActive() {
		t.Error("the child that declined is not the active one in its pane")
	}
	// A window opened afterwards, and closed again, gives the activation back
	// to the window holding the pane, with the child still active inside it.
	q := window.NewWindow("question")
	d.WindowManager().AddWindow(q)
	d.WindowManager().ActivateWindow(q)
	if !b.IsActive() {
		t.Error("opening another window deactivated the MDI child, as if it had been a top-level window")
	}
	d.WindowManager().RemoveWindow(q)
	if d.WindowManager().ActiveWindow() != top || !top.IsActive() || !b.IsActive() {
		t.Error("after the other window went, the window holding the pane or its child is not active")
	}
}

// A child minimized inside its pane is restored there, not left on the pane's
// dock while its window comes forward.
func TestAMinimizedMDIChildIsRestoredByItsPane(t *testing.T) {
	d, top, pane, a, _ := mdiDesktop(t)
	pane.MinimizeWindow(a)
	if !a.IsMinimized() {
		t.Fatal("setup: a is not minimized")
	}
	d.SurfaceWindow(a)
	if a.IsMinimized() || pane.ActiveWindow() != a {
		t.Errorf("a minimized %v, pane's active %q; want a restored and active", a.IsMinimized(), pane.ActiveWindow().Title())
	}
	if d.WindowManager().ActiveWindow() != top {
		t.Error("the window holding the pane is not the desktop's active window")
	}
}

// Brought forward is not restored: a maximized child stays maximized.
func TestAMaximizedMDIChildStaysMaximized(t *testing.T) {
	d, _, pane, a, _ := mdiDesktop(t)
	pane.MaximizeWindow(a)
	pane.ActivateWindow(nil)
	d.SurfaceWindow(a)
	if !a.IsMaximized() || pane.ActiveWindow() != a {
		t.Errorf("a maximized %v, active %v; want it brought forward still maximized", a.IsMaximized(), pane.ActiveWindow() == a)
	}
}

// A pane standing in no window at all still brings its child forward, and
// there is nothing further out to bring.
func TestAPaneInNoWindowBringsItsChildForward(t *testing.T) {
	d := NewDesktop()
	d.SetBackend(&nullBackend{})
	pane := NewMDIPane()
	a, b := window.NewWindow("a"), window.NewWindow("b")
	pane.AddWindow(a)
	pane.AddWindow(b)
	pane.ActivateWindow(b)
	d.SurfaceWindow(a)
	if pane.ActiveWindow() != a {
		t.Error("the child was not brought forward in its pane")
	}
}
