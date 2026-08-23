package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A window builds its own context rather than sharing the desktop's, so a
// change here cannot stale anything over there — and focus moving between
// windows, the commonest structural event of all, costs no rebuild at all,
// because each window's context is already built and still valid.
func TestWindowBuildsItsOwnContext(t *testing.T) {
	a, b := NewWindow("A"), NewWindow("B")
	a.refreshKeyContext()
	b.refreshKeyContext()

	if a.KeyContext() == nil || b.KeyContext() == nil {
		t.Fatal("each window should hold a context of its own")
	}
	if a.KeyContext() == b.KeyContext() {
		t.Error("two windows are sharing one context")
	}

	// Putting one window into title-bar focus must not disturb the other's.
	before := b.KeyContext()
	a.SetTitleFocus(TitleFocusTitle)
	if b.KeyContext() != before {
		t.Error("a state change in one window rebuilt another window's context")
	}
}

// Entering and leaving the title bar is what brings the sixteen move and size
// bindings in and out, so it is the state change the context tracks.
func TestWindowContextFollowsTitleFocus(t *testing.T) {
	w := NewWindow("W")
	w.refreshKeyContext()

	if got := w.KeyContext().Resolve("Up"); got != "" {
		t.Errorf("ordinary state resolved Up to %q; the arrows are not the window's to eat", got)
	}

	w.SetTitleFocus(TitleFocusTitle)
	ctx := w.KeyContext()
	for key, want := range map[string]string{
		"Up":     "window_move_fine_up",
		"S-Left": "window_size_fine_left",
		"C-Up":   "window_move_up",
		"Esc":    "window_cancel_resize",
	} {
		ctx.Abandon()
		if got := ctx.Resolve(key); got != want {
			t.Errorf("title-focused: %s -> %q, want %q", key, got, want)
		}
	}

	// Leaving gives the arrows back.
	w.SetTitleFocus(TitleFocusNone)
	ctx = w.KeyContext()
	ctx.Abandon()
	if got := ctx.Resolve("Up"); got != "" {
		t.Errorf("after leaving the title bar, Up -> %q, want nothing", got)
	}
}

// The window's state, not its focus chain, is what a context is keyed on: a
// title bar is a mode of the window rather than a trinket with focus.
func TestWindowUIState(t *testing.T) {
	w := NewWindow("W")
	if got := w.windowUIState(); got != core.StateNormal {
		t.Errorf("a fresh window is in state %v, want normal", got)
	}
	w.SetTitleFocus(TitleFocusTitle)
	if got := w.windowUIState(); got != core.StateTitleBarFocused {
		t.Errorf("with the title bar focused, state is %v, want title-bar-focused", got)
	}
	// The other title-bar elements are not the resize mode.
	w.SetTitleFocus(TitleFocusBlur)
	if got := w.windowUIState(); got != core.StateNormal {
		t.Errorf("blur focus reported %v, want normal", got)
	}
}
