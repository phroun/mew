package trinkets

// The desktop's own close button, and the system menu's Exit Desktop item.
//
// **It ends the desktop, not the session.** An application torn out onto a surface of
// its own is not inside the desktop and does not go with it. What is at stake is the
// applications DOCKED in it, which have nowhere to be once it is gone -- so it asks
// about exactly those, and offers to give them windows of their own instead of closing
// them.
//
// The other file's AskBeforeQuitting is a different question: the OS asking to quit
// everything. This one is about the desktop.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// desktopWithApps is a desktop with n applications on it, one window each.
func desktopWithApps(t *testing.T, names ...string) (*Desktop, []*window.Window) {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	var wins []*window.Window
	for _, name := range names {
		w := window.NewWindow(name + " Window")
		wins = append(wins, w)
		d.AddApplication(&mockApp{name: name, main: w, windows: []*window.Window{w}})
	}
	d.SetOnStartup(func() {
		wm := d.WindowManager()
		for _, w := range wins {
			wm.AddWindow(w)
			w.SetBounds(core.UnitRect{X: 40, Y: 40, Width: 300, Height: 200})
		}
	})
	return d, wins
}

// theQuitBox is the "exit the desktop?" question while it is up.
func theQuitBox(t *testing.T, d *Desktop) *MessageBox {
	t.Helper()
	d.mu.RLock()
	mb := d.quitConfirm
	d.mu.RUnlock()
	if mb == nil {
		t.Fatal("the desktop is not asking about exiting")
	}
	return mb
}

// **It asks, and nothing has happened yet.** The applications are still running while
// the question is on the screen.
func TestExitingTheDesktopAsksFirst(t *testing.T) {
	d, wins := desktopWithApps(t, "Ledger", "Diary", "Demo")
	plat := &msPlatform{}
	plat.script = func() {
		d.AskBeforeQuitting(0)

		if d.QuitRequested() {
			t.Fatal("the desktop quit without asking")
		}
		for _, w := range wins {
			if !w.IsVisible() {
				t.Errorf("%q was closed before anybody agreed to it", w.Title())
			}
		}
		// And it says how many applications go with it, which is the whole of what
		// makes this a decision rather than a click.
		text := theQuitBox(t, d).content.text
		for _, want := range []string{"3", "applications"} {
			if !strings.Contains(text, want) {
				t.Errorf("the question does not mention %q; it says: %s", want, text)
			}
		}
		d.ForceQuitWithCode(0)
	}
	d.RunOn(plat)
}

// One application is one application, not "1 applications".
func TestTheQuestionCountsInPlainWords(t *testing.T) {
	d, _ := desktopWithApps(t, "Ledger")
	plat := &msPlatform{}
	plat.script = func() {
		d.AskBeforeQuitting(0)
		text := theQuitBox(t, d).content.text
		if !strings.Contains(text, "1 running application.") {
			t.Errorf("the question reads: %s", text)
		}
		d.ForceQuitWithCode(0)
	}
	d.RunOn(plat)
}

// Yes, and the sweep begins.
func TestSayingYesQuitsTheDesktop(t *testing.T) {
	d, _ := desktopWithApps(t, "Ledger", "Diary")
	plat := &msPlatform{}
	plat.script = func() {
		d.AskBeforeQuitting(0)
		theQuitBox(t, d).done(ResultYes)
		if !d.QuitRequested() {
			t.Error("the answer was yes and the desktop did not quit")
		}
	}
	d.RunOn(plat)
}

// No, and nothing happens at all -- which is the point of asking.
func TestSayingNoLeavesEverythingRunning(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer DialogResult
	}{
		{"no", ResultNo},
		// Dismissed without choosing: Escape, or the dialog's own [x].
		{"dismissed", ResultNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, wins := desktopWithApps(t, "Ledger", "Diary")
			plat := &msPlatform{}
			plat.script = func() {
				d.AskBeforeQuitting(0)
				theQuitBox(t, d).done(tc.answer)

				if d.QuitRequested() {
					t.Error("the desktop quit although the answer was no")
				}
				for _, w := range wins {
					if !w.IsVisible() {
						t.Errorf("%q was closed although the answer was no", w.Title())
					}
				}
				d.ForceQuitWithCode(0)
			}
			d.RunOn(plat)
		})
	}
}

