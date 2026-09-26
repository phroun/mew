package trinkets

// A dialog of the display's OWN, when the desktop is not on the screen.
//
// The display puts up its own windows -- "did not respond, force close?", waiting for
// the clipboard -- by adding them to the window manager, which paints them on the
// desktop's surface. In SOLO mode there is no desktop on that surface: it has been
// given over to one application's window. So the dialog went up, and modally blocked,
// where nobody was looking.
//
// That is the worst shape a question can have. A window that will not close is at
// least visibly stuck; a question nobody can see explains nothing and cannot be
// answered.

import (
	"testing"
	"time"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// soloDesktop is a desktop running one application as the whole display, on a
// platform that can hold more than one OS surface.
func soloDesktop(t *testing.T) (*Desktop, *window.Window, *msPlatform, func(func())) {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("Solo")
	d.AddApplication(&mockApp{name: "Solo", main: main, windows: []*window.Window{main}})
	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 100, Y: 100, Width: 300, Height: 200})
		main.Layout()
	})

	plat := &msPlatform{}
	run := func(body func()) {
		plat.script = func() {
			d.EnterSoloMode(main)
			if !d.IsSolo() {
				t.Fatal("the desktop is not in solo mode, so this proves nothing")
			}
			body()
			d.ForceQuitWithCode(0)
		}
		d.RunOn(plat)
	}
	return d, main, plat, run
}

// **With the desktop hidden, asking shows the desktop.**
//
// There is no desktop behind the application, so a question left on the desktop's
// surface would be painted where nobody is looking. Showing the desktop is what
// show_desktop does and is the way back a person already has, so the question arrives
// somewhere they know, alongside everything else that was on the desktop, rather than
// as one lone window floating over an application.
func TestAskingWithTheDesktopHiddenShowsTheDesktop(t *testing.T) {
	d, main, _, run := soloDesktop(t)

	run(func() {
		var answered *bool
		d.AskForceClose(main, func(force bool) { answered = &force })

		if answered != nil {
			t.Fatalf("the question answered itself: %v", *answered)
		}
		if d.IsSolo() {
			t.Fatal("the desktop is still hidden, so the question is painted where nobody can see it")
		}
		if !onTheScreen(d) {
			t.Error("the desktop was shown and the question is not on it")
		}
		// The application that was filling the display is still there -- re-homed as
		// a window on the revealed desktop, not closed to make room for a dialog.
		if !main.IsVisible() {
			t.Error("showing the desktop closed the application that was filling it")
		}
	})
}

// And it is answerable, which is the whole point of showing the desktop for it.
func TestAQuestionOnAShownDesktopAnswers(t *testing.T) {
	d, main, _, run := soloDesktop(t)

	run(func() {
		var answered *bool
		d.AskForceClose(main, func(force bool) { answered = &force })
		theBox(t, d, main).done(ResultYes)

		if answered == nil {
			t.Fatal("the person answered and nobody was told")
		}
		if !*answered {
			t.Error("the answer arrived as no, and yes was given")
		}
	})
}

// A desktop whose own surface is MINIMIZED is a smaller version of the same problem:
// the dialog is on it, and it is not on the screen. Restored and raised, or the
// person is being asked inside a taskbar button.
func TestAMinimizedDesktopIsRestoredToAsk(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	win := window.NewWindow("Quarterly Figures")
	d.AddApplication(&mockApp{name: "Ledger", windows: []*window.Window{win}})
	d.SetOnStartup(func() {
		d.WindowManager().AddWindow(win)
		win.SetBounds(core.UnitRect{X: 40, Y: 40, Width: 300, Height: 200})
	})

	plat := &msPlatform{}
	plat.script = func() {
		primary := plat.surfaces[0]
		primary.Minimize()

		d.AskForceClose(win, func(bool) {})

		if primary.minimized {
			t.Error("the desktop is still minimized, so the question is inside a taskbar button")
		}
		if !primary.raised {
			t.Error("the desktop was not raised, so the question may be behind everything else")
		}
		d.ForceQuitWithCode(0)
	}
	d.RunOn(plat)
}

// **A question about a window that has gone comes down by itself.** The application
// may destroy the window, or disconnect and have it closed for it, while somebody is
// reading "force this closed?". Left up, that is a MODAL dialog blocking the desktop
// and asking about a window nobody can point at -- and there is no answer to it.
func TestAQuestionAboutAWindowThatWentComesDown(t *testing.T) {
	d, win := deskWithApp(t, "Ledger", "Quarterly Figures")

	var answered *bool
	d.AskForceClose(win, func(force bool) { answered = &force })
	if !onTheScreen(d) {
		t.Fatal("no question was put, so there is nothing to take down")
	}

	// The close is settled some other way, which is what the window's own close
	// observer reports.
	d.CloseDecided(win, true)

	if onTheScreen(d) {
		t.Error("the question is still up, asking whether to force closed a window that has already gone")
	}
	if answered == nil {
		t.Fatal("whoever was waiting on the answer was never told, and waits for ever")
	}
	if *answered {
		t.Error("reported as forced, and nobody forced anything -- it went on its own")
	}
	// And the desktop is no longer holding it, so the next close asks afresh rather
	// than joining an answer that has been and gone.
	d.mu.RLock()
	held := d.forceClose[win]
	d.mu.RUnlock()
	if held != nil {
		t.Error("the desktop still holds the question it just took down")
	}
}

