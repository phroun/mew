package main

import (
	"testing"

	"github.com/phroun/kittytk/inprocess"
	"github.com/phroun/kittytk/objects/trinkets"
)

// The change event reports a tab by index, so wireTerminalTab has to name one.
// Insert a tab anywhere before it and that number is silently wrong: the shell
// would start on whatever tab took the position, and the Terminal tab would
// sit there never starting one. This reads the caption the built strip
// actually has at that index.
func TestTerminalTabIsWhereTheWiringExpectsIt(t *testing.T) {
	conn := inprocess.New(nil)
	ui, err := conn.Build(mainBuildScript())
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	tabs, _ := ui.Object("tabs").Target().(*trinkets.TabTrinket)
	if tabs == nil {
		t.Fatal("no tab strip behind the main build")
	}
	if terminalTabIndex >= tabs.Count() {
		t.Fatalf("terminalTabIndex is %d but the strip has %d tabs",
			terminalTabIndex, tabs.Count())
	}
	if got := tabs.TabText(terminalTabIndex); got != "Terminal" {
		t.Errorf("tab %d is %q, but wireTerminalTab starts the shell there; "+
			"a tab was added before it", terminalTabIndex, got)
	}
}

// The Terminal tab holds a real terminal surface, and it is the one the
// wiring addresses. A path that stops resolving leaves the handle at id 0,
// which is quiet: the shell would never start and the tab would look inert.
func TestTerminalTabHoldsTheSurfaceTheWiringAddresses(t *testing.T) {
	conn := inprocess.New(nil)
	ui, err := conn.Build(mainBuildScript())
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if _, ok := ui.Object("mterm").Target().(*trinkets.PurfecTerm); !ok {
		t.Errorf("mterm is %T, want a terminal", ui.Object("mterm").Target())
	}
	if _, ok := ui.Object("mtclear").Target().(*trinkets.Button); !ok {
		t.Errorf("mtclear is %T, want a button", ui.Object("mtclear").Target())
	}
}

// Nothing starts a shell until the tab is opened. Building and wiring the
// main window must leave the app with no child process -- which is also what
// keeps the rest of this package's tests from spawning shells.
func TestTerminalTabStartsNoShellUntilItIsOpened(t *testing.T) {
	conn := inprocess.New(nil)
	ui, err := conn.Build(mainBuildScript())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	a := &app{conn: conn, ui: ui}
	a.wireMainWindow()

	if a.terminalStarted {
		t.Error("wiring the main window started the terminal")
	}
	if len(a.drivers) != 0 {
		t.Errorf("wiring the main window spawned %d PTYs, want 0", len(a.drivers))
	}
}

// Opening the tab is what starts it, and only once: switching away and back
// must not stack a second shell on the same surface.
func TestOpeningTheTerminalTabStartsItOnce(t *testing.T) {
	conn := inprocess.New(nil)
	ui, err := conn.Build(mainBuildScript())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	a := &app{conn: conn, ui: ui}
	a.wireMainWindow()
	// A real child process, so close it however the test ends.
	t.Cleanup(func() {
		for _, d := range a.drivers {
			d.Close()
		}
	})

	tabs, _ := ui.Object("tabs").Target().(*trinkets.TabTrinket)
	if tabs == nil {
		t.Fatal("no tab strip behind the main build")
	}

	tabs.SetCurrentIndex(terminalTabIndex)
	if !a.terminalStarted {
		t.Fatal("selecting the Terminal tab did not start it")
	}
	started := len(a.drivers)

	tabs.SetCurrentIndex(0)
	tabs.SetCurrentIndex(terminalTabIndex)
	if len(a.drivers) != started {
		t.Errorf("returning to the tab started another shell: %d PTYs, want %d",
			len(a.drivers), started)
	}
}
