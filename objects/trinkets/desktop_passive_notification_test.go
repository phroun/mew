package trinkets

import "testing"

// selNoop is a minimal edit target that never has a selection, used to
// exercise the "nothing selected" path.
type selNoop struct{ cut, copied bool }

func (s *selNoop) Cut()               { s.cut = true }
func (s *selNoop) Copy()              { s.copied = true }
func (s *selNoop) Paste()             {}
func (s *selNoop) SelectAll()         {}
func (s *selNoop) HasSelection() bool { return false }

func TestHasSelectionDefaultsTrue(t *testing.T) {
	// A target that advertises no selection reports false...
	if hasSelection(&selNoop{}) {
		t.Error("selNoop should report no selection")
	}
	// ...but a target without the reporter interface is assumed selected.
	if !hasSelection(&bareActor{}) {
		t.Error("an edit target without HasSelection should default to selected")
	}
}

// bareActor implements editActor but not selectionReporter.
type bareActor struct{}

func (bareActor) Cut()       {}
func (bareActor) Copy()      {}
func (bareActor) Paste()     {}
func (bareActor) SelectAll() {}

// A passive notification overlays the status bar and reverts to the prior
// content once its generation's timer clears it.
func TestPassiveNotificationOverlaysAndReverts(t *testing.T) {
	d := NewDesktop()
	sb := NewStatusBar()
	sb.SetText("Ready")
	d.SetStatusBar(sb)

	d.NotifyPassive("Nothing was selected to cut.")
	if got := sb.Text(); got != "Nothing was selected to cut." {
		t.Fatalf("status text = %q, want the notice", got)
	}

	// Capturing the baseline must not have aliased the live section, so the
	// original text is still recoverable.
	d.notifyMu.Lock()
	gen := d.notifyGen
	d.notifyMu.Unlock()

	// A stale generation (an already-superseded notice) must not revert.
	d.clearPassiveNotification(gen - 1)
	if got := sb.Text(); got != "Nothing was selected to cut." {
		t.Fatalf("stale clear changed the bar: %q", got)
	}

	// The current generation reverts to the normal content.
	d.clearPassiveNotification(gen)
	if got := sb.Text(); got != "Ready" {
		t.Fatalf("after clear, status text = %q, want Ready", got)
	}
}

// The newest notice wins immediately and restarts the countdown; the older
// notice's timer, when it fires, is a no-op.
func TestPassiveNotificationNewestWins(t *testing.T) {
	d := NewDesktop()
	sb := NewStatusBar()
	sb.SetText("Ready")
	d.SetStatusBar(sb)

	d.NotifyPassive("first")
	d.notifyMu.Lock()
	firstGen := d.notifyGen
	d.notifyMu.Unlock()

	d.NotifyPassive("second")
	if got := sb.Text(); got != "second" {
		t.Fatalf("status text = %q, want second", got)
	}

	// The first notice's timer firing must not revert - a newer notice owns
	// the bar now.
	d.clearPassiveNotification(firstGen)
	if got := sb.Text(); got != "second" {
		t.Fatalf("superseded clear changed the bar: %q", got)
	}

	// The baseline is still the original content, not "first".
	d.notifyMu.Lock()
	secondGen := d.notifyGen
	d.notifyMu.Unlock()
	d.clearPassiveNotification(secondGen)
	if got := sb.Text(); got != "Ready" {
		t.Fatalf("after final clear, status text = %q, want Ready", got)
	}
}
