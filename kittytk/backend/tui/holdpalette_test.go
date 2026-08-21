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

// keysIn lists the key names dispatched, in order, for a test that cares about
// what reached the application rather than about the composition around it.
func keysIn(got []core.Event) []string {
	var keys []string
	for _, ev := range got {
		if kp, ok := ev.(core.KeyPressEvent); ok {
			keys = append(keys, kp.Key)
		}
	}
	return keys
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

	if got := keysIn(drain(b)); len(got) != 1 || got[0] != "o" {
		t.Errorf("dispatched %v, want only the press", got)
	}
}

// Past the wait a hold means what a hold usually means, and the repeats flow.
func TestRepeatsFlowOnceTheWaitHasPassed(t *testing.T) {
	b := held(10 * time.Millisecond)

	b.handleKey("o") // the press starts the hold
	b.handleKey("o:Repeat")
	time.Sleep(25 * time.Millisecond)
	b.handleKey("o:Repeat")

	if got := keysIn(drain(b)); len(got) != 2 {
		t.Errorf("dispatched %v, want the press and the one past the wait", got)
	}
}

// The palette opens as a COMPOSITION standing on the letter the hold typed.
//
// It is the same shape the graphical host is handed for the same gesture, and
// what Covers exists for: macOS commits the held letter the moment the key goes
// down and only then opens over it, so the letter is in the document and has to
// be hidden while its alternatives are shown.
func TestTheHoldOpensACompositionOverItsLetter(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat") // withheld — the only sign a palette opened

	got := drain(b)
	if len(got) != 2 {
		t.Fatalf("dispatched %+v, want the press and the composition", got)
	}
	ed, ok := got[1].(core.TextEditingEvent)
	if !ok {
		t.Fatalf("second event %#v, want a composition", got[1])
	}
	if ed.Covers != 1 {
		t.Errorf("the composition covers %d runes, want the one the hold typed",
			ed.Covers)
	}
	// The letter itself, because a composition OPENS only with text of its own:
	// empty text with an extent is a composition ending and with none it is a
	// cancel, so one opened empty opened nothing and the commit behind it found
	// no region to replace. macOS marks the held character while offering
	// alternatives over it, which is exactly this.
	if ed.Text != "o" {
		t.Errorf("the composition holds %q, want the letter the palette marks",
			ed.Text)
	}
}

// Only ONE composition, however long the key is held.
func TestASecondWithheldRepeatDoesNotOpenASecondComposition(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat")
	b.handleKey("o:Repeat")
	b.handleKey("o:Repeat")

	compositions := 0
	for _, ev := range drain(b) {
		if _, ok := ev.(core.TextEditingEvent); ok {
			compositions++
		}
	}
	if compositions != 1 {
		t.Errorf("opened %d compositions, want one", compositions)
	}
}

// The chosen character arrives as the composition's COMMIT, which replaces what
// the composition covered — the letter the palette opened on.
func TestThePaletteResultCommitsTheComposition(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat")
	b.handleKey("Text:ö")

	got := drain(b)
	c, ok := got[len(got)-1].(core.TextCommitEvent)
	if !ok || c.Text != "ö" {
		t.Fatalf("last event %#v, want the ö commit", got[len(got)-1])
	}
	for _, ev := range got {
		if _, ok := ev.(core.TextEraseEvent); ok {
			t.Error("erased runes counted back from the caret; the composition " +
				"covers what the commit replaces, and a count does not survive " +
				"anything else reaching the document first")
		}
	}
}

// The palette's SELECTOR never reaches the document.
//
// Choosing by number is the palette being driven, not a "4" being typed: the
// commit that follows is the whole of what the gesture produced. Let through it
// stood between the letter and the accent — "o4ö" — and taking it back
// afterwards meant counting characters at the far end.
func TestTheSelectorTypedIntoThePaletteIsHeldBack(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat")
	b.handleKey("4")
	b.handleKey("Text:ö")

	if got := keysIn(drain(b)); len(got) != 1 || got[0] != "o" {
		t.Errorf("dispatched %v, want the held letter and nothing else", got)
	}
}

// But it is HELD, not dropped. A palette that was never there commits nothing,
// and everything struck in the meantime was ordinary typing.
func TestKeysHeldForAPaletteThatNeverCommitsAreLetThrough(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat") // a hold that opened no palette
	b.handleKey("e")
	b.handleKey("Escape") // and the user carries on

	got := keysIn(drain(b))
	if len(got) != 3 || got[0] != "o" || got[1] != "e" || got[2] != "Escape" {
		t.Errorf("dispatched %v, want [o e Escape] in the order they were struck", got)
	}
}

// Letting go is how the palette gets USED, so the release cannot end it. The
// terminal reports it before the choice is even made — in every capture of this
// gesture the held key comes up ahead of the arrows that walk the palette.
func TestTheCompositionOutlivesTheRelease(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat")
	b.handleKey("o:Release")
	b.handleKey("Text:ö")

	got := drain(b)
	if _, ok := got[len(got)-1].(core.TextCommitEvent); !ok {
		t.Errorf("last event %#v, want the commit; the composition must survive "+
			"the release or the letter is left in front of the chosen one",
			got[len(got)-1])
	}
}

