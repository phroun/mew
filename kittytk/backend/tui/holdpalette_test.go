package tui

import (
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
)

func held(wait time.Duration) *TUIBackend {
	return &TUIBackend{
		metrics:    core.DefaultCellMetrics(),
		eventQueue: make(chan core.Event, 32),
		holdWait:   wait,
	}
}

func drain(b *TUIBackend) []core.Event {
	var got []core.Event
	for {
		select {
		case ev := <-b.eventQueue:
			got = append(got, ev)
		default:
			return got
		}
	}
}

// A held TEXT key's repeats wait, so a press-and-hold palette can be used.
//
// macOS opens the palette on the hold and the terminal keeps repeating from
// behind it, so the letter types itself over and over while the palette is up
// and there is no moment early enough to let go in. Without the wait the
// palette is not untidy — it cannot be used at all.
func TestAHeldTextKeyWithholdsItsRepeats(t *testing.T) {
	b := held(time.Second)

	b.handleKey("o")        // the press types, always
	b.handleKey("o:Repeat") // and the palette opens about here
	b.handleKey("o:Repeat")
	b.handleKey("o:Repeat")

	got := drain(b)
	if len(got) != 1 {
		t.Fatalf("dispatched %d events, want only the press: %+v", len(got), got)
	}
	if kp, ok := got[0].(core.KeyPressEvent); !ok || kp.Repeat {
		t.Errorf("dispatched %#v, want the plain press", got[0])
	}
}

// Past the wait a hold means what a hold usually means, and the repeats flow.
func TestRepeatsFlowOnceTheWaitHasPassed(t *testing.T) {
	b := held(10 * time.Millisecond)

	b.handleKey("o") // the press starts the hold
	b.handleKey("o:Repeat")
	time.Sleep(25 * time.Millisecond)
	b.handleKey("o:Repeat")

	got := drain(b)
	if len(got) != 2 {
		t.Fatalf("dispatched %d events, want the press and the one past the "+
			"wait: %+v", len(got), got)
	}
	if kp, ok := got[1].(core.KeyPressEvent); !ok || !kp.Repeat {
		t.Errorf("dispatched %#v, want a repeat", got[1])
	}
}

// The palette's RESULT is a commit, and it arrives behind the take-back.
//
// The terminal reports the chosen character in whatever shape it likes — on the
// key that confirmed it, on a keycode nobody touched, or as bare text — and the
// key handler marks all of them with the "Text:" prefix, because no modifier is
// held and no cap on the keyboard says ö. Withholding it is indistinguishable
// from the palette doing nothing: the original letter is all that is left.
func TestThePaletteResultIsACommitBehindTheTakeBack(t *testing.T) {
	b := held(time.Hour) // long enough that anything subject to it stays put

	b.handleKey("o")        // the hold that opens the palette
	b.handleKey("o:Repeat") // withheld, correctly
	b.handleKey("Text:ö")   // and this is what was chosen out of it

	got := drain(b)
	if len(got) != 3 {
		t.Fatalf("dispatched %d events, want the press, the take-back and the "+
			"commit: %+v", len(got), got)
	}
	if c, ok := got[2].(core.TextCommitEvent); !ok || c.Text != "ö" {
		t.Errorf("dispatched %#v, want the chosen ö", got[2])
	}
}

// The letter the hold typed comes back OUT, so choosing from the palette
// replaces it rather than following it.
//
// A terminal reports no composition, so unlike the graphical host — where the
// base letter sits inside the commit's Covers and is never really typed — the
// letter is in the document by the time anything reveals a palette was open
// over it. Left there, holding "o" and choosing ö reads "oö".
func TestTheLetterAPaletteOpenedOverIsTakenBack(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat") // withheld — the only sign a palette opened
	b.handleKey("Text:ö")

	got := drain(b)
	if len(got) < 2 {
		t.Fatalf("dispatched %+v", got)
	}
	erase, ok := got[1].(core.TextEraseEvent)
	if !ok {
		t.Fatalf("second event %#v, want the take-back before the commit", got[1])
	}
	if erase.Count != 1 {
		t.Errorf("erased %d runes, want the one letter the hold typed", erase.Count)
	}
}

