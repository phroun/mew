package trinkets

// The desktop's own close button.
//
// **Closing the desktop is not closing a window.** It ends every application running on
// it -- the Program Manager rather than a program -- and the button that does it sits in
// the corner of a window next to the ones that minimize and maximize it, which is a
// button in the wrong place for what it does. So it asks first, and names the cost in
// the only terms that make it a decision: how many applications go with it.

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
