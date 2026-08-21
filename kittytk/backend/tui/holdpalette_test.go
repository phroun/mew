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

// The palette's RESULT wears the repeat marker too, and must not be mistaken
// for the hold it came out of.
//
// The terminal reports the chosen character on the key that confirmed it, as an
// event type 2: choosing ö arrives as "CSI 13;1:2;246u", which the key handler
// names "ö:Repeat" because what a key TYPED outranks what it is called. It is a
// repeat by the marker and a commit by every other measure, and withholding it
// is indistinguishable from the palette doing nothing — the original letter is
// all that is left behind.
//
// The held key's name is the thing that separates them: no key named ö is down.
func TestThePaletteResultIsNotTheHeldKeyRepeating(t *testing.T) {
	b := held(time.Hour) // long enough that anything subject to it stays put

	b.handleKey("o")        // the hold that opens the palette
	b.handleKey("o:Repeat") // withheld, correctly
	b.handleKey("ö:Repeat") // and this is what was chosen out of it

	got := drain(b)
	if len(got) != 2 {
		t.Fatalf("dispatched %d events, want the press and the commit: %+v",
			len(got), got)
	}
	if kp, ok := got[1].(core.KeyPressEvent); !ok || kp.Key != "ö" {
		t.Errorf("dispatched %#v, want the chosen ö", got[1])
	}
}

// The same rule carries the dead key, which the terminal also marks as a
// repeat: Mega+i then "u" arrives as "CSI 117;1:2;251u", named "û:Repeat".
func TestADeadKeyCompletionIsNotAHeldKeyRepeating(t *testing.T) {
	b := held(time.Hour)

	b.handleKey("û:Repeat")

	if got := drain(b); len(got) != 1 {
		t.Errorf("dispatched %d events, want the completed û: %+v", len(got), got)
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
