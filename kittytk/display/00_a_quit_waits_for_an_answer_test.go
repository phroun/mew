package display_test

// A quit that a deferred close cancelled.
//
// Quit works by closing every window, and any window saying no cancels it -- which
// is deliberate, and is how unsaved work stops a quit. But once an application can be
// CONSULTED about a close, a window that has not closed is either refusing or waiting
// for an answer, and those are opposites.
//
// Read as a refusal, the quit was abandoned; the answer arrived a moment later, every
// window closed, and the desktop was still running with nothing on it and nothing to
// say why. These are about that, over a real socket, because the pieces are in three
// packages and only the whole thing shows whether they meet.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/wire"
)

// quitting is a desktop with one careful application on it, whose window's close it
// is consulted about.
type quitting struct {
	t       *testing.T
	desktop *trinkets.Desktop
	conn    *client.Conn
	closing chan uint64
}

func carefulApp(t *testing.T) (*quitting, func()) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)

	conn, err := client.Dial(sock, "Careful App", nil)
	if err != nil {
		stop()
		t.Fatalf("dial: %v", err)
	}
	q := &quitting{t: t, desktop: desktop, conn: conn, closing: make(chan uint64, 8)}

	ui, err := conn.Build(`w=new window title="Papers" width=320 height=200`)
	if err != nil {
		conn.Close()
		stop()
		t.Fatalf("build: %v", err)
	}
	ui.Object("w").On("window_closing", func(ev *wire.Event) {
		id, _ := ev.Uint(wire.DecisionField)
		q.closing <- id
	})

	// Wait for the window to actually be on the desktop, or the quit below has
	// nothing to stop at and proves nothing.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		onUI(desktop, func() {
			for _, a := range desktop.Applications() {
				n += len(a.Windows())
			}
		})
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The connection goes FIRST, and this waits for the display to have finished
	// with it: a window whose application is still there asks it about closing, and
	// the desktop's own shutdown would sit waiting for an answer from a socket that
	// is on its way out.
	return q, func() {
		conn.Close()
		gone := time.Now().Add(5 * time.Second)
		for time.Now().Before(gone) {
			var empty bool
			if !q.running() {
				break // already quit; nothing left to drain it anyway
			}
			onUI(desktop, func() { empty = len(desktop.Applications()) == 0 })
			if empty {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		stop()
	}
}

func (q *quitting) asked() uint64 {
	q.t.Helper()
	select {
	case id := <-q.closing:
		return id
	case <-time.After(5 * time.Second):
		q.t.Fatal("the quit did not consult the application about closing its window")
		return 0
	}
}

func (q *quitting) waitFor(what string, cond func() bool) {
	q.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	q.t.Fatalf("timed out waiting for %s", what)
}

// running reads the desktop's own flag DIRECTLY and not through onUI.
//
// onUI posts to the desktop and waits, and a desktop that has quit is one whose run
// loop has stopped draining what is posted to it -- so asking it on its own thread
// whether it has stopped waits for ever. IsRunning is atomic and QuitRequested is a
// channel, and both are meant to be read from anywhere.
func (q *quitting) running() bool { return q.desktop.IsRunning() }

// **A quit resumes when the answer it was waiting for arrives.** This is the one
// that was broken: the window closed and the desktop stayed up.
func TestAQuitResumesWhenTheAnswerArrives(t *testing.T) {
	q, stop := carefulApp(t)
	defer stop()

	onUI(q.desktop, func() { q.desktop.Quit() })
	id := q.asked()
	if !q.running() {
		t.Fatal("the desktop quit while the application was still deciding")
	}

	if err := q.conn.Decide(id, true); err != nil {
		t.Fatalf("allowing the close: %v", err)
	}
	q.waitFor("the desktop to finish quitting", func() bool { return !q.running() })
}

// **And a refusal abandons it, for good.** Closing some unrelated window later must
// not quit the desktop out from under somebody who already said no.
func TestARefusedQuitStaysAbandoned(t *testing.T) {
	q, stop := carefulApp(t)
	defer stop()

	onUI(q.desktop, func() { q.desktop.Quit() })
	if err := q.conn.Decide(q.asked(), false); err != nil {
		t.Fatalf("denying the close: %v", err)
	}

	// Nothing to wait for -- a refusal changes nothing -- so this waits long enough
	// that a quit on its way would have landed.
	time.Sleep(300 * time.Millisecond)
	if !q.running() {
		t.Fatal("the application refused the close and the desktop quit anyway")
	}

	// **And it was asked ONCE.** A quit that went back round on hearing the refusal
	// would put the same question to the application that had just answered it, and
	// then again on that answer, for ever -- a loop, not a retry.
	select {
	case id := <-q.closing:
		t.Errorf("the application was asked again (decision %d) after refusing the close", id)
	default:
	}

	// **And an abandoned quit is not resurrected by the next close.** A second
	// window, closed through the same question-and-answer, tells the desktop a
	// deferred close resolved -- which is exactly the signal a waiting quit resumes
	// on, and this quit is not waiting any more.
	// A second top-level window needs the application to say it manages more than
	// one; the display refuses one otherwise.
	if _, err := q.conn.Exec("set app multiwindow"); err != nil {
		t.Fatalf("declaring multiwindow: %v", err)
	}
	ui, err := q.conn.Build(`p=new window title="Palette" width=200 height=120`)
	if err != nil {
		t.Fatalf("building a second window: %v", err)
	}
	ui.Object("p").On("window_closing", func(ev *wire.Event) {
		id, _ := ev.Uint(wire.DecisionField)
		q.closing <- id
	})

	// Pressed on the display, so it goes through the decision rather than through
	// `destroy`, which does not ask and so tells nobody anything.
	q.waitFor("the palette to reach the desktop", func() bool { return q.window("Palette") != nil })
	palette := q.window("Palette")
	onUI(q.desktop, func() { palette.Close() })
	if err := q.conn.Decide(q.asked(), true); err != nil {
		t.Fatalf("allowing the palette's close: %v", err)
	}
	q.waitFor("the palette to close", func() bool { return q.window("Palette") == nil })

	time.Sleep(200 * time.Millisecond)
	if !q.running() {
		t.Error("a later decided close resumed a quit that had been refused")
	}
	// **And the application was never asked about Papers a second time.** A quit
	// still remembered would have swept again on hearing the palette close, put the
	// same question to the application that already refused, and gone round on that
	// answer too.
	select {
	case id := <-q.closing:
		t.Errorf("the application was asked about the refused window again (decision %d)", id)
	default:
	}
}

// window finds one of the desktop's windows by title, or nil.
func (q *quitting) window(title string) *window.Window {
	q.t.Helper()
	var found *window.Window
	onUI(q.desktop, func() {
		for _, a := range q.desktop.Applications() {
			for _, w := range a.Windows() {
				if w.Title() == title {
					found = w
				}
			}
		}
	})
	return found
}
