package display_test

// The last thing a connection carries.
//
// A display that has stopped serves nobody, and its applications were left holding a
// socket to it. The shipped binaries got away with that because the process exited
// and the operating system closed the sockets for them; a host that embeds a display,
// or keeps running after stopping one, got nothing at all -- and an application
// waiting on a display that had gone waited for ever.
//
// So the display closes them. And says which kind of going it was first, because a
// closed socket cannot: a display that quit and a connection that broke are the same
// silence from the far end, and they call for opposite things.
//
// **What the application does about it is not the display's business.** It may have
// work of its own that outlives its display. Nothing here instructs it; the display
// says it is going, and goes.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/wire"
)

// gone waits for a connection to end and reports what the display said on the way, or
// fails if it is still open -- which is the fault this is all about.
func gone(t *testing.T, conn *client.Conn) (string, bool) {
	t.Helper()
	select {
	case <-conn.Closed():
	case <-time.After(5 * time.Second):
		t.Fatal("the display stopped and the connection is still open: an application " +
			"waiting on it would wait for ever")
	}
	return conn.Goodbye()
}

// The desktop quitting is the ordinary way a display goes, and every application
// connected to it is told so and hung up on.
func TestQuittingSaysGoodbyeToEveryApplication(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	var conns []*client.Conn
	for _, name := range []string{"First App", "Second App", "Third App"} {
		conn, err := client.Dial(sock, name, nil)
		if err != nil {
			t.Fatalf("dial %s: %v", name, err)
		}
		defer conn.Close()
		conns = append(conns, conn)
	}

	desktop.Quit()

	for i, conn := range conns {
		reason, said := gone(t, conn)
		if !said {
			t.Errorf("application %d was hung up on without a word", i)
			continue
		}
		if reason != wire.GoodbyeQuit {
			t.Errorf("application %d was told %q, want %q", i, reason, wire.GoodbyeQuit)
		}
	}
}

// A desktop that stopped because something was wrong says so, and that is the
// distinction worth having: it is the one an application can act on.
func TestADesktopThatStoppedBadlySaysSo(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "An App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	desktop.QuitWithCode(2)

	reason, said := gone(t, conn)
	if !said {
		t.Fatal("it was hung up on without a word")
	}
	if reason != wire.GoodbyeCrash {
		t.Errorf("a desktop that stopped with a code of 2 said %q, want %q",
			reason, wire.GoodbyeCrash)
	}
}

// Closing the server is the same thing without the desktop going: it serves nobody
// afterwards either.
func TestClosingTheServerHangsUp(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	_, srv, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "An App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := srv.Close(); err != nil {
		t.Fatalf("closing the server: %v", err)
	}
	if reason, said := gone(t, conn); !said || reason != wire.GoodbyeQuit {
		t.Errorf("a closed server said %q (said=%v), want %q", reason, said, wire.GoodbyeQuit)
	}
}

// **The windows go first, and the farewell is last.** An application watching its own
// windows has tidied up by the time it hears this; the farewell only says not to come
// back. The other order would tell it the display is gone and then tell it about
// windows closing on a display that is not there.
func TestTheWindowsCloseBeforeTheFarewell(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "An App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	ui, err := conn.Build(`w=new window title="Papers" width=320 height=200`)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	closed := make(chan struct{})
	ui.Window("w").OnClosed(func() { close(closed) })

	// Wait for the window to be on the desktop, or quitting has nothing to sweep.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		n := 0
		onUI(desktop, func() { n = len(desktop.WindowManager().Windows()) })
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	desktop.Quit()

	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the window closed and the application was never told, so the farewell " +
			"is all it got")
	}
	if _, said := gone(t, conn); !said {
		t.Error("the connection ended without a farewell")
	}
}

// A farewell is advisory, and a connection that ends without one is the ordinary
// case: the application hanging up on the display says nothing to itself.
func TestAnApplicationLeavingHearsNoFarewell(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	_, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "An App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()

	if reason, said := gone(t, conn); said {
		t.Errorf("an application that closed its own connection was told %q about it", reason)
	}
}

// And the application is left running. A farewell is not an instruction: whatever it
// was doing that does not need a display, it goes on doing.
func TestTheApplicationIsLeftToItsOwnBusiness(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "An App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Work of its own, which has nothing to do with any display.
	own := make(chan int, 1)
	go func() {
		<-conn.Closed()
		own <- 6 * 7
	}()

	desktop.Quit()
	if _, said := gone(t, conn); !said {
		t.Fatal("no farewell")
	}
	select {
	case got := <-own:
		if got != 42 {
			t.Errorf("its own work came out %d", got)
		}
	case <-time.After(5 * time.Second):
		t.Error("the application's own work did not carry on past the display going")
	}
	// Still an object, still answerable, still holding what it knew.
	if conn.AppID() == 0 {
		t.Error("the application lost its own identity when its display went")
	}
}
