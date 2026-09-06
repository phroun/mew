package main

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/inprocess"
	"github.com/phroun/kittytk/objects/window"
)

// buildWindow runs a script and returns the window it keyed.
func buildWindow(t *testing.T, src, key string) *window.Window {
	t.Helper()
	ui, err := inprocess.New(nil).Build(src)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	win, _ := ui.Object(key).Target().(*window.Window)
	if win == nil {
		t.Fatalf("the script built no window behind key %q", key)
	}
	return win
}

// The demo's bounded window says how far it grows, and maximizing it takes
// what it may of the desktop and centers it there rather than filling it.
func TestTheBoundedWindowStopsWhereItSaysWhenMaximized(t *testing.T) {
	win := buildWindow(t, boundedWindowScript(1), "bwin")

	if got := win.MaximumSize(); got.Width != 480 || got.Height != 320 {
		t.Fatalf("the bounded window's maximum is %v, want 480x320", got)
	}
	// Its opening size is under the maximum, so maximizing it visibly does
	// something -- a demo of a bound that never bites shows nothing.
	if b := win.Bounds(); b.Width >= 480 || b.Height >= 320 {
		t.Errorf("it opens at %v, which is not under its own maximum", b.Size())
	}

	m := window.NewWindowManager()
	m.SetScreenBounds(core.UnitRect{Width: 1200, Height: 800})
	m.AddWindow(win)
	m.MaximizeWindow(win)

	room := m.ClientArea()
	if got := win.Bounds(); got != room {
		t.Errorf("maximized its surface is %v, want the whole room %v", got, room)
	}
	fr := window.MaximizedFrameRect(win, room.Size())
	if fr.Width != 480 || fr.Height != 320 {
		t.Errorf("maximized its frame is %v, want its maximum of 480x320", fr.Size())
	}
	if fr.X != (room.Width-480)/2 || fr.Y != (room.Height-320)/2 {
		t.Errorf("maximized its frame sits at %d,%d, want it centered in %v", fr.X, fr.Y, room)
	}
}

// The MDI pane's bounded child does the same one level down, where the room
// it declines is the pane's.
func TestTheBoundedMDIChildStopsWhereItSays(t *testing.T) {
	// The script sets the child on the demo's own mdi pane, so build the
	// main window first for it to attach to.
	conn := inprocess.New(nil)
	if _, err := conn.Build(mainBuildScript()); err != nil {
		t.Fatalf("build main: %v", err)
	}
	ui, err := conn.Build(mdiBoundedChildScript(1))
	if err != nil {
		t.Fatalf("build child: %v", err)
	}
	win, _ := ui.Object("bwwin").Target().(*window.Window)
	if win == nil {
		t.Fatal("the script built no MDI child behind key bwwin")
	}

	if got := win.MaximumSize(); got.Width != 320 || got.Height != 200 {
		t.Errorf("the bounded child's maximum is %v, want 320x200", got)
	}

	// The pane it lives in is larger than that, so maximizing it there leaves
	// room over.
	pane := core.UnitRect{Width: 640, Height: 400}
	fr := window.MaximizedFrameRect(win, pane.Size())
	if fr.Width != 320 || fr.Height != 200 {
		t.Errorf("maximized in the pane its frame is %v, want its maximum of 320x200", fr.Size())
	}
	// Centred, then floored onto the cell grid.
	cm := core.DefaultCellMetrics()
	wantX, wantY := cm.RoundDownToCellX((640-320)/2), cm.RoundDownToCellY((400-200)/2)
	if fr.X != wantX || fr.Y != wantY {
		t.Errorf("maximized in the pane its frame sits at %d,%d, want %d,%d -- centered "+
			"and floored onto the cell grid", fr.X, fr.Y, wantX, wantY)
	}
}

// Every window the demo opens has to land on the cell grid. A cell surface
// renders a window nowhere else -- drawing rounds and hit-testing does not, so
// a window standing off the grid draws in one row and answers the mouse in
// another.
//
// The y coordinates are the ones to watch: a cell is 8 units across and 16
// down, so a script author who steps both axes by the same number gets away
// with it horizontally and not vertically.
func TestTheDemosWindowsOpenOnTheCellGrid(t *testing.T) {
	m := core.DefaultCellMetrics()
	for _, c := range []struct {
		name string
		src  string
		key  string
	}{
		{"bounded window", boundedWindowScript(1), "bwin"},
		{"bounded window 2", boundedWindowScript(2), "bwin"},
		{"demo terminal window", demoTerminalScript(1), "dwin"},
		{"demo terminal window 2", demoTerminalScript(2), "dwin"},
		{"secondary app window", secondaryBuildScript(1), "w"},
	} {
		win := buildWindow(t, c.src, c.key)
		b := win.Bounds()
		if b.X%m.UnitsPerCellWidth != 0 {
			t.Errorf("%s opens at x=%d, which is %d units into a %d-unit cell",
				c.name, b.X, b.X%m.UnitsPerCellWidth, m.UnitsPerCellWidth)
		}
		if b.Y%m.UnitsPerCellHeight != 0 {
			t.Errorf("%s opens at y=%d, which is %d units into a %d-unit row",
				c.name, b.Y, b.Y%m.UnitsPerCellHeight, m.UnitsPerCellHeight)
		}
	}
}
