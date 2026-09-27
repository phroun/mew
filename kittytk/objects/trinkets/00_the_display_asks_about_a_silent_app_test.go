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
// on: the binding has a window and nothing else, and a test that hands the desktop
// the window directly skips the only step that can fail.
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

// **A plain refusal is not a pending answer, and a quit it cancelled stays
// cancelled.** A window whose own handler says no is refusing, full stop -- there is
// no application being consulted and nothing to wait for. Remembered as "waiting"
// instead, the quit would sit there, and the next close to resolve anywhere on the
// desktop would carry it out: the desktop quitting minutes later, out from under
// somebody who said not to.
func TestAPlainRefusalDoesNotLeaveAQuitWaiting(t *testing.T) {
	d, win := deskWithApp(t, "Ledger", "Quarterly Figures")
	win.SetOnClose(func() bool { return false })

	d.Quit()
	if d.QuitRequested() {
		t.Fatal("the window refused and the desktop quit anyway")
	}

	// The window goes some other way -- its handler dropped, so this close is the
	// plain one -- and now nothing is in the quit's way any more.
	win.SetOnClose(nil)
	win.Close()

	// A close resolving elsewhere is what a WAITING quit resumes on. This one is
	// not waiting; it was refused.
	d.CloseDecided(win, true)
	if d.QuitRequested() {
		t.Error("a later close carried out a quit that had been refused")
	}
}

// **An APPLICATION's own quit waits the same way a desktop quit does.** It sweeps its
// own windows and stops at the first that does not close, and a window waiting on an
// answer about closing is not refusing. Read as a refusal the application silently
// fails to quit -- nothing happens when you close it -- and then its window closes a
// moment later and it is left on the desktop with nothing open.
func TestAnApplicationQuitWaitsForAnAnswerToo(t *testing.T) {
	d, win := deskWithApp(t, "Ledger", "Quarterly Figures")
	app := d.Applications()[0]

	// A window that has not closed because an answer about closing it is still
	// coming, which is what the wire binding leaves behind while it asks.
	win.SetOnClose(func() bool { return false })
	win.SetDeciding(true)

	d.quitApplication(app)
	if len(d.Applications()) != 1 {
		t.Fatal("the application was taken off the desktop with a window still open")
	}
	d.mu.RLock()
	remembered := d.appQuitWanted[app]
	d.mu.RUnlock()
	if !remembered {
		t.Fatal("the quit was abandoned, so answering will not finish it and closing the app does nothing")
	}

	// The answer arrives and the window goes, which is what the window's own close
	// observer reports.
	win.SetOnClose(nil)
	win.SetDeciding(false)
	win.Close()
	d.CloseDecided(win, true)

	if len(d.Applications()) != 0 {
		t.Error("the answer arrived, the window went, and the application is still on the desktop")
	}
	// And the note is gone with it, rather than left behind keyed by an application
	// that is no longer on the desktop.
	d.mu.RLock()
	left := len(d.appQuitWanted)
	d.mu.RUnlock()
	if left != 0 {
		t.Errorf("%d application quits are still remembered after one finished", left)
	}
}

// **A refusal ends an application quit that was waiting.** Not "try again": going back
// round would put the same question to the application that has just refused, and
// then again on that answer.
func TestADeniedCloseEndsTheApplicationQuitItStopped(t *testing.T) {
	d, win := deskWithApp(t, "Ledger", "Quarterly Figures")
	app := d.Applications()[0]

	asked := 0
	win.SetOnClose(func() bool { asked++; return false })
	win.SetDeciding(true)
	d.quitApplication(app)

	d.mu.RLock()
	remembered := d.appQuitWanted[app]
	d.mu.RUnlock()
	if !remembered {
		t.Fatal("the quit was not remembered, so this proves nothing about ending it")
	}
	was := asked

	// Denied: the window stays, and the close is reported as not having happened.
	win.SetDeciding(false)
	d.CloseDecided(win, false)

	// **Not asked again.** This is the loop the outcome exists to prevent: sweeping
	// on a refusal puts the same question back to the application that just refused,
	// and then again on that answer.
	if asked != was {
		t.Errorf("the window was asked to close again (%d, was %d) on hearing its own refusal", asked, was)
	}

	d.mu.RLock()
	still := d.appQuitWanted[app]
	d.mu.RUnlock()
	if still {
		t.Error("the application quit is still waiting, so the next close would re-ask the application that refused")
	}
	if len(d.Applications()) != 1 {
		t.Error("the application quit over a window whose close was refused")
	}
}

// **One application's answer finishes one application's quit.** Two can be waiting at
// once, and a window settling in one of them says nothing about the other -- which
// would otherwise be torn off the desktop with its own window still open.
func TestOneApplicationsAnswerDoesNotQuitAnother(t *testing.T) {
	d, mine := deskWithApp(t, "Ledger", "Quarterly Figures")
	ledger := d.Applications()[0]

	theirs := window.NewWindow("Someone Else's Work")
	d.WindowManager().AddWindow(theirs)
	other := &mockApp{name: "Diary", windows: []*window.Window{theirs}}
	d.AddApplication(other)

	mine.SetOnClose(func() bool { return false })
	mine.SetDeciding(true)
	// Counted, because the sharpest way somebody else's answer can go wrong is not
	// the Diary quitting -- its own window is still deciding, so it would not -- but
	// the Diary being ASKED again about a window it has already been asked about.
	theirsAsked := 0
	theirs.SetOnClose(func() bool { theirsAsked++; return false })
	theirs.SetDeciding(true)

	d.quitApplication(ledger)
	d.quitApplication(other)
	asked := theirsAsked

	d.mu.RLock()
	both := len(d.appQuitWanted)
	d.mu.RUnlock()
	if both != 2 {
		t.Fatalf("%d application quits are waiting, want both", both)
	}

	// The Ledger's window is answered and goes. The Diary's is untouched.
	mine.SetOnClose(nil)
	mine.SetDeciding(false)
	mine.Close()
	d.CloseDecided(mine, true)

	names := map[string]bool{}
	for _, a := range d.Applications() {
		names[a.Name()] = true
	}
	if names["Ledger"] {
		t.Error("the Ledger answered and is still on the desktop")
	}
	if !names["Diary"] {
		t.Error("the Diary was quit by somebody else's answer, with its own window still open")
	}
	if !theirs.IsVisible() {
		t.Error("the Diary's window was closed by somebody else's answer")
	}
	if theirsAsked != asked {
		t.Errorf("the Diary was asked about closing its window again (%d, was %d) because another application answered",
			theirsAsked, asked)
	}
}

// And a REFUSAL abandons it, for the reason a desktop quit's does: going back round
// would put the same question to an application that has just answered it.
func TestARefusedApplicationQuitStaysAbandoned(t *testing.T) {
	d, win := deskWithApp(t, "Ledger", "Quarterly Figures")
	app := d.Applications()[0]

	win.SetOnClose(func() bool { return false }) // refusing, not deciding

	d.quitApplication(app)
	d.mu.RLock()
	remembered := d.appQuitWanted[app]
	d.mu.RUnlock()
	if remembered {
		t.Error("a refused application quit is remembered, so a later close would carry it out")
	}
	if len(d.Applications()) != 1 {
		t.Error("the application quit over a window that refused")
	}
}
