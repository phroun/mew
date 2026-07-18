package editor

import (
	"testing"

	"github.com/phroun/mew/internal/window"
)

// countOptionsWindows returns how many top-dock windows carry Class "options".
func countOptionsWindows(e *Editor) int {
	n := 0
	for _, w := range e.WindowManager.GetWindowsByDock(window.DockTop) {
		if w.Class == "options" {
			n++
		}
	}
	return n
}

// editor_options opens the options display on first invocation and dismisses
// it on the second, rather than stacking a second identical window.
func TestEditorOptionsToggle(t *testing.T) {
	e, _ := newTestEditor(t, "")

	if got := countOptionsWindows(e); got != 0 {
		t.Fatalf("no options window should exist yet, got %d", got)
	}

	e.executeCommand("editor_options")
	if got := countOptionsWindows(e); got != 1 {
		t.Fatalf("first invocation should open the options window, got %d", got)
	}

	e.executeCommand("editor_options")
	if got := countOptionsWindows(e); got != 0 {
		t.Fatalf("second invocation should dismiss the options window, got %d", got)
	}

	// And it can be reopened again (toggle is stateless).
	e.executeCommand("editor_options")
	if got := countOptionsWindows(e); got != 1 {
		t.Fatalf("third invocation should reopen the options window, got %d", got)
	}
}

// buffer_list toggles open/dismiss on repeated invocation.
func TestBufferListToggle(t *testing.T) {
	e, _ := newTestEditor(t, "")
	count := func() int {
		n := 0
		for _, w := range e.WindowManager.GetWindowsByDock(window.DockTop) {
			if w.Class == "buffer_list" {
				n++
			}
		}
		return n
	}
	if count() != 0 {
		t.Fatalf("no list window yet, got %d", count())
	}
	e.executeCommand("buffer_list")
	if count() != 1 {
		t.Fatalf("first invocation should open the list, got %d", count())
	}
	e.executeCommand("buffer_list")
	if count() != 0 {
		t.Fatalf("second invocation should dismiss the list, got %d", count())
	}
	e.executeCommand("buffer_list")
	if count() != 1 {
		t.Fatalf("third invocation should reopen the list, got %d", count())
	}
}
