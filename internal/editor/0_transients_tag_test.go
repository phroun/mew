package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// countTransients returns how many bottom-docked transient message viewports
// currently carry the given class.
func countTransients(e *Editor, class string) int {
	n := 0
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockBottom) {
		if w.Class == class {
			n++
		}
	}
	return n
}

// A tagged notification replaces its same-tag predecessor instead of stacking,
// while an UNtagged one always stacks.
func TestTaggedNotificationReplaces(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")

	// Untagged: each call stacks a new bar.
	e.ShowNotification("one")
	e.ShowNotification("two")
	if got := countTransients(e, "notification"); got != 2 {
		t.Fatalf("untagged notifications should stack; got %d, want 2", got)
	}

	// Tagged: three calls collapse to a single bar (the latest).
	e.ShowNotificationTagged("a", "grp")
	e.ShowNotificationTagged("b", "grp")
	e.ShowNotificationTagged("c", "grp")
	tagged := 0
	var last *viewport.Viewport
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockBottom) {
		if w.Tag == "grp" {
			tagged++
			last = w
		}
	}
	if tagged != 1 {
		t.Fatalf("same-tag notifications should collapse to one; got %d", tagged)
	}
	if last == nil || !strings.Contains(last.MessageTopInner, "c") {
		t.Fatalf("the surviving tagged toast should be the latest; got %+v", last)
	}
	// The untagged pair is untouched by the tagged replacement.
	if got := countTransients(e, "notification"); got != 3 {
		t.Fatalf("tagged replacement must not disturb untagged toasts; total %d, want 3", got)
	}
}

// The "Switched to …" focus toast and the "Buffer is read-only" warning are
// tagged, so a rapid series of each collapses to one bar.
func TestFocusAndReadOnlyToastsCollapse(t *testing.T) {
	e, w := newTestEditor(t, "x\n")

	e.announceFocusedViewport()
	e.announceFocusedViewport()
	e.announceFocusedViewport()
	switched := 0
	for _, v := range e.ViewportManager.GetViewportsByDock(viewport.DockBottom) {
		if v.Tag == "navigate" {
			switched++
		}
	}
	if switched != 1 {
		t.Fatalf("repeated Switched-to toasts should collapse to one; got %d", switched)
	}

	w.ViewState.ReadOnly = true
	e.executeCommand("insert 'a'")
	e.executeCommand("insert 'b'")
	e.executeCommand("insert 'c'")
	ro := 0
	for _, v := range e.ViewportManager.GetViewportsByDock(viewport.DockBottom) {
		if v.Tag == "readonly_warning" {
			ro++
		}
	}
	if ro != 1 {
		t.Fatalf("repeated read-only warnings should collapse to one; got %d", ro)
	}
}
