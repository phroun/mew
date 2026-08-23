package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// A torn SECONDARY window blurs to its app's main window. The main window is
// still docked here, so it is activated on the desktop's surface — and that
// surface has to be raised too, or the activation happens behind the torn
// window the user just left.
func TestTornSecondaryBlursToItsAppMainWindow(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("main")
	second := window.NewWindow("second")
	second.SetTearable(true)
	app := &mockApp{name: "App", main: main, windows: []*window.Window{main, second}}
	d.AddApplication(app)

	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
		wm.AddWindow(second)
		second.SetBounds(core.UnitRect{X: 20, Y: 20, Width: 200, Height: 150})
		second.Layout()
		wm.ActivateWindow(second)
	})

	plat := &msPlatform{}
	plat.script = func() {
		d.tearOffInPlace(second)
		if !second.IsDetached() {
			t.Fatal("the secondary window did not tear off")
		}
		desktopSurf := plat.surfaces[0]
		desktopSurf.raised = false

		d.BlurDetachedWindow(second)

		if got := d.WindowManager().ActiveWindow(); got != main {
			title := "<nil>"
			if got != nil {
				title = got.Title()
			}
			t.Errorf("active window after blur = %s, want main", title)
		}
		if !desktopSurf.raised {
			t.Error("the desktop surface was not raised, so the main window came up behind the torn one")
		}
		d.QuitWithCode(0)
	}

	d.RunOn(plat)
}

// A torn MAIN window has no app window above it, so its blur goes out to the
// desktop it was torn from.
func TestTornMainWindowBlursToTheDesktop(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("main")
	main.SetTearable(true)
	app := &mockApp{name: "App", main: main, windows: []*window.Window{main}}
	d.AddApplication(app)

	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		d.tearOffInPlace(main)
		if !main.IsDetached() {
			t.Fatal("the main window did not tear off")
		}
		desktopSurf := plat.surfaces[0]
		desktopSurf.raised = false

		d.BlurDetachedWindow(main)

		if !desktopSurf.raised {
			t.Error("blurring the torn main window did not return to the desktop")
		}
		d.QuitWithCode(0)
	}

	d.RunOn(plat)
}

// In SOLO mode the primary surface IS the application window: there is no
// desktop behind it to return to, so the main window's blur must not pretend
// otherwise. Raising the primary surface here would just re-raise the window
// the user is trying to leave.
func TestSoloMainWindowBlurHasNowhereToGo(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("Solo")
	app := &mockApp{name: "Solo", main: main, windows: []*window.Window{main}}
	d.AddApplication(app)

	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		d.EnterSoloMode(main)
		if !d.IsSolo() {
			t.Fatal("did not enter solo mode")
		}
		primary := plat.surfaces[0]
		primary.raised = false

		d.BlurDetachedWindow(main)

		if primary.raised {
			t.Error("solo blur re-raised the very surface the window lives on")
		}
		d.QuitWithCode(0)
	}

	d.RunOn(plat)
}

// Solo mode takes the tear handle away because there is nothing to dock back
// to. Revealing a desktop gives it back to every window that DECLARED itself
// tearable - the app's own main window and any child the app deliberately
// made tearable, which docks independently by design - while a follower that
// never declared it stays handle-less and travels with its app's main window.
//
// The peers matter as much as the primary: a second app opening its main
// window while solo is adopted onto its own surface with no handle, and
// before this it kept none for the rest of the session, unable to dock to the
// desktop that had just appeared.
func TestDesktopRevealRestoresDeclaredTearHandles(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("Main")
	main.SetTearable(true)
	otherMain := window.NewWindow("Second app") // declares itself tearable
	otherMain.SetTearable(true)
	follower := window.NewWindow("Palette") // never declares it
	app := &mockApp{name: "Solo", main: main, windows: []*window.Window{main}}
	d.AddApplication(app)

	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		wm := d.WindowManager()
		d.EnterSoloMode(main)

		// Both open while solo, so both are adopted onto their own surfaces
		// with the handle suppressed.
		for _, w := range []*window.Window{otherMain, follower} {
			app.windows = append(app.windows, w)
			w.SetBounds(core.UnitRect{X: 40, Y: 30, Width: 220, Height: 160})
			wm.AddWindow(w)
			w.Layout()
		}
		for _, w := range []*window.Window{main, otherMain, follower} {
			if w.IsTearable() {
				t.Errorf("%s kept a tear handle in solo mode", w.Title())
			}
		}

		// show_desktop: the desktop is revealed and there is somewhere to dock.
		d.ExitSoloMode()

		if !main.IsTearable() {
			t.Error("the solo main window did not get its tear handle back")
		}
		if !otherMain.IsTearable() {
			t.Error("a peer that declared itself tearable did not get its handle back")
		}
		if follower.IsTearable() {
			t.Error("a follower that never declared tearable was given a handle")
		}
		d.QuitWithCode(0)
	}

	d.RunOn(plat)
}

// The blur control is not offered where it would be inert. A solo app's main
// window has no app window above it and no desktop behind it; the moment a
// desktop is revealed the same question answers yes, with no state to reset.
func TestSoloMainWindowOffersNoBlurUntilThereIsADesktop(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("Solo")
	main.SetTearable(true)
	app := &mockApp{name: "Solo", main: main, windows: []*window.Window{main}}
	d.AddApplication(app)

	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
	})

	plat := &msPlatform{}
	plat.script = func() {
		d.EnterSoloMode(main)
		if d.CanBlurDetachedWindow(main) {
			t.Error("solo main window offers a blur item with nowhere to blur to")
		}
		d.ExitSoloMode()
		if !d.CanBlurDetachedWindow(main) {
			t.Error("blur item did not return once a desktop was revealed")
		}
		d.QuitWithCode(0)
	}

	d.RunOn(plat)
}