// **Nothing running is nothing to warn about.** A question with no cost in it is a click
// for its own sake.
func TestAnEmptyDesktopQuitsWithoutAsking(t *testing.T) {
	d, _ := desktopWithApps(t)
	plat := &msPlatform{}
	plat.script = func() {
		d.AskBeforeQuitting(0)
		if !d.QuitRequested() {
			t.Error("an empty desktop asked a question nobody needed and did not quit")
		}
	}
	d.RunOn(plat)
}

// An application with no windows on the desktop is not something exiting it would close,
// so it is not counted and does not by itself make the question worth asking.
func TestApplicationsWithNoWindowsAreNotCounted(t *testing.T) {
	d, _ := desktopWithApps(t, "Ledger")
	d.AddApplication(&mockApp{name: "Headless"})
	plat := &msPlatform{}
	plat.script = func() {
		d.AskBeforeQuitting(0)
		text := theQuitBox(t, d).content.text
		if !strings.Contains(text, "1 running application.") {
			t.Errorf("an application with nothing open was counted; it says: %s", text)
		}
		d.ForceQuitWithCode(0)
	}
	d.RunOn(plat)
}

// **One question, however many times the button is pressed.**
func TestPressingTheCloseButtonAgainAsksOnce(t *testing.T) {
	d, _ := desktopWithApps(t, "Ledger", "Diary")
	plat := &msPlatform{}
	plat.script = func() {
		d.AskBeforeQuitting(0)
		first := theQuitBox(t, d)
		d.AskBeforeQuitting(0)
		if theQuitBox(t, d) != first {
			t.Error("a second question went up about the same exit")
		}
		count := 0
		for _, w := range d.WindowManager().Windows() {
			if w.Title() == "Exit Desktop" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("%d exit questions are on the screen", count)
		}
		d.ForceQuitWithCode(0)
	}
	d.RunOn(plat)
}

// And after an answer the button asks again, rather than being spent.
func TestTheQuestionComesBackAfterNo(t *testing.T) {
	d, _ := desktopWithApps(t, "Ledger")
	plat := &msPlatform{}
	plat.script = func() {
		d.AskBeforeQuitting(0)
		theQuitBox(t, d).done(ResultNo)

		d.AskBeforeQuitting(0)
		theQuitBox(t, d) // fatals if it is not asking again
		d.ForceQuitWithCode(0)
	}
	d.RunOn(plat)
}

// dockedAndTorn is a desktop with one application docked inside it and one torn out onto
// a surface of its own -- the shape the question is about.
func dockedAndTorn(t *testing.T) (*Desktop, *window.Window, *window.Window, *msPlatform, func(func())) {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 480)
	d := NewDesktop()
	d.SetBackend(px)

	torn := window.NewWindow("mew")
	torn.SetMainRequested(true)
	torn.SetTearable(true)
	d.AddApplication(&mockApp{name: "mew", main: torn, windows: []*window.Window{torn}})

	docked := window.NewWindow("KittyTK Demo")
	d.AddApplication(&mockApp{name: "Demo", main: docked, windows: []*window.Window{docked}})

	d.SetOnStartup(func() {
		wm := d.WindowManager()
		for _, w := range []*window.Window{torn, docked} {
			wm.AddWindow(w)
			w.SetBounds(core.UnitRect{X: 40, Y: 40, Width: 300, Height: 200})
		}
	})

	plat := &msPlatform{}
	run := func(body func()) {
		plat.script = func() {
			d.tearOffInPlace(torn)
			if !torn.IsDetached() || docked.IsDetached() {
				t.Fatal("harness: one torn out and one docked is the shape this is about")
			}
			body()
			d.ForceQuitWithCode(0)
		}
		d.RunOn(plat)
	}
	return d, docked, torn, plat, run
}

