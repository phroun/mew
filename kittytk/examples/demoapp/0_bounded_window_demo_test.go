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
	got := win.Bounds()
	if got.Width != 480 || got.Height != 320 {
		t.Errorf("maximized it is %v, want its maximum of 480x320", got.Size())
	}
	if got.X != room.X+(room.Width-480)/2 || got.Y != room.Y+(room.Height-320)/2 {
		t.Errorf("maximized it sits at %d,%d, want it centered in %v", got.X, got.Y, room)
	}

	// And there is room left over for the filler to occupy, which is the
	// whole point of the demonstration.
	if len(window.MaximizedFillerRects(win, room)) != 4 {
		t.Error("maximized in a room larger than its maximum, it left no filler to see")
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
	if got := window.MaximizedBounds(win, pane); got.Width != 320 || got.Height != 200 {
		t.Errorf("maximized in the pane it is %v, want its maximum of 320x200", got.Size())
	}
	if len(window.MaximizedFillerRects(win, pane)) != 4 {
		t.Error("maximized in the pane, it left no filler to see")
	}
}
