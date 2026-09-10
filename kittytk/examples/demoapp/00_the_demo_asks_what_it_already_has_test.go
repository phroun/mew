package main

// Subscribing to the store opens the flow of its answers; it does not bring
// one. The demo shows the material it found before it adds to it, so it has to
// ask -- and if it only subscribes and waits, the wait is all it gets, and the
// listing it puts in the status bar is whatever finally times out.

import (
	"testing"
	"time"
)

// The demo gives up waiting for its first listing after five seconds. Reaching
// the writing phase well inside that says an answer arrived, rather than the
// wait running out.
const askedAndAnswered = 3 * time.Second

func TestTheDemoAsksWhatItAlreadyHasBeforeItWrites(t *testing.T) {
	sock, stop := startService(t)
	defer stop()

	began := time.Now()
	a, err := newPrimary(sock)
	if err != nil {
		t.Fatalf("start the demo: %v", err)
	}
	defer a.conn.Close()

	phase := func() (string, int) {
		a.shelf.mu.Lock()
		defer a.shelf.mu.Unlock()
		return a.shelf.when, len(a.shelf.ids)
	}

	deadline := time.Now().Add(askedAndAnswered)
	for {
		when, known := phase()
		if when == "after" {
			// The listing it found is behind it; what it writes is its own.
			if known > 0 && time.Since(began) < askedAndAnswered {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("after %v the demo is still in phase %q with %d items known: "+
				"it subscribed and waited instead of asking",
				time.Since(began).Round(time.Millisecond), when, known)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// And then it writes everything it stocks.
	want := len(samples())
	for deadline := time.Now().Add(5 * time.Second); ; {
		_, known := phase()
		if known >= want {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d samples were acknowledged", known, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
