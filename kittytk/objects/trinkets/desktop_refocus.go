package trinkets

import (
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/platform"
)

// Where the focus goes when a window closes.
//
// **Back to the window that held it before**, wherever that window lives. A
// window opened from another -- a dialog asked for from a button, say -- took
// the focus from it, and closing it is the person finishing with it, so the
// focus goes back to where they were. Its owner, if it has one; otherwise the
// most recent window in the focus history that is still there to take it.
//
// Neither z-order nor creation order is that. The topmost remaining window on
// the desktop's own surface knows nothing of a torn window the person came
// from, and the newest torn window, or a solo host's primary, is only where
// windows happen to be.

// noteFocusedLocked moves w to the most recent end of the focus history.
// d.mu held.
func (d *Desktop) noteFocusedLocked(w *window.Window) {
	for i, h := range d.focusHistory {
		if h == w {
			d.focusHistory = append(d.focusHistory[:i], d.focusHistory[i+1:]...)
			break
		}
	}
	d.focusHistory = append(d.focusHistory, w)
}

// nextFocus is the window the focus goes back to when closing goes, or nil
// when nothing is left to take it. closing has already closed, so the history
// passes over it with every other closed window. A window a modal is blocking gives way to
// that modal: the focus goes where the person can act.
func (d *Desktop) nextFocus(closing *window.Window) *window.Window {
	if o := closing.Owner(); o != nil && d.canTakeFocusBack(o) {
		return d.unblocked(o)
	}
	// Pruned for its own sake: canTakeFocusBack would pass over the closed
	// windows anyway, but a history nobody trims keeps every window ever
	// focused.
	d.mu.Lock()
	kept := d.focusHistory[:0]
	for _, w := range d.focusHistory {
		if !w.IsClosed() {
			kept = append(kept, w)
		}
	}
	d.focusHistory = kept
	history := append([]*window.Window(nil), kept...)
	d.mu.Unlock()

	for i := len(history) - 1; i >= 0; i-- {
		if w := history[i]; d.canTakeFocusBack(w) {
			return d.unblocked(w)
		}
	}
	return nil
}

// canTakeFocusBack reports whether w is somewhere the focus can go: open,
// shown, not minimized, and on a surface -- the desktop's own, or one of its
// own as a torn window.
func (d *Desktop) canTakeFocusBack(w *window.Window) bool {
	if w.IsClosed() || !w.IsVisible() || w.IsMinimized() {
		return false
	}
	if d.tornHostForWindow(w) != nil {
		return true
	}
	for _, docked := range d.windowManager.Windows() {
		if docked == w {
			return true
		}
	}
	return false
}

// unblocked is w, or the modal blocking it.
func (d *Desktop) unblocked(w *window.Window) *window.Window {
	if top := d.windowManager.TopModalBlocking(w); top != nil {
		return top
	}
	return w
}

// chooseNextActive is the window manager's question when its active window
// closes. A docked answer the manager activates itself; a torn one it cannot,
// so it is brought forward here.
func (d *Desktop) chooseNextActive(closing *window.Window) *window.Window {
	next := d.nextFocus(closing)
	if next != nil && d.tornHostForWindow(next) != nil {
		d.Post(func() { d.giveFocusTo(next) })
	}
	return next
}

// giveFocusTo raises target's OS surface -- its own if it is torn, otherwise
// the desktop's, where the window manager is told to activate it -- and points
// the desktop's focus at it.
func (d *Desktop) giveFocusTo(target *window.Window) {
	if h := d.tornHostForWindow(target); h != nil {
		if ns, ok := h.Surface().(platform.NativeSurface); ok {
			ns.Raise()
		}
	} else {
		d.mu.RLock()
		surf := d.surface
		d.mu.RUnlock()
		if ns, ok := surf.(platform.NativeSurface); ok {
			ns.Raise()
		}
		d.windowManager.ActivateWindow(target)
	}
	d.windowFocusChanged(target)
}