// **It counts what it would close, which is what is docked.** The torn-out application
// keeps running either way, so warning about it would be warning about nothing.
func TestTheQuestionCountsOnlyWhatIsInsideTheDesktop(t *testing.T) {
	d, _, _, _, run := dockedAndTorn(t)
	run(func() {
		d.ExitDesktop()
		text := theQuitBox(t, d).content.text
		if !strings.Contains(text, "1 running application.") {
			t.Errorf("the question counts the torn-out application too; it says: %s", text)
		}
	})
}

// Yes closes what was inside, and the desktop goes -- leaving the torn-out application
// running on the primary surface.
func TestYesClosesWhatWasInsideAndLeavesTheRest(t *testing.T) {
	d, docked, torn, plat, run := dockedAndTorn(t)
	run(func() {
		d.ExitDesktop()
		theQuitBox(t, d).done(ResultYes)

		if docked.IsVisible() {
			t.Error("the application inside the desktop is still open")
		}
		if !torn.IsVisible() {
			t.Error("the torn-out application was closed by the desktop exiting")
		}
		if !d.IsSolo() {
			t.Error("the desktop did not close")
		}
		if plat.quitCalled {
			t.Error("the host quit although an application was still running")
		}
	})
}

// **Pop It Out is the lossless answer**: the application inside gets a window of its own
// and keeps running, and the desktop goes anyway.
func TestPopOutGivesTheApplicationAWindowOfItsOwn(t *testing.T) {
	d, docked, torn, _, run := dockedAndTorn(t)
	run(func() {
		d.ExitDesktop()
		mb := theQuitBox(t, d)
		mb.done(ResultPopOut)

		if !docked.IsVisible() {
			t.Fatal("popping it out closed it")
		}
		if !docked.IsDetached() {
			t.Error("it was not given a surface of its own, so it is on a desktop that has gone")
		}
		if !torn.IsVisible() {
			t.Error("the torn-out application was closed")
		}
		if !d.IsSolo() {
			t.Error("the desktop did not close")
		}
	})
}

// And it is named for what it is about: one application is "It", several are "Them".
func TestThePopOutButtonNamesWhatItWouldDo(t *testing.T) {
	d, _, _, _, run := dockedAndTorn(t)
	run(func() {
		d.ExitDesktop()
		mb := theQuitBox(t, d)
		var labels []string
		for _, b := range mb.content.buttonTrinkets {
			labels = append(labels, b.Text())
		}
		joined := strings.Join(labels, "|")
		if !strings.Contains(joined, "Pop It Out") {
			t.Errorf("the buttons read %s", joined)
		}
	})
}

// No leaves everything exactly as it was, desktop included.
func TestNoLeavesTheDesktopAlone(t *testing.T) {
	d, docked, torn, _, run := dockedAndTorn(t)
	run(func() {
		d.ExitDesktop()
		theQuitBox(t, d).done(ResultNo)

		if d.IsSolo() {
			t.Error("the desktop closed although the answer was no")
		}
		for _, w := range []*window.Window{docked, torn} {
			if !w.IsVisible() {
				t.Errorf("%q was closed although the answer was no", w.Title())
			}
		}
	})
}

// **Pop Out is offered only where a window can have a surface of its own.** On a host
// that holds one, there is nowhere to pop out to and the offer would be a lie.
func TestPopOutIsNotOfferedWithNowhereToPopOutTo(t *testing.T) {
	d, wins := desktopWithApps(t, "Ledger")
	ready := make(chan struct{})
	d.SetOnStartup(func() {
		wm := d.WindowManager()
		for _, w := range wins {
			wm.AddWindow(w)
			w.SetBounds(core.UnitRect{X: 40, Y: 40, Width: 300, Height: 200})
		}
		close(ready)
	})
	// Run() builds a polling platform: one surface, and not a native one.
	go d.Run()
	<-ready
	defer d.ForceQuitWithCode(0)

	done := make(chan struct{})
	d.Post(func() {
		defer close(done)
		if d.canTearOff() {
			t.Fatal("harness: this platform can hold a second surface")
		}
		d.ExitDesktop()
		mb := theQuitBox(t, d)
		for _, b := range mb.content.buttonTrinkets {
			if strings.Contains(b.Text(), "Pop") {
				t.Errorf("the question offers %q on a host with nowhere to pop out to", b.Text())
			}
		}
	})
	<-done
}
