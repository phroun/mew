package main

// The demo exits when it has nothing left on the screen, and says so.
//
// This is the whole reported fault, end to end: exit the desktop, choose Yes, watch
// the windows go -- and the terminal running the demo sits there until somebody
// presses ctrl-C. Two things were wrong and both are here.
//
// **Nothing told the application.** `window_closed` was announced through the
// close-complete slot, which the window manager takes when it adopts the window, so
// the announcement was overwritten on every window the demo ever opened. See
// objects/window/0_window_protocol_closed_test.go for that half.
//
// **And nothing was watching.** An application with no windows is not told to go --
// it may perfectly well open another later -- so noticing is its own job.

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
)

// aDisplay is startService with the desktop handed back, because what this test does
// is quit that desktop and watch what the application makes of it.
func aDisplay(t *testing.T) (*trinkets.Desktop, string) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop := trinkets.NewDesktop()
	desktop.SetBackend(&nullBackend{})
	desktop.SetOnStartup(func() {
		if _, err := display.Serve(desktop, sock); err != nil {
			t.Errorf("serve: %v", err)
			desktop.Quit()
		}
	})
	exited := make(chan int, 1)
	go func() { exited <- desktop.Run() }()
	t.Cleanup(func() {
		desktop.Quit()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Error("the desktop did not exit")
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := (&net.Dialer{}).Dial("unix", sock); err == nil {
			return desktop, sock
		}
		if time.Now().After(deadline) {
			t.Fatal("display service did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// theDemo is the primary application, connected and built, as the binary starts it.
func theDemo(t *testing.T, sock string) *app {
	t.Helper()
	a, err := newPrimary(sock)
	if err != nil {
		t.Fatalf("starting the demo: %v", err)
	}
	t.Cleanup(func() { a.conn.Close() })
	return a
}

// ended reports what the app's wait said, or fails if it is still waiting.
func ended(t *testing.T, a *app) string {
	t.Helper()
	done := make(chan string, 1)
	go func() { done <- a.wait() }()
	select {
	case why := <-done:
		return why
	case <-time.After(5 * time.Second):
		t.Fatal("the demo is still waiting, and a terminal running it would sit there until somebody killed it")
		return ""
	}
}

// **The reported fault.** The desktop quits, which closes every window in it, and the
// demo notices and ends -- saying which window was the last one.
func TestTheDemoEndsWhenTheDesktopQuits(t *testing.T) {
	desktop, sock := aDisplay(t)
	a := theDemo(t, sock)

	// Everything the demo opens at startup is watched, or closing them all would
	// not add up to none left.
	if len(a.windowsOpen()) == 0 {
		t.Fatal("the demo is watching none of its windows, so it can never tell that they have gone")
	}
	for id, what := range a.windowsOpen() {
		if id == 0 {
			t.Errorf("%s is watched by id 0, which is nothing: a name the build never surfaced", what)
		}
	}

	desktop.Quit() // what Exit Desktop > Yes does once the sweep is agreed

	why := ended(t, a)
	if why == "" {
		t.Fatal("the demo ended without saying why, and a quiet exit cannot be told from a hang")
	}
	if why == "the display service went away" {
		t.Errorf("the demo only noticed because the socket went; it is meant to notice its windows going: %q", why)
	}
}

// A window closing while others remain is not the end of anything.
func TestTheDemoStaysWhileItStillHasAWindow(t *testing.T) {
	_, sock := aDisplay(t)
	a := theDemo(t, sock)

	// Both of the windows the demo opens at startup are watched, by name. A window
	// it opens and does not watch is one it can exit out from under: the others
	// close, the count reaches nought, and that one is still on the screen.
	open := a.windowsOpen()
	watching := map[string]uint64{}
	for id, what := range open {
		watching[what] = id
	}
	for _, want := range []string{"the main window", "the protocol window"} {
		if watching[want] == 0 {
			t.Fatalf("%s is not watched; the demo has %v", want, open)
		}
	}

	// Take the main window away. Everything else it opened is still there, so the
	// demo has not run out of anything.
	one := watching["the main window"]
	if err := a.conn.Object(one).Destroy(); err != nil {
		t.Fatalf("closing one window: %v", err)
	}

	// Long enough for the close and its announcement to have landed.
	deadline := time.Now().Add(2 * time.Second)
	for len(a.windowsOpen()) > 1 && !time.Now().After(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(a.windowsOpen()) == 0 {
		t.Fatal("closing one window left the demo thinking it had none")
	}
	select {
	case <-a.quit:
		t.Error("the demo ended with a window still open")
	default:
	}
}

// The message is the point of the exercise: from a terminal, a demo that exited
// cleanly and one that was killed look identical without it.
func TestTheDemoSaysWhyItEnded(t *testing.T) {
	_, sock := aDisplay(t)
	a := theDemo(t, sock)

	for id := range a.windowsOpen() {
		if err := a.conn.Object(id).Destroy(); err != nil {
			t.Fatalf("closing a window: %v", err)
		}
	}

	why := ended(t, a)
	if why == "" || why == "the display service went away" {
		t.Fatalf("the demo ended saying %q, want it to name the window it ran out of", why)
	}
}
