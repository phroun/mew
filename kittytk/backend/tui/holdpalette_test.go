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

	b.handleKey("o:Repeat") // opens the window
	time.Sleep(25 * time.Millisecond)
	b.handleKey("o:Repeat")

	got := drain(b)
	if len(got) != 1 {
		t.Fatalf("dispatched %d events, want the one past the wait: %+v", len(got), got)
	}
	if kp, ok := got[0].(core.KeyPressEvent); !ok || !kp.Repeat {
		t.Errorf("dispatched %#v, want a repeat", got[0])
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

	b.handleKey("o:Repeat")
	time.Sleep(60 * time.Millisecond)
	b.handleKey("o:Repeat") // past its wait, flows
	b.handleKey("e:Repeat") // a different key, its own wait starts now

	got := drain(b)
	if len(got) != 1 {
		t.Fatalf("dispatched %d events, want only the o repeat: %+v", len(got), got)
	}
	if kp, ok := got[0].(core.KeyPressEvent); !ok || kp.Key != "o" {
		t.Errorf("dispatched %#v, want the o repeat", got[0])
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
