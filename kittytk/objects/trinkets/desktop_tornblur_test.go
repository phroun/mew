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