// Letting go is how the palette gets USED, so the release cannot end the
// take-back. The terminal reports it before the choice is even made — in a
// capture of this gesture the "o" release arrives ahead of the arrow keys that
// walk the palette.
func TestTheTakeBackOutlivesTheRelease(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat")
	b.handleKey("o:Release")
	b.handleKey("Text:ö")

	for _, ev := range drain(b) {
		if _, ok := ev.(core.TextEraseEvent); ok {
			return
		}
	}
	t.Error("no take-back after the release; the letter the palette opened " +
		"over would be left standing in front of the chosen one")
}

// Nothing withheld, nothing to take back. A dead key composes without any hold
// at all, and erasing on its account would eat whatever preceded it.
func TestNothingIsTakenBackWithoutAHold(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("Text:û")

	for _, ev := range drain(b) {
		if _, ok := ev.(core.TextEraseEvent); ok {
			t.Fatal("took a rune back for a composition that opened over nothing")
		}
	}
}

// The palette's SELECTOR comes back out with the letter.
//
// Choosing by number types the digit into the document — "CSI 52;;52u", the 4
// key reporting that it typed a 4 — and macOS expects it replaced along with
// the base letter, without ever asking for it back. Take back only the letter
// and the digit is left behind: holding "o" and picking option 4 read "o4ö".
func TestTheSelectorTypedIntoThePaletteComesBackToo(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat") // withheld — the palette opened
	b.handleKey("4")        // its selector, typed into the document
	b.handleKey("Text:ö")

	for _, ev := range drain(b) {
		if erase, ok := ev.(core.TextEraseEvent); ok {
			if erase.Count != 2 {
				t.Errorf("erased %d runes, want the letter and the selector",
					erase.Count)
			}
			return
		}
	}
	t.Error("no take-back at all")
}

// A key that types NOTHING ends it: Escape dismissing the palette leaves the
// letter it opened over standing, and nothing is coming to replace it.
func TestAKeyThatTypesNothingEndsTheTakeBack(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat") // withheld
	b.handleKey("Escape")   // the palette dismissed
	b.handleKey("Text:ö")   // some later composition, on its own account

	for _, ev := range drain(b) {
		if _, ok := ev.(core.TextEraseEvent); ok {
			t.Fatal("took back a letter after the palette had been dismissed")
		}
	}
}

// And so does an ordinary keystroke FINISHING — the release of a text key that
// is not the one armed.
//
// This is what bounds the count. Every capture has the commit arriving before
// the key that chose it comes up, so a release reaching here first means no
// commit is coming: the character was somebody typing, and the letter before it
// is theirs to keep.
func TestAnOrdinaryKeystrokeFinishingEndsTheTakeBack(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat") // withheld
	b.handleKey("o:Release")
	b.handleKey("e") // counted, in case a palette is still open
	b.handleKey("e:Release")
	b.handleKey("Text:ö") // a later composition, nothing to do with the hold

	for _, ev := range drain(b) {
		if _, ok := ev.(core.TextEraseEvent); ok {
			t.Fatal("took back a letter the user had typed past and let go of")
		}
	}
}

// The armed key's own release is exempt, and a SECOND hold of it re-arms rather
// than being counted as somebody typing on.
func TestASecondHoldOfTheArmedKeyStillWithholds(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat") // armed
	b.handleKey("o:Release")
	drain(b)

	b.handleKey("o")        // held again
	b.handleKey("o:Repeat") // must be withheld, or the palette is unreachable

	for _, ev := range drain(b) {
		if kp, ok := ev.(core.KeyPressEvent); ok && kp.Repeat {
			t.Fatal("the second hold repeated at once; its palette cannot be used")
		}
	}
}

