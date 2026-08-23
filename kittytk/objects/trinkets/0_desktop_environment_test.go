package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// newRunnableDesktop builds a desktop with a raster backend, ready to RunOn a
// fake platform.
func newRunnableDesktop(t *testing.T) *Desktop {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)
	return d
}

// A desktop launched ALONE - nothing registered on it - was run AS a desktop
// environment, and an empty screen is its normal state rather than its end.
func TestBareLaunchIsADesktopEnvironment(t *testing.T) {
	d := newRunnableDesktop(t)
	plat := &msPlatform{}
	plat.script = func() { d.QuitWithCode(0) }
	d.RunOn(plat)

	if !d.IsDesktopEnvironment() {
		t.Error("a desktop launched with no application of its own is a desktop environment")
	}
}

// A desktop launched to run ONE application is that application's frame. It is
// not a place to go back to, so nothing about it survives the app.
func TestLaunchWithAnApplicationIsNotADesktopEnvironment(t *testing.T) {
	d := newRunnableDesktop(t)
	d.AddApplication(&mockApp{name: "Demo", windows: []*window.Window{window.NewWindow("Doc")}})
	plat := &msPlatform{}
	plat.script = func() { d.QuitWithCode(0) }
	d.RunOn(plat)

	if d.IsDesktopEnvironment() {
		t.Error("a host launched with its own application is a frame, not a desktop environment")
	}
}

// Revealing the desktop turns the frame into a desktop environment after the
// fact: asking for the desktop is asking for somewhere to go back TO. From
// then on the last window closing leaves the desktop showing.
func TestRevealingTheDesktopMakesItADesktopEnvironment(t *testing.T) {
	d := newRunnableDesktop(t)
	main := window.NewWindow("Solo")
	app := &mockApp{name: "Solo", main: main, windows: []*window.Window{main}}
	d.AddApplication(app)

	plat := &msPlatform{}
	d.SetOnStartup(func() {
		d.EnterSoloMode(main)
		if d.IsDesktopEnvironment() {
			t.Error("solo mode is the opposite of a desktop environment")
		}

		d.ExitSoloMode() // what show_desktop does on the graphical host
		if !d.IsDesktopEnvironment() {
			t.Fatal("revealing the desktop should make it a desktop environment")
		}

		d.quitApplication(app)
		if d.QuitRequested() {
			t.Error("the revealed desktop should outlive the app that revealed it")
		}
	})
	d.RunOn(plat)
}

// The other half of the same rule, one level down: the LAST WINDOW closing in
// solo mode ends a frame, and reveals a desktop environment instead of ending
// it.
func TestLastWindowClosedRevealsADesktopEnvironment(t *testing.T) {
	d := NewDesktop()
	d.mu.Lock()
	d.solo = true
	d.mu.Unlock()
	d.SetDesktopEnvironment(true)

	d.lastWindowClosed()

	if d.QuitRequested() {
		t.Fatal("a desktop environment must not quit when its last window closes")
	}
	if d.IsSolo() {
		t.Error("with nothing left to fill the display, solo mode should have ended")
	}
}

func TestLastWindowClosedEndsAFrame(t *testing.T) {
	d := NewDesktop()
	d.mu.Lock()
	d.solo = true
	d.mu.Unlock()

	d.lastWindowClosed()

	if !d.QuitRequested() {
		t.Error("a desktop that was only an application's frame ends with it")
	}
}

// A bare display server IS a desktop environment - until an app dials solo
// and takes the display over. That app now IS the display, so its last window
// closing ends the process rather than revealing a desktop it never had.
func TestSoloModeEndsDesktopEnvironmentStatus(t *testing.T) {
	d := newRunnableDesktop(t)
	plat := &msPlatform{}
	main := window.NewWindow("Solo")
	d.SetOnStartup(func() {
		if !d.IsDesktopEnvironment() {
			t.Fatal("a bare launch should start out a desktop environment")
		}
		d.EnterSoloMode(main)
		if d.IsDesktopEnvironment() {
			t.Error("an app taking the whole display ends desktop-environment status")
		}
		d.QuitWithCode(0)
	})
	d.RunOn(plat)
}
