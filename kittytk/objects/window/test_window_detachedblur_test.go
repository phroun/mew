package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// blurRecorder stands in for the desktop: it offers both the generic
// container blur (what a DOCKED window's blur item uses) and the detached
// one, and records which was asked.
type blurRecorder struct {
	*core.TrinketBase
	generic  int
	detached []string
}

func (b *blurRecorder) Children() []core.Trinket            { return nil }
func (b *blurRecorder) AddChild(core.Trinket)               {}
func (b *blurRecorder) RemoveChild(core.Trinket)            {}
func (b *blurRecorder) ChildAt(core.UnitPoint) core.Trinket { return nil }
func (b *blurRecorder) Layout()                             {}
func (b *blurRecorder) LayoutManager() core.LayoutManager   { return nil }
func (b *blurRecorder) SetLayoutManager(core.LayoutManager) {}

func (b *blurRecorder) KeyboardBlurChildren() bool { return true }
func (b *blurRecorder) PerformKeyboardBlur()       { b.generic++ }
func (b *blurRecorder) BlurDetachedWindow(w *Window) {
	b.detached = append(b.detached, w.Title())
}

// A DOCKED window's blur goes to its container: the desktop or MDI pane
// focuses its menu bar, and the window is still on screen beside it.
//
// A TORN window's surface holds that one window and nothing else, so the
// generic path would focus a menu bar the user is not looking at while this
// window kept the OS focus. It has to leave the OS window instead, which
// only the desktop can arrange.
func TestBlurRoutesByWhetherTheWindowIsTorn(t *testing.T) {
	desk := &blurRecorder{TrinketBase: core.NewTrinketBase()}
	win := NewWindow("Document")
	win.SetParent(desk)

	win.performKeyboardBlur()
	if desk.generic != 1 || len(desk.detached) != 0 {
		t.Errorf("docked blur: generic=%d detached=%v, want the container blur only",
			desk.generic, desk.detached)
	}

	win.SetDetached(true)
	win.performKeyboardBlur()
	if desk.generic != 1 {
		t.Errorf("torn blur also ran the container blur (generic=%d)", desk.generic)
	}
	if len(desk.detached) != 1 || desk.detached[0] != "Document" {
		t.Errorf("torn blur did not route to the desktop: %v", desk.detached)
	}
}

// Tearing a window off removes it from the manager's list, not from the
// trinket tree, so the walk up to the desktop still arrives. If that ever
// changes, a torn window's blur would silently do nothing.
func TestDetachedBlurrerIsReachableFromATornWindow(t *testing.T) {
	desk := &blurRecorder{TrinketBase: core.NewTrinketBase()}
	wm := NewWindowManager()
	win := NewWindow("Torn")
	win.SetParent(desk)
	wm.AddWindow(win)
	wm.RemoveWindow(win) // what tear-off does
	win.SetDetached(true)

	if win.findDetachedBlurrer() == nil {
		t.Fatal("a torn window cannot reach the desktop to blur through it")
	}
}

// With nothing above it that understands a detached blur, a torn window's
// blur must not fall through to the container path — that would focus a menu
// bar on another surface. Doing nothing is the honest answer.
func TestTornBlurWithNoDesktopDoesNothing(t *testing.T) {
	win := NewWindow("Orphan")
	win.SetDetached(true)
	win.performKeyboardBlur() // must not panic, must not reach anything
}