// **A desktop nothing could be covering is not raised.** Where every window is
// in-surface, the desktop IS the only surface, and raising it to show a dialog on it
// would be a focus grab that changes nothing a person can see.
func TestAnUncoveredDesktopIsNotRaisedToAsk(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	win := window.NewWindow("Quarterly Figures")
	d.AddApplication(&mockApp{name: "Ledger", windows: []*window.Window{win}})
	d.SetOnStartup(func() {
		d.WindowManager().AddWindow(win)
		win.SetBounds(core.UnitRect{X: 40, Y: 40, Width: 300, Height: 200})
	})

	plat := &msPlatform{}
	plat.script = func() {
		d.AskForceClose(win, func(bool) {})

		if !onTheScreen(d) {
			t.Fatal("no question went up at all")
		}
		if plat.surfaces[0].raised {
			t.Error("the desktop grabbed focus to show a dialog on the only surface there is")
		}
		d.ForceQuitWithCode(0)
	}
	d.RunOn(plat)
}

// **The desktop HIDDEN, rather than never shown.** Reveal the desktop, run a second
// application on it, hide the desktop again -- mew's show_desktop and hide_desktop,
// which is how this was met.
//
// It is a different road into solo mode from EnterSoloMode: a window is PROMOTED onto
// the primary surface rather than the desktop being reshaped around one. What asking a
// question needs from it is the same, so this is here to say so rather than assume it.
func TestAQuestionSurvivesTheDesktopBeingHidden(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("Ledger")
	main.SetMainRequested(true)
	main.SetTearable(true)
	sulking := window.NewWindow("Quarterly Figures")
	d.AddApplication(&mockApp{
		name: "Ledger", main: main,
		windows: []*window.Window{main, sulking},
	})
	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 20, Y: 20, Width: 400, Height: 300})
		wm.AddWindow(sulking)
		sulking.SetBounds(core.UnitRect{X: 60, Y: 60, Width: 300, Height: 200})
	})

	plat := &msPlatform{}
	plat.script = func() {
		// Torn first, so hiding the desktop has a window to promote -- which is what
		// show_desktop leaves behind and hide_desktop picks up.
		d.tearOffInPlace(main)
		d.EnterSoloFromDesktop()
		if !d.IsSolo() {
			t.Fatal("the desktop was not hidden, so this proves nothing")
		}

		var answered *bool
		d.AskForceClose(sulking, func(force bool) { answered = &force })

		if d.IsSolo() {
			t.Fatal("the desktop is still hidden, so the question is painted where nobody can see it")
		}
		if !onTheScreen(d) {
			t.Fatal("the desktop was shown and the question is not on it")
		}
		if answered != nil {
			t.Errorf("the question answered itself: %v", *answered)
		}
		theBox(t, d, sulking).done(ResultYes)
		if answered == nil || !*answered {
			t.Errorf("the question could not be answered: %v", answered)
		}
		d.ForceQuitWithCode(0)
	}
	d.RunOn(plat)
}

// **A desktop hidden on a host that cannot hand over its surface.**
//
// A single-surface host -- a terminal, headless polling -- has no surface to give an
// application, so hiding the desktop leaves its windows docked and means only that the
// desktop is the frame around one application. That is a real state and not a mistake.
//
// What WAS a mistake is that it could not be left again. ExitSoloMode returned early
// without a host behind the solo mode, so `solo` stayed true for ever -- and every path
// that reveals the desktop in order to show something then silently did nothing,
// including the one that puts a force-close question where a person can see it. Hide the
// desktop, try to close an application, and from then on no desktop, no question, and no
// way back to either.
func TestADesktopHiddenWithNoSurfaceToGiveComesBack(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	main := window.NewWindow("Ledger")
	main.SetMainRequested(true)
	sulking := window.NewWindow("Quarterly Figures")
	d.AddApplication(&mockApp{
		name: "Ledger", main: main,
		windows: []*window.Window{main, sulking},
	})

	// Run() builds a polling platform over the backend: one surface, and not a
	// native one, so there is nothing to hand over.
	ready := make(chan struct{})
	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(main)
		main.SetBounds(core.UnitRect{X: 20, Y: 20, Width: 400, Height: 300})
		wm.AddWindow(sulking)
		sulking.SetBounds(core.UnitRect{X: 60, Y: 60, Width: 300, Height: 200})
		close(ready)
	})
	go d.Run()
	<-ready
	defer d.ForceQuitWithCode(0)

	onThread := func(fn func()) {
		t.Helper()
		done := make(chan struct{})
		d.Post(func() { defer close(done); fn() })
		<-done
	}

	onThread(func() { d.EnterSoloFromDesktop() }) // hide_desktop
	if !d.IsSolo() {
		t.Fatal("the desktop was not hidden, so this proves nothing")
	}

	// And now something has to be asked about closing a window.
	var answered *bool
	onThread(func() { d.AskForceClose(sulking, func(force bool) { answered = &force }) })

	// Revealing the desktop is posted, so give the queue a chance to run it.
	deadline := time.Now().Add(5 * time.Second)
	for d.IsSolo() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if d.IsSolo() {
		t.Fatal("the desktop could not be revealed, so the question is painted where nobody can see it and there is no way back")
	}
	// **And it is a place in its own right again.** Revealing the desktop is asking
	// for somewhere to go back to, so the last window closing now leaves the desktop
	// showing rather than ending the process -- the same as the hosted path.
	if !d.IsDesktopEnvironment() {
		t.Error("the revealed desktop is still only the frame around an application, so closing the last window would end it")
	}

	var up bool
	onThread(func() { up = onTheScreen(d) })
	if !up {
		t.Fatal("the desktop came back and the question is not on it")
	}
	if answered != nil {
		t.Errorf("the question answered itself: %v", *answered)
	}
	onThread(func() { theBox(t, d, sulking).done(ResultYes) })
	if answered == nil || !*answered {
		t.Errorf("the question could not be answered: %v", answered)
	}
}

