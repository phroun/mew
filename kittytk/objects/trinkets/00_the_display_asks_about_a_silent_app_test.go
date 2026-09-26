package trinkets

// The display's own question, when an application that asked to be consulted about
// a window closing has not answered.
//
// The display cannot tell an application that is thinking from one that is gone, so
// it asks the person, and the question has to say enough to be answerable: WHICH
// application, WHICH window, and what forcing it would cost.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// deskWithApp is a desktop with one application holding one window, which is the
// situation the question is asked in.
func deskWithApp(t *testing.T, appName, title string) (*Desktop, *window.Window) {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDesktop()
	d.SetBackend(px)

	win := window.NewWindow(title)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 400, Height: 200})
	d.WindowManager().AddWindow(win)
	d.AddApplication(&mockApp{name: appName, windows: []*window.Window{win}})
	return d, win
}

// onTheScreen reports whether the question is where the person can see it, which is
// the only place a question is any use.
func onTheScreen(d *Desktop) bool {
	for _, w := range d.WindowManager().Windows() {
		if w.Title() == "Not Responding" {
			return true
		}
	}
	return false
}

// **The question goes up, and it says who and what.** A dialog that said only "an
// application did not respond" would leave the person guessing which of five it was,
// and whether the window they are about to throw away holds their work.
func TestTheQuestionNamesTheApplicationAndTheWindow(t *testing.T) {
	d, win := deskWithApp(t, "Ledger", "Quarterly Figures")

	var got *bool
	d.AskForceClose(win, func(force bool) { got = &force })

	if !onTheScreen(d) {
		t.Fatal("no question was put to the person")
	}
	if got != nil {
		t.Errorf("the question answered itself: %v", *got)
	}

	// The words, read off the dialog the person is looking at.
	text := theDialogText(t, d, win)
	for _, want := range []string{"Ledger", "Quarterly Figures", "unsaved"} {
		if !strings.Contains(text, want) {
			t.Errorf("the question does not mention %q; it says: %s", want, text)
		}
	}
}

// Yes forces it; No does not. The desktop only reports the answer -- closing the
// window is the caller's to do, because it is the caller that knows what it was in
// the middle of.
func TestTheAnswerComesBackAsItWasGiven(t *testing.T) {
	for _, tc := range []struct {
		name  string
		click DialogResult
		force bool
	}{
		{"yes", ResultYes, true},
		{"no", ResultNo, false},
		// Dismissed without choosing -- Escape, or the dialog's own [x]. The safe
		// half, the same as the close it is about.
		{"dismissed", ResultNone, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, win := deskWithApp(t, "Ledger", "Quarterly Figures")
			var got *bool
			d.AskForceClose(win, func(force bool) { got = &force })

			mb := theBox(t, d, win)
			mb.done(tc.click)

			if got == nil {
				t.Fatal("the person answered and nobody was told")
			}
			if *got != tc.force {
				t.Errorf("%s came back force=%v, want %v", tc.name, *got, tc.force)
			}
		})
	}
}

// Answered once. A dialog that finishes twice -- a click landing as Escape is
// handled -- must not force a close the person declined.
func TestTheQuestionIsAnsweredOnce(t *testing.T) {
	d, win := deskWithApp(t, "Ledger", "Quarterly Figures")
	var answers []bool
	d.AskForceClose(win, func(force bool) { answers = append(answers, force) })

	mb := theBox(t, d, win)
	mb.done(ResultNo)
	mb.done(ResultYes)

	if len(answers) != 1 {
		t.Fatalf("the caller was told %d times: %v", len(answers), answers)
	}
	if answers[0] {
		t.Error("a second finish overruled the person's answer")
	}
}

