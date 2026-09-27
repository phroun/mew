package display_test

// A close an application refuses, over a real socket.
//
// The pieces are all tested where they live: the decision object in `protocol`, the
// window's binding in `objects/window`, the view's line in `objects/trinkets`. What
// none of those can show is whether they are WIRED -- whether the connection hands
// a decision somewhere addressable, whether the id an event carried resolves in the
// session that batch runs against, and whether the answer arrives on the thread the
// window tree lives on.
//
// So this is the whole path in one test: the [x] pressed on the display, the
// question read off the wire by a client library nobody taught anything new, and
// the window still standing afterwards because the application said not yet.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/wire"
)

// asking builds one window over a socket and reports what the application is asked
// about closing it.
type asking struct {
	t       *testing.T
	desktop *trinkets.Desktop
	conn    *client.Conn
	win     *window.Window
	closing chan *wire.Event
}

func closableWindow(t *testing.T) (*asking, func()) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)

	conn, err := client.Dial(sock, "Careful App", nil)
	if err != nil {
		stop()
		t.Fatalf("dial: %v", err)
	}

	a := &asking{t: t, desktop: desktop, conn: conn, closing: make(chan *wire.Event, 4)}
	ui, err := conn.Build(`w=new window title="Papers" width=320 height=200`)
	if err != nil {
		conn.Close()
		stop()
		t.Fatalf("build: %v", err)
	}
	// Subscribing is what makes the close askable at all, and it is an ordinary
	// subscription on an ordinary event: nothing here knows about decisions.
	ui.Object("w").On("window_closing", func(ev *wire.Event) { a.closing <- ev })

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && a.win == nil {
		onUI(desktop, func() {
			for _, app := range desktop.Applications() {
				for _, w := range app.Windows() {
					a.win = w
				}
			}
		})
		if a.win == nil {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if a.win == nil {
		conn.Close()
		stop()
		t.Fatal("the window never reached the desktop")
	}
	// The connection goes FIRST, and this waits for the display to have finished
	// with it. Otherwise the desktop's own shutdown closes the window, which asks
	// an application that is on its way out -- a real question about quitting, and
	// not one these tests are about.
	return a, func() {
		conn.Close()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var empty bool
			onUI(desktop, func() { empty = len(desktop.Applications()) == 0 })
			if empty {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		stop()
	}
}

// close presses the [x], on the thread a close happens on, and reports what Close
// answered.
func (a *asking) close() bool {
	a.t.Helper()
	var ok bool
	onUI(a.desktop, func() { ok = a.win.Close() })
	return ok
}

func (a *asking) visible() bool {
	a.t.Helper()
	var v bool
	onUI(a.desktop, func() { v = a.win.IsVisible() })
	return v
}

// asked waits for the question and hands back the decision it carries.
func (a *asking) asked() uint64 {
	a.t.Helper()
	select {
	case ev := <-a.closing:
		id, ok := ev.Uint(wire.DecisionField)
		if !ok {
			a.t.Fatalf("the question carries no %s=: %s", wire.DecisionField, ev.Encode())
		}
		return id
	case <-time.After(5 * time.Second):
		a.t.Fatal("the application was never asked about the close")
		return 0
	}
}

// waitFor polls a condition on the UI thread, because the answer crosses a socket
// and then a thread, and neither is instant.
func (a *asking) waitFor(what string, cond func() bool) {
	a.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.t.Fatalf("timed out waiting for %s", what)
}

// **The whole path.** The window declines to close, the application is asked over
// the wire, it says the window may go, and the window goes -- which is the feature
// that did not exist, because a Go handler returning false reached nobody.
func TestAnApplicationAllowsACloseOverTheWire(t *testing.T) {
	a, stop := closableWindow(t)
	defer stop()

	if a.close() {
		t.Error("the close went through before the application had answered")
	}
	if !a.visible() {
		t.Fatal("the window closed while the application was deciding")
	}

	// One call, and underneath it the `do <id> allow` the client library could
	// already say: nothing in it had to be taught a thing.
	if err := a.conn.Decide(a.asked(), true); err != nil {
		t.Fatalf("allowing the close: %v", err)
	}
	a.waitFor("the window to close", func() bool { return !a.visible() })
}

// **And the refusal is the point of it.** An application with unsaved work says so,
// and the window is still on the screen for the person who has not saved it.
func TestAnApplicationRefusesACloseOverTheWire(t *testing.T) {
	a, stop := closableWindow(t)
	defer stop()

	if a.close() {
		t.Error("the close went through before the application had answered")
	}
	if err := a.conn.Decide(a.asked(), false); err != nil {
		t.Fatalf("denying the close: %v", err)
	}

	// Nothing to wait for -- a refusal changes nothing -- so this waits long enough
	// that a close on its way would have landed.
	time.Sleep(200 * time.Millisecond)
	if !a.visible() {
		t.Error("the application said not yet and the window closed anyway")
	}

	// Pressing it again asks again: the answer was about that close, not about the
	// window.
	if a.close() {
		t.Error("the second close went through on the first refusal's answer")
	}
	if second := a.asked(); second == 0 {
		t.Error("the second close did not ask")
	}
}

// **An application that goes while deciding leaves the window to the display**,
// which closes it as it tears the connection down. Otherwise a crashed application
// would leave its windows on the screen for ever, waiting for an answer from a
// socket that is shut.
func TestAWindowOutlivingItsApplicationIsClosed(t *testing.T) {
	a, stop := closableWindow(t)
	defer stop()

	if a.close() {
		t.Error("the close went through before the application had answered")
	}
	a.asked() // it was asked, and is about to stop being able to answer

	if err := a.conn.Close(); err != nil {
		t.Fatalf("closing the connection: %v", err)
	}
	a.waitFor("the abandoned window to be closed", func() bool {
		var gone bool
		onUI(a.desktop, func() { gone = len(a.desktop.Applications()) == 0 })
		return gone
	})
}