// Walking the palette does not end it either. Whether an arrow press even
// reaches us varies by input method: some want Tab before the arrows go to
// them, and a palette holding the keyboard swallows the press entirely.
func TestWalkingThePaletteDoesNotEndTheComposition(t *testing.T) {
	for _, key := range []string{"Left", "Right", "Up", "Down", "Tab", "Return",
		"PageUp", "PageDown", "Home", "End"} {
		b := held(time.Hour)
		b.handleKey("o")
		b.handleKey("o:Repeat")
		b.handleKey(key)
		b.handleKey("Text:ö")

		got := drain(b)
		if _, ok := got[len(got)-1].(core.TextCommitEvent); !ok {
			t.Errorf("%s ended the composition; the letter it opened over would "+
				"be left in front of the chosen one", key)
		}
	}
}

// Escape is not one of them: it DISMISSES the palette, and the letter
// underneath is the user's to keep.
func TestEscapeEndsTheComposition(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat")
	b.handleKey("Escape")

	closed := false
	for _, ev := range drain(b) {
		if ed, ok := ev.(core.TextEditingEvent); ok && ed.Covers == 0 {
			closed = true
		}
	}
	if !closed {
		t.Error("Escape left the composition standing, so the letter it opened " +
			"over stays hidden for good")
	}
}

// And so does a repeat let THROUGH: past the wait a hold means ordinary
// repeating, and its letter is not a base character waiting to be replaced.
func TestRepeatsFlowingEndTheComposition(t *testing.T) {
	b := held(20 * time.Millisecond)

	b.handleKey("o")
	b.handleKey("o:Repeat")
	time.Sleep(30 * time.Millisecond)
	b.handleKey("o:Repeat") // flows

	closed := false
	for _, ev := range drain(b) {
		if ed, ok := ev.(core.TextEditingEvent); ok && ed.Covers == 0 {
			closed = true
		}
	}
	if !closed {
		t.Error("a hold that ran on into ordinary repeating left its composition open")
	}
}

// An ordinary keystroke FINISHING ends it too — the release of a text key that
// is not the one the composition belongs to.
//
// Every capture has the commit arriving before the key that chose it comes up,
// so a release reaching here first means no commit is coming.
func TestAnOrdinaryKeystrokeFinishingEndsTheComposition(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat")
	b.handleKey("o:Release")
	b.handleKey("e") // held, in case a palette is still open
	b.handleKey("e:Release")

	if got := keysIn(drain(b)); len(got) != 2 || got[1] != "e" {
		t.Errorf("dispatched %v, want the e let through once its release said "+
			"nobody was choosing with it", got)
	}
}

// The armed key's own release is exempt, and a SECOND hold of it re-opens
// rather than being held back as somebody typing on.
func TestASecondHoldOfTheSameKeyStillWithholds(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat")
	b.handleKey("o:Release")
	drain(b)

	b.handleKey("o")
	b.handleKey("o:Repeat")

	for _, ev := range drain(b) {
		if kp, ok := ev.(core.KeyPressEvent); ok && kp.Repeat {
			t.Fatal("the second hold repeated at once; its palette cannot be used")
		}
	}
}

// A composition commits nothing on its own account. A dead key composes with no
// hold behind it, so its completion is a commit and nothing else.
func TestADeadKeyCompletionArrivesOnItsOwn(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("Text:û")

	got := drain(b)
	if len(got) != 1 {
		t.Fatalf("dispatched %+v, want the completed û", got)
	}
	if c, ok := got[0].(core.TextCommitEvent); !ok || c.Text != "û" {
		t.Errorf("dispatched %#v, want the û commit", got[0])
	}
}

// A palette that commits as a PASTE is the commit it is. Ghostty delivers the
// result as bracketed paste when it is chosen by number or by click; the same
// gesture dismissed by TYPING arrives as a key event with associated text.
func TestAPaletteThatCommitsAsAPasteCommitsTheComposition(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat")
	b.handleKey("4")
	b.deliverPaste("ö")

	got := drain(b)
	if c, ok := got[len(got)-1].(core.TextCommitEvent); !ok || c.Text != "ö" {
		t.Fatalf("last event %#v, want the ö commit", got[len(got)-1])
	}
	if keys := keysIn(got); len(keys) != 1 || keys[0] != "o" {
		t.Errorf("dispatched %v, want the selector held back with the rest", keys)
	}
}

// An ordinary paste is not a palette. Nothing but a withheld repeat opens a
// composition, so pasting into a document is delivered as what it is.
func TestAnOrdinaryPasteIsStillAPaste(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.deliverPaste("hello")

	got := drain(b)
	if _, ok := got[len(got)-1].(core.PasteEvent); !ok {
		t.Errorf("last event %#v, want the paste delivered as a paste",
			got[len(got)-1])
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

	if got := keysIn(drain(b)); len(got) != 8 {
		t.Errorf("dispatched %d of 8 navigation repeats; a held arrow must not "+
			"wait on a palette that never opens over it: %v", len(got), got)
	}
}

// A fresh press ends the window: the next hold is a new question.
func TestAFreshPressEndsTheWait(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("o")
	b.handleKey("o:Repeat") // withheld
	b.handleKey("x")        // somebody typed on
	b.handleKey("x:Release")

	if got := keysIn(drain(b)); len(got) != 2 || got[1] != "x" {
		t.Errorf("dispatched %v, want the two presses", got)
	}
}

// A host with no such palette turns the wait off, and nothing is held back.
func TestANegativeWaitDisablesIt(t *testing.T) {
	b := held(-1)

	b.handleKey("o")
	b.handleKey("o:Repeat")
	b.handleKey("o:Repeat")

	if got := keysIn(drain(b)); len(got) != 3 {
		t.Errorf("dispatched %v with the wait off, want all three", got)
	}
}