// **One question per window.** Pressing [x] again while the person is reading this
// starts another close, which waits its own five seconds and would put up a SECOND
// dialog about the same window. The second press joins the answer the first is
// waiting for, and both are told it.
func TestASecondCloseJoinsTheQuestionAlreadyUp(t *testing.T) {
	d, win := deskWithApp(t, "Ledger", "Quarterly Figures")

	var first, second *bool
	d.AskForceClose(win, func(f bool) { first = &f })
	mb := theBox(t, d, win)
	d.AskForceClose(win, func(f bool) { second = &f })

	if got := theBox(t, d, win); got != mb {
		t.Error("a second dialog went up about the same window")
	}
	count := 0
	for _, w := range d.WindowManager().Windows() {
		if w.Title() == "Not Responding" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("%d questions are on the screen about one window", count)
	}

	mb.done(ResultYes)
	if first == nil || second == nil {
		t.Fatalf("both closes wanted the answer; first=%v second=%v", first, second)
	}
	if !*first || !*second {
		t.Errorf("the answer reached them as first=%v second=%v, want both forced", *first, *second)
	}
}

// And a caller that arrives after the person has answered is told what they said,
// rather than left waiting on an answer that has been and gone.
func TestALateJoinerIsToldWhatWasDecided(t *testing.T) {
	d, win := deskWithApp(t, "Ledger", "Quarterly Figures")
	d.AskForceClose(win, func(bool) {})
	mb := theBox(t, d, win)
	mb.done(ResultYes)

	var late *bool
	mb.alsoTell(func(f bool) { late = &f })
	if late == nil {
		t.Fatal("a caller arriving after the answer was left waiting for ever")
	}
	if !*late {
		t.Errorf("it was told force=%v, want what the person actually said", *late)
	}
}

// **The window finds the desktop by itself**, which is the path this actually runs
// on. A window's parent is the window MANAGER; the desktop is above that. So the
// walk has to go further than one level, and a test that hands the desktop the
// window directly never finds out.
func TestAWindowFindsTheDesktopToAskThrough(t *testing.T) {
	d, win := deskWithApp(t, "Ledger", "Quarterly Figures")

	var got *bool
	win.AskForceClose(func(force bool) { got = &force })

	if !onTheScreen(d) {
		t.Fatal("the window could not find anything to ask through, so a hung application holds it for ever")
	}
	if got != nil {
		t.Errorf("the question answered itself: %v", *got)
	}
	theBox(t, d, win).done(ResultYes)
	if got == nil || !*got {
		t.Errorf("the answer did not come back through the window: %v", got)
	}
}

// A desktop with nothing to show a dialog on has nowhere to ask, and says so as
// "not forced" -- the half of the answer that loses no work.
func TestWithNowhereToShowTheQuestionNothingIsForced(t *testing.T) {
	d := NewDesktop() // no backend, so no window manager
	if d.WindowManager() != nil {
		t.Skip("a bare desktop has a window manager; this branch is unreachable")
	}
	win := window.NewWindow("Quarterly Figures")

	var got *bool
	d.AskForceClose(win, func(force bool) { got = &force })
	if got == nil {
		t.Fatal("nobody could be asked and the caller was left waiting for ever")
	}
	if *got {
		t.Error("a question that could not be asked came back as the person saying yes")
	}
}

// An application the desktop does not know about is still worth asking about: the
// window is on the screen either way, and a question naming nobody is better than
// no question.
func TestAWindowWithNoApplicationIsStillAsked(t *testing.T) {
	d, _ := deskWithApp(t, "Ledger", "Quarterly Figures")
	orphan := window.NewWindow("Nobody's Window")
	d.WindowManager().AddWindow(orphan)

	d.AskForceClose(orphan, func(bool) {})
	if !onTheScreen(d) {
		t.Fatal("no question was put about a window with no application")
	}
	if text := theDialogText(t, d, orphan); !strings.Contains(text, "Nobody's Window") {
		t.Errorf("the question does not name the window; it says: %s", text)
	}
}

// theBox is the force-close dialog the desktop is holding about win, which is the
// same handle it uses to keep a second close attempt from putting up a second one.
func theBox(t *testing.T, d *Desktop, win *window.Window) *MessageBox {
	t.Helper()
	d.mu.RLock()
	mb := d.forceClose[win]
	d.mu.RUnlock()
	if mb == nil {
		t.Fatal("the desktop is not holding a force-close question about this window")
	}
	return mb
}

func theDialogText(t *testing.T, d *Desktop, win *window.Window) string {
	t.Helper()
	return theBox(t, d, win).content.text
}