// **Asking hands the display to nobody.** Showing the desktop retires the primary host
// -- that is what leaving solo mode means -- and what must NOT happen is some other
// application taking it over. An application promoted onto the primary surface because a
// dialog appeared has been moved for no reason a person could explain.
//
// Run on a platform that behaves like SDL: Post defers, and creating a surface drains
// the queue from inside, which is the re-entrancy createTornHost's own claim guards.
// Every earlier test of this ran with both off, which is why none could see it.
func TestAskingHandsTheDisplayToNobody(t *testing.T) {
	d, main, plat, run := soloDesktop(t)

	run(func() {
		plat.deferPosts = true
		plat.reentrantCreate = true
		defer func() {
			plat.deferPosts, plat.reentrantCreate = false, false
			plat.drainPosts()
		}()

		if d.soloPrimaryHost == nil || d.soloPrimaryHost.Window() != main {
			t.Fatalf("harness: the primary host is not the solo window")
		}

		d.AskForceClose(main, func(bool) {})
		plat.drainPosts()

		if d.IsSolo() {
			t.Fatal("the desktop is still hidden")
		}
		if h := d.soloPrimaryHost; h != nil {
			var who string
			if h.Window() != nil {
				who = h.Window().Title()
			}
			t.Errorf("asking the question handed the display to %q", who)
		}
		// And the application that was filling it is still open, re-homed onto the
		// revealed desktop rather than closed to make room.
		if !main.IsVisible() {
			t.Error("the application that was filling the display was closed")
		}
	})
}

// **Revealing the desktop gives the surface back even with nothing to re-home.**
//
// The solo window can close before the desktop is revealed: its [x] is pressed, its
// host is dropped, soloPrimaryHost goes nil, and whatever reveals the desktop next --
// the host's own shutdown, say -- arrives with no window to put back.
//
// Giving the SURFACE back does not depend on that. Until its handler points at the
// desktop, it goes on painting through the host that was dropped: a dead frame of a
// window that has gone. Every flag says the desktop is revealed, nothing on the screen
// agrees, and clicking the one thing still showing does nothing at all -- which is
// exactly how this was reported.
func TestRevealingTheDesktopGivesTheSurfaceBackWithNothingToReHome(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	solo := window.NewWindow("mew")
	solo.SetMainRequested(true)
	// A peer, so closing the solo window is not the last window closing -- that ends
	// the process and is a different story.
	peer := window.NewWindow("Sulking Window")
	d.AddApplication(&mockApp{name: "mew", main: solo, windows: []*window.Window{solo}})
	d.AddApplication(&mockApp{name: "Demo", windows: []*window.Window{peer}})
	d.SetOnStartup(func() {
		wm := d.WindowManager()
		wm.AddWindow(solo)
		solo.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 480})
		wm.AddWindow(peer)
		peer.SetBounds(core.UnitRect{X: 40, Y: 40, Width: 300, Height: 200})
	})

	plat := &msPlatform{}
	plat.script = func() {
		d.EnterSoloMode(solo)
		primary := plat.surfaces[0]
		if d.soloPrimaryHost == nil {
			t.Fatal("harness: nothing is hosted on the primary surface")
		}

		// The solo window's own [x].
		solo.Close()
		if d.soloPrimaryHost != nil {
			t.Fatal("harness: the host was not dropped, so there is still something to re-home")
		}

		// And now something reveals the desktop, with nothing to put back.
		d.ExitSoloMode()

		if _, ok := primary.handler.(*desktopSurfaceHandler); !ok {
			t.Errorf("the surface still paints through %T, so the screen shows the window that closed",
				primary.handler)
		}
		if !primary.raised {
			t.Error("the revealed desktop was never brought to the front")
		}
		if d.IsSolo() {
			t.Error("the desktop still says it is hidden")
		}
		d.ForceQuitWithCode(0)
	}
	d.RunOn(plat)
}