// And so does a repeat that is let THROUGH. Past the wait a hold means what a
// hold usually means — type another one — so its letter is not a base character
// waiting to be replaced.
func TestRepeatsFlowingEndTheTakeBack(t *testing.T) {
	b := held(20 * time.Millisecond)

	b.handleKey("o")
	b.handleKey("o:Repeat") // withheld
	time.Sleep(30 * time.Millisecond)
	b.handleKey("o:Repeat") // flows
	b.handleKey("Text:ö")

	for _, ev := range drain(b) {
		if _, ok := ev.(core.TextEraseEvent); ok {
			t.Fatal("took a rune back out of a run the user deliberately repeated")
		}
	}
}

// A dead key composes with no hold behind it — Mega+i then "u" arrives as
// "CSI 117;1:2;251u" — so its completion is a commit and nothing else. It must
// reach the app whole, and take nothing back on its way.
func TestADeadKeyCompletionArrivesOnItsOwn(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("Text:û")

	got := drain(b)
	if len(got) != 1 {
		t.Fatalf("dispatched %d events, want the completed û: %+v", len(got), got)
	}
	if c, ok := got[0].(core.TextCommitEvent); !ok || c.Text != "û" {
		t.Errorf("dispatched %#v, want the û commit", got[0])
	}
}

// NAVIGATION never waits. An arrow repeats because somebody is navigating, and
// holding one is the ordinary way to do it: no palette opens over those, and
// delaying them would only make the host feel stuck.
func TestNavigationRepeatsAreNeverWithheld(t *testing.T) {
	b := held(time.Hour) // long enough that anything subject to it would be held

	for _, key := range []string{"Down:Repeat", "Up:Repeat", "PageDown:Repeat",
		"Left:Repeat", "F5:Repeat", "Return:Repeat", "^A:Repeat", "M-x:Repeat"} {
		b.handleKey(key)
	}

	if got := drain(b); len(got) != 8 {
		t.Errorf("dispatched %d of 8 navigation repeats; a held arrow must not "+
			"wait on a palette that never opens over it: %+v", len(got), got)
	}
}

// A fresh press ends the window: the next hold is a new question.
func TestAFreshPressEndsTheWait(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat") // withheld
	b.handleKey("x")        // somebody typed on
	got := drain(b)

	if len(got) != 2 {
		t.Fatalf("dispatched %d events, want the two presses: %+v", len(got), got)
	}
	if kp, ok := got[1].(core.KeyPressEvent); !ok || kp.Key != "x" {
		t.Errorf("second event %#v, want the x press", got[1])
	}
}

// Holding a DIFFERENT key restarts the wait rather than inheriting the old
// one's elapsed time.
func TestASecondHeldKeyGetsItsOwnWait(t *testing.T) {
	b := held(50 * time.Millisecond)

	b.handleKey("o")
	b.handleKey("o:Repeat") // withheld
	time.Sleep(60 * time.Millisecond)
	b.handleKey("o:Repeat") // past its wait, flows
	drain(b)

	b.handleKey("e")        // a different key, its own wait starts now
	b.handleKey("e:Repeat") // and is withheld by it

	got := drain(b)
	if len(got) != 1 {
		t.Fatalf("dispatched %d events, want only the e press: %+v", len(got), got)
	}
	if kp, ok := got[0].(core.KeyPressEvent); !ok || kp.Key != "e" || kp.Repeat {
		t.Errorf("dispatched %#v, want the plain e press", got[0])
	}
}

// A host with no such palette turns the wait off.
func TestANegativeWaitDisablesIt(t *testing.T) {
	b := held(-1)

	b.handleKey("o:Repeat")
	b.handleKey("o:Repeat")

	if got := drain(b); len(got) != 2 {
		t.Errorf("dispatched %d repeats with the wait off, want both: %+v", len(got), got)
	}
}

