package editor

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

func helpTestEditor(t *testing.T, files map[string]string) (*Editor, *window.Window) {
	t.Helper()
	e := mewHomeEditor(t, "[options]\nsyntax=dokuwiki\n", files)
	id := e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, Type: window.DocWindow, Dock: window.DockNone,
		Buffer: buffer.NewFromString("start\n"), SetFocus: true,
		LinkBrowsing: e.Config.LinkBrowsing,
	})
	return e, e.WindowManager.GetWindow(id)
}

func helpWindowCount(e *Editor) int {
	n := 0
	for _, w := range e.WindowManager.GetWindowsByDock(window.DockTop) {
		if w.Tag == helpWindowTag {
			n++
		}
	}
	return n
}

// buffer_open_file with an argument opens THAT file directly (the menu /
// scripted equivalent of ^B O then a name) — here the help wiki, which fills
// the shared help slot but is NOT the Quick Help window.
func TestBufferOpenFileArgumentOpensHelp(t *testing.T) {
	e, _ := helpTestEditor(t, nil)
	e.executeCommand(`buffer_open_file "help:/"`)

	if !e.anyHelpWindowOpen() {
		t.Fatal(`buffer_open_file "help:/" should open a help window`)
	}
	if e.quickHelpWindowOpen() {
		t.Fatal("a help:/ wiki page is not the Quick Help window")
	}
}

// The help family is mutually exclusive in one docked slot: help_toggle with no
// argument toggles Quick Help and closes EITHER open help window; the Quick
// Help checkmark predicate tracks the built-in window specifically.
func TestHelpMutualExclusionAndCheckmark(t *testing.T) {
	e, _ := helpTestEditor(t, nil)

	if e.anyHelpWindowOpen() {
		t.Fatal("no help window at start")
	}

	// help_toggle (no arg) opens Quick Help.
	e.executeCommand("help_toggle")
	if !e.quickHelpWindowOpen() || !e.anyHelpWindowOpen() {
		t.Fatal("help_toggle should open the Quick Help window")
	}
	// Again closes it.
	e.executeCommand("help_toggle")
	if e.anyHelpWindowOpen() {
		t.Fatal("help_toggle should close the Quick Help window")
	}

	// A help wiki page fills the slot but is not Quick Help (checkmark stays off).
	e.executeCommand(`buffer_open_file "help:/"`)
	if !e.anyHelpWindowOpen() || e.quickHelpWindowOpen() {
		t.Fatal("a help wiki page is a help window but not Quick Help")
	}
	// help_toggle (no arg) closes EITHER help window — here the wiki page.
	e.executeCommand("help_toggle")
	if e.anyHelpWindowOpen() {
		t.Fatal("help_toggle should close the open help wiki page")
	}

	// Mutual exclusivity: opening Quick Help then a wiki page keeps ONE window.
	e.executeCommand("help_toggle")               // Quick Help
	e.executeCommand(`buffer_open_file "help:/"`) // wiki replaces Quick Help
	if e.quickHelpWindowOpen() {
		t.Fatal("opening the help wiki must replace Quick Help (shared slot)")
	}
	if n := helpWindowCount(e); n != 1 {
		t.Fatalf("want exactly one help window in the slot, got %d", n)
	}
}

// help_toggle with a page argument opens that help wiki page in the shared slot
// (the same as buffer_open_file "help:/<arg>"), replacing whatever help was
// showing — so one key opens the Quick Help and another opens a help page.
func TestHelpToggleArgumentOpensPage(t *testing.T) {
	e, _ := helpTestEditor(t, map[string]string{
		"help/keys.txt": "=== Keys ===\nbindings\n",
	})

	// Quick Help first.
	e.executeCommand("help_toggle")
	if !e.quickHelpWindowOpen() {
		t.Fatal("precondition: Quick Help should open")
	}

	// help_toggle "keys" opens the help wiki page, replacing Quick Help.
	e.executeCommand(`help_toggle "keys"`)
	if e.quickHelpWindowOpen() {
		t.Fatal(`help_toggle "keys" should replace Quick Help with the wiki page`)
	}
	var wiki *window.Window
	for _, w := range e.WindowManager.GetWindowsByDock(window.DockTop) {
		if w.Tag == helpWindowTag && w.WikiName == "help" {
			wiki = w
		}
	}
	if wiki == nil {
		t.Fatal(`help_toggle "keys" should open the help wiki page`)
	}
	if helpWindowCount(e) != 1 {
		t.Fatalf("the help slot should hold exactly one window, got %d", helpWindowCount(e))
	}
}

// notifyHelpState pushes the Quick Help window's open state to the host on the
// first render and thereafter on transitions (a host syncs a Quick Help
// checkmark to it) — and a help WIKI page does not trip it.
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
