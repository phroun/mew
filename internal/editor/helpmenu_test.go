package editor

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// buffer_open_file with an argument opens THAT file directly (the menu /
// scripted equivalent of ^B O then a name), with no prompt — here the help
// wiki, which surfaces as a top-docked wiki page.
func TestBufferOpenFileArgumentOpensHelp(t *testing.T) {
	e := mewHomeEditor(t, "[options]\nsyntax=dokuwiki\n", nil)
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "doc", Type: window.DocWindow, Dock: window.DockNone,
		Buffer: buffer.NewFromString("start\n"), SetFocus: true,
		LinkBrowsing: e.Config.LinkBrowsing,
	})

	e.executeCommand(`buffer_open_file "help:/"`)

	var help *window.Window
	for _, w := range e.WindowManager.GetWindowsByDock(window.DockTop) {
		if w.WikiName == "help" {
			help = w
		}
	}
	if help == nil {
		t.Fatal(`buffer_open_file "help:/" should open the help wiki page`)
	}
	if help.Buffer.GetLineCount() < 2 {
		t.Fatalf("help page came up blank: %d lines", help.Buffer.GetLineCount())
	}
}

// help_toggle opens/closes the built-in help window, and helpWindowOpen tracks
// it — WITHOUT conflating a help:/ wiki page (which shares Class "help" but
// carries a WikiName). Toggling the built-in help never closes the wiki page.
func TestHelpToggleStateAndWikiCollision(t *testing.T) {
	e := mewHomeEditor(t, "[options]\nsyntax=dokuwiki\n", nil)
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "doc", Type: window.DocWindow, Dock: window.DockNone,
		Buffer: buffer.NewFromString("start\n"), SetFocus: true,
		LinkBrowsing: e.Config.LinkBrowsing,
	})

	if e.helpWindowOpen() {
		t.Fatal("the built-in help window should start closed")
	}

	// A help:/ wiki page is NOT the built-in help window.
	e.executeCommand(`buffer_open_file "help:/"`)
	if e.helpWindowOpen() {
		t.Fatal("a help:/ wiki page must not count as the built-in help window")
	}

	// Toggle the built-in help window on, then off.
	e.executeCommand("help_toggle")
	if !e.helpWindowOpen() {
		t.Fatal("help_toggle should open the built-in help window")
	}
	e.executeCommand("help_toggle")
	if e.helpWindowOpen() {
		t.Fatal("help_toggle should close the built-in help window")
	}

	// The wiki page survived the toggle.
	wikiStillOpen := false
	for _, w := range e.WindowManager.GetWindowsByDock(window.DockTop) {
		if w.WikiName == "help" {
			wikiStillOpen = true
		}
	}
	if !wikiStillOpen {
		t.Fatal("toggling the built-in help must not close the help:/ wiki page")
	}
}

// notifyHelpState pushes the built-in help window's open state to the host on
// the first render and thereafter on transitions (a host syncs a Quick Help
// checkmark to it).
func TestNotifyHelpStateTransitions(t *testing.T) {
	e, _, _ := renderedEditorWithConfig(t, "hi\n", "[options]\n")
	var states []bool
	e.Config.HelpState = func(open bool) { states = append(states, open) }
	e.createPluginWindows()

	e.performRender() // first push: closed
	e.executeCommand("help_toggle")
	e.performRender() // -> open
	e.executeCommand("help_toggle")
	e.performRender() // -> closed

	if len(states) != 3 || states[0] || !states[1] || states[2] {
		t.Fatalf("help-state transitions = %v, want [false true false]", states)
	}
}