// Letting go ends the hold, so the NEXT hold of the same key asks the palette
// question again.
//
// The wait is measured from when the hold was first seen. Left standing across
// a release, a second hold of the same key found it already spent and repeated
// at once — the palette could be reached once per key per session, which is
// indistinguishable from it not working.
func TestReleasingEndsTheWaitForTheNextHold(t *testing.T) {
	b := held(50 * time.Millisecond)

	b.handleKey("o")
	b.handleKey("o:Repeat")
	time.Sleep(60 * time.Millisecond)
	b.handleKey("o:Repeat") // past the wait, flows
	b.handleKey("o:Release")

	// Second hold: its own wait, from now.
	b.handleKey("o")
	drain(b)

	b.handleKey("o:Repeat")
	if got := drain(b); len(got) != 0 {
		t.Errorf("the second hold repeated at once (%+v); its wait must start "+
			"over, or the palette is reachable only the first time", got)
	}
}

// Walking the palette does not end the take-back.
//
// Whether an arrow press even reaches us varies by input method: some want Tab
// before the arrows go to them, and a palette holding the keyboard swallows the
// press entirely. Treating one as "the user has moved on" made the same gesture
// work on the methods that swallow it and fail on the ones that do not.
func TestWalkingThePaletteDoesNotEndTheTakeBack(t *testing.T) {
	for _, key := range []string{"Left", "Right", "Up", "Down", "Tab", "Return",
		"PageUp", "PageDown", "Home", "End"} {
		b := held(time.Hour)
		b.handleKey("o")
		b.handleKey("o:Repeat") // armed
		b.handleKey(key)
		b.handleKey("Text:ö")

		found := false
		for _, ev := range drain(b) {
			if _, ok := ev.(core.TextEraseEvent); ok {
				found = true
			}
		}
		if !found {
			t.Errorf("%s ended the take-back; the letter the palette opened "+
				"over would be left in front of the chosen one", key)
		}
	}
}

// Escape is not one of them: it DISMISSES the palette, and the letter
// underneath is the user's to keep.
func TestEscapeStillEndsTheTakeBack(t *testing.T) {
	b := held(time.Hour)
	b.handleKey("o")
	b.handleKey("o:Repeat")
	b.handleKey("Escape")
	b.handleKey("Text:ö")

	for _, ev := range drain(b) {
		if _, ok := ev.(core.TextEraseEvent); ok {
			t.Fatal("took back a letter after the palette had been dismissed")
		}
	}
}

// A palette that commits as a PASTE takes the letter back too.
//
// Ghostty delivers a press-and-hold palette's result as bracketed paste when it
// is chosen by number or by click; the same gesture dismissed by TYPING arrives
// as a key event with associated text. Only the second route took the letter
// back, so choosing by number read "o4ö" and clicking read "oö".
func TestAPaletteThatCommitsAsAPasteTakesTheLetterBack(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat") // withheld — the palette opened
	b.handleKey("4")        // its selector, typed into the document
	b.deliverPaste("ö")

	got := drain(b)
	for i, ev := range got {
		if erase, ok := ev.(core.TextEraseEvent); ok {
			if erase.Count != 2 {
				t.Errorf("erased %d runes, want the letter and the selector",
					erase.Count)
			}
			if _, ok := got[i+1].(core.PasteEvent); !ok {
				t.Errorf("after the take-back came %#v, want the paste", got[i+1])
			}
			return
		}
	}
	t.Error("a paste committed with nothing taken back")
}

// An ordinary paste is not a palette. Nothing but a withheld repeat arms the
// take-back, so pasting into a document takes nothing out of it.
func TestAnOrdinaryPasteTakesNothingBack(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.deliverPaste("hello")

	for _, ev := range drain(b) {
		if _, ok := ev.(core.TextEraseEvent); ok {
			t.Fatal("an ordinary paste erased a character before itself")
		}
	}
}
