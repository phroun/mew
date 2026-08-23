package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// releaseSpy is a focusable trinket that records what it is handed.
type releaseSpy struct {
	core.TrinketBase
	releases []string
	declines bool
}

func (r *releaseSpy) HandleKeyRelease(e core.KeyReleaseEvent) bool {
	r.releases = append(r.releases, e.Key)
	return !r.declines
}

func (r *releaseSpy) IsVisible() bool { return true }
func (r *releaseSpy) IsEnabled() bool { return true }

// The focus manager admits a trinket by its policy, not by a CanFocus flag.
func (r *releaseSpy) FocusPolicy() core.FocusPolicy { return core.StrongFocus }

// A key release reaches the focused trinket.
//
// It did not, and every level was missing the same method: the desktop's
// KeyReleaseEvent case notified the window manager that modifiers had gone up
// and then returned false, while FocusManager, GlobalFocusManager,
// WindowManager and Window each had a HandleKeyPress with no release
// counterpart. A release dispatched by a backend had nowhere to travel — the
// SDL backend had been producing them all along and they died at the desktop.
//
// A hosted child that needs them is what this cost. A browser cannot know a
// held key was let go without them, so everything below — the trinket
// forwarding the release, the emulator encoding it for the protocol — was
// unreachable code sitting behind this gap.
func TestKeyReleaseReachesTheFocusedTrinket(t *testing.T) {
	spy := &releaseSpy{}
	fm := core.NewFocusManager(spy)
	if !fm.SetFocusedTrinket(spy) {
		t.Fatal("could not focus the spy")
	}

	if !fm.HandleKeyRelease(core.KeyReleaseEvent{Key: "a"}) {
		t.Fatal("the focus manager did not route the release to the focused trinket")
	}
	if len(spy.releases) != 1 || spy.releases[0] != "a" {
		t.Fatalf("trinket saw releases %v, want [a]", spy.releases)
	}
}

// Focus cycling is decided on the press and not repeated on the release.
//
// HandleKeyPress walks the focus chain when the focused trinket declines Tab.
// The release path deliberately has no such fallback: running it again on the
// way up would move focus twice for one keystroke.
func TestReleasingTabDoesNotCycleFocus(t *testing.T) {
	spy := &releaseSpy{declines: true}
	fm := core.NewFocusManager(spy)
	fm.SetFocusedTrinket(spy)

	before := fm.FocusedTrinket()
	fm.HandleKeyRelease(core.KeyReleaseEvent{Key: "Tab"})
	if after := fm.FocusedTrinket(); after != before {
		t.Error("releasing Tab moved focus; cycling belongs to the press alone")
	}
	if len(spy.releases) != 1 {
		t.Errorf("the trinket saw %d releases, want 1 — it is still offered the "+
			"key even though there is no fallback behind it", len(spy.releases))
	}
}
