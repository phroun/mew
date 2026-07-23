package editor

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

func helpTestEditor(t *testing.T, files map[string]string) *Editor {
	t.Helper()
	e := mewHomeEditor(t, "[options]\nsyntax=dokuwiki\n", files)
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, Type: window.DocWindow, Dock: window.DockNone,
		Buffer: buffer.NewFromString("start\n"), SetFocus: true,
		LinkBrowsing: e.Config.LinkBrowsing,
	})
	return e
}

// buffer_open_file with an argument opens THAT file directly — an ORDINARY,
// UNtagged help window (stackable), NOT the docked help slot that help_toggle
// owns.
func TestBufferOpenFileHelpIsUntagged(t *testing.T) {
	e := helpTestEditor(t, nil)
	e.executeCommand(`buffer_open_file "help:/"`)

	if e.helpWindow() != nil {
		t.Fatal("buffer_open_file must not create a tagged help-slot window")
	}
	found := false
	for _, w := range e.WindowManager.GetWindowsByDock(window.DockTop) {
		if w.WikiName == "help" {
			found = true
		}
	}
	if !found {
		t.Fatal(`buffer_open_file "help:/" should still open a help wiki window`)
	}
}

// help_toggle with no argument toggles the Quick Help location in the docked
// help window: it opens the window at Quick Help, and toggling again (still at
// Quick Help) closes it.
func TestHelpToggleQuickHelp(t *testing.T) {
	e := helpTestEditor(t, nil)
	if e.helpWindow() != nil {
		t.Fatal("no help window at start")
	}
	e.executeCommand("help_toggle")
	if !e.quickHelpWindowOpen() || e.helpWindow() == nil {
		t.Fatal("help_toggle should open the docked help window at Quick Help")
	}
	e.executeCommand("help_toggle")
	if e.helpWindow() != nil {
		t.Fatal("help_toggle at Quick Help should close the docked help window")
	}
}

// help_toggle <page> navigates the SAME docked window to that help page,
// growing its nav history so BACK returns to where the reader came from; the
// checkmark turns off (a page is not Quick Help).
func TestHelpToggleNavigatesWithHistory(t *testing.T) {
	e := helpTestEditor(t, map[string]string{"help/keys.txt": "=== Keys ===\nbindings\n"})

	e.executeCommand("help_toggle") // Quick Help
	hw := e.helpWindow()
	if hw == nil {
		t.Fatal("Quick Help should open the docked window")
	}
	quickURL := e.bufferCanonicalURL(hw.Buffer)

	e.executeCommand(`help_toggle "keys"`) // navigate to the page
	if e.helpWindow() != hw {
		t.Fatal("help_toggle should navigate the SAME docked window")
	}
	if e.quickHelpWindowOpen() {
		t.Fatal("a wiki page is not Quick Help (checkmark off)")
	}
	if e.bufferCanonicalURL(hw.Buffer) == quickURL {
		t.Fatal("the window should have navigated off Quick Help")
	}

	// Back returns to Quick Help via the window's nav history.
	if !hw.NavHistoryPrior() {
		t.Fatal("the help window should carry back history after navigating")
	}
	if e.bufferCanonicalURL(hw.Buffer) != quickURL {
		t.Fatal("back should return to the Quick Help location")
	}
}

// help_toggle <page> when the window is already showing that page closes it.
func TestHelpToggleClosesAtCurrentLocation(t *testing.T) {
	e := helpTestEditor(t, map[string]string{"help/keys.txt": "=== Keys ===\n"})
	e.executeCommand(`help_toggle "keys"`)
	if e.helpWindow() == nil {
		t.Fatal(`help_toggle "keys" should open the page`)
	}
	e.executeCommand(`help_toggle "keys"`)
	if e.helpWindow() != nil {
		t.Fatal("help_toggle at the current location should close the docked window")
	}
}

// notifyHelpState pushes whether the docked help window is showing Quick Help,
// on the first render and on transitions (a host syncs the checkmark to it).
func TestNotifyHelpStateTransitions(t *testing.T) {
	e, _, _ := renderedEditorWithConfig(t, "hi\n", "[options]\n")
	var states []bool
	e.Config.HelpState = func(open bool) { states = append(states, open) }
	e.createPluginWindows()

	e.performRender() // first push: closed
	e.executeCommand("help_toggle")
	e.performRender() // Quick Help -> open
	e.executeCommand("help_toggle")
	e.performRender() // -> closed

	if len(states) != 3 || states[0] || !states[1] || states[2] {
		t.Fatalf("help-state transitions = %v, want [false true false]", states)
	}
}
