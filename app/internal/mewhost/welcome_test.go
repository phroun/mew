package mewhost

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
)

// The welcome is a gate: Try is what opens the editor. Nothing of the session
// exists until this runs, so a dialog that forgot to call it would leave the
// user staring at an empty desktop.
func TestWelcomeTryOpensTheEditor(t *testing.T) {
	opened, installed := 0, 0
	dlg := newWelcomeDialog("Welcome", []string{"hi"},
		func() { installed++ }, func() { opened++ })

	dlg.doTry()

	if opened != 1 {
		t.Errorf("Try ran the open-editor action %d times, want 1", opened)
	}
	if installed != 0 {
		t.Error("Try must not install")
	}
	if dlg.IsVisible() {
		t.Error("the gate should close once it has been answered")
	}
}

// Install is the other answer, and it does NOT open the editor: the session
// being installed is the one that will run, not this one.
func TestWelcomeInstallDoesNotOpenTheEditor(t *testing.T) {
	opened, installed := 0, 0
	dlg := newWelcomeDialog("Welcome", []string{"hi"},
		func() { installed++ }, func() { opened++ })

	dlg.doInstall()

	if installed != 1 {
		t.Errorf("Install ran %d times, want 1", installed)
	}
	if opened != 0 {
		t.Error("Install must not open an editor this process is about to abandon")
	}
}

// Both spellings of accept reach Install, and Escape reaches Try. Return is the
// home-row key, Enter the keypad one; a dialog answers to either.
func TestWelcomeKeys(t *testing.T) {
	for _, key := range []string{"Return", "Enter"} {
		opened, installed := 0, 0
		dlg := newWelcomeDialog("Welcome", []string{"hi"},
			func() { installed++ }, func() { opened++ })
		if !dlg.HandleKeyPress(core.KeyPressEvent{Key: key}) {
			t.Errorf("%s was not handled", key)
		}
		if installed != 1 || opened != 0 {
			t.Errorf("%s: install=%d open=%d, want the primary action", key, installed, opened)
		}
	}

	opened, installed := 0, 0
	dlg := newWelcomeDialog("Welcome", []string{"hi"},
		func() { installed++ }, func() { opened++ })
	if !dlg.HandleKeyPress(core.KeyPressEvent{Key: "Escape"}) {
		t.Error("Escape was not handled")
	}
	if opened != 1 || installed != 0 {
		t.Errorf("Escape: install=%d open=%d, want the dismissal", installed, opened)
	}
}

// With no welcome to show - the TUI host, or an already-installed copy - the
// caller is told so and opens the editor itself, rather than waiting forever
// for a Try that never comes.
func TestNoWelcomeLeavesTheEditorToTheCaller(t *testing.T) {
	desktop := trinkets.NewDesktop()
	application := app.New(nil)
	opened := 0

	if maybeShowWelcome(desktop, application, nil, false /* graphical */, func() { opened++ }) {
		t.Fatal("the TUI host has no first-run welcome")
	}
	if opened != 0 {
		t.Error("maybeShowWelcome must not open the editor itself; the caller does")
	}
}

// The welcome is the only thing on screen now, so it sits in the middle of the
// desktop rather than in the manager's top-left cascade slot.
func TestWelcomeIsCenteredOnTheDesktop(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 600)
	desktop := trinkets.NewDesktop()
	desktop.SetBackend(px) // the window manager is built with the backend
	wm := desktop.WindowManager()
	if wm == nil {
		t.Fatal("a backed desktop should have a window manager")
	}
	wm.SetScreenBounds(core.UnitRect{Width: 800, Height: 600})

	dlg := newWelcomeDialog("Welcome", []string{"hi"}, func() {}, func() {})
	size := dlg.Bounds()
	wm.AddWindow(&dlg.Window)
	centerOnDesktop(desktop, &dlg.Window)

	area := wm.ClientArea()
	got := dlg.Bounds()
	wantX := area.X + (area.Width-size.Width)/2
	wantY := area.Y + (area.Height-size.Height)/2
	if got.X != wantX || got.Y != wantY {
		t.Errorf("welcome at (%v,%v), want centered at (%v,%v)", got.X, got.Y, wantX, wantY)
	}
}

// Closing the welcome by its [x] is a dismissal, and a dismissal is Try:
// otherwise the gate would leave an empty desktop with no way to open mew.
func TestWelcomeDismissedByCloseOpensTheEditor(t *testing.T) {
	opened, installed := 0, 0
	dlg := newWelcomeDialog("Welcome", []string{"hi"},
		func() { installed++ }, func() { opened++ })

	dlg.Close()

	if opened != 1 || installed != 0 {
		t.Errorf("close: install=%d open=%d, want the editor opened", installed, opened)
	}
}

// ...but a chosen Install is not ALSO a dismissal: the close that follows it
// must not open an editor this process is abandoning.
func TestWelcomeInstallIsNotReadAsADismissal(t *testing.T) {
	opened, installed := 0, 0
	dlg := newWelcomeDialog("Welcome", []string{"hi"},
		func() { installed++ }, func() { opened++ })

	dlg.doInstall() // runs Install, then closes the window

	if installed != 1 || opened != 0 {
		t.Errorf("install: install=%d open=%d, want install alone", installed, opened)
	}
}
