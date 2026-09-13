package display_test

// A connection arrives holding two objects it did not build: its application
// and its store. The handshake carries their ids, and the display also has them
// waiting under names, so an app says what it means without first writing the
// numbers down.
//
// They are names in the session's key table, not reserved words. A client that
// wants either for something of its own takes it, and what it displaced is
// still there under the id it was handed.

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/wire"
)

// servedDesktop runs a headless desktop with a display server on a socket, and
// hands back the desktop, the socket, and how to stop it.
func servedDesktop(t *testing.T) (*trinkets.Desktop, *display.Server, string, func()) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "display.sock")

	desktop := trinkets.NewDesktop()
	desktop.SetBackend(&nullBackend{})
	ready := make(chan *display.Server, 1)
	desktop.SetOnStartup(func() {
		srv, err := display.Serve(desktop, sock)
		if err != nil {
			t.Errorf("serve: %v", err)
			desktop.Quit()
			return
		}
		ready <- srv
	})
	exited := make(chan int, 1)
	go func() { exited <- desktop.Run() }()

	var srv *display.Server
	select {
	case srv = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("desktop did not start")
	}

	var once sync.Once
	return desktop, srv, sock, func() {
		once.Do(func() {
			srv.Close()
			desktop.Quit()
			select {
			case <-exited:
			case <-time.After(5 * time.Second):
				t.Error("desktop did not exit")
			}
		})
	}
}

// dialSocket connects an app to a desktop already running.
func dialSocket(t *testing.T, sock, appName string) *client.Conn {
	t.Helper()
	conn, err := client.Dial(sock, appName, func(string) {})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// dialDesktop runs a headless desktop and returns a connection to it.
func dialDesktop(t *testing.T, appName string) *client.Conn {
	t.Helper()
	_, _, sock, done := servedDesktop(t)
	t.Cleanup(done)
	return dialSocket(t, sock, appName)
}

func TestTheNamesAreThereBeforeAnythingIsSaid(t *testing.T) {
	conn := dialDesktop(t, "Named App")

	// Not one naming statement first.
	for _, src := range []string{
		`set app multiwindow contextonly`,
		`sub store store_blob store_done`,
		`ask store inventory`,
	} {
		if _, err := conn.Exec(src); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}

	// And they are the objects they are supposed to be: the app takes what an
	// application takes, and refuses what it does not.
	_, err := conn.Exec(`set app title="not a window"`)
	if err == nil || !strings.Contains(err.Error(), "application has no property") {
		t.Errorf(`set app title= reads %v, want an application's refusal`, err)
	}
}

// The store answers to its name, so the answer comes back the way asking by id
// brought it.
func TestTheStoreAnswersToItsName(t *testing.T) {
	conn := dialDesktop(t, "Asking App")

	done := make(chan int, 2)
	conn.OnStore(client.StoreDone, func(ev *wire.Event) {
		n, _ := ev.Int("count")
		done <- n
	})
	if _, err := conn.Exec("ask store inventory"); err != nil {
		t.Fatalf("ask store inventory: %v", err)
	}
	select {
	case n := <-done:
		if n != 0 {
			t.Errorf("a store nothing has been written to holds %d items", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("asking the store by name was never answered")
	}
}

// The name is a session key, so re-keying it means what re-keying anything else
// means. The application is not lost with it: it still answers to its id.
func TestTakingTheNameLeavesTheObjectUnderItsID(t *testing.T) {
	conn := dialDesktop(t, "Shadowing App")

	reply, err := conn.Exec(`app=new window title="Mine" width=120 height=80`)
	if err != nil {
		t.Fatalf("taking the name: %v", err)
	}
	if reply.IDs["app"] == 0 || reply.IDs["app"] == conn.AppID() {
		t.Fatalf("the window came back as %d, the application is %d",
			reply.IDs["app"], conn.AppID())
	}

	// `app` now names the window, which is no application.
	if _, err := conn.Exec(`set app multiwindow`); err == nil {
		t.Error("a window took an application's property")
	}
	// The library said the name too, so it follows the name where it went:
	// an app that takes `app` for a window has said what it means by it, and
	// SetApp goes to the window and is refused there. AppID is what reaches
	// the application afterwards.
	if _, err := conn.SetApp("multiwindow"); err == nil {
		t.Error("SetApp addressed the application by id, past the name it was given")
	}
	// The application is where it always was.
	if _, err := conn.Exec(fmt.Sprintf("set %d multiwindow", conn.AppID())); err != nil {
		t.Errorf("the application no longer answers to its id: %v", err)
	}
}

// The client library says the names too, so what an app sends reads as what it
// meant rather than as a number.
func TestTheLibrarySaysTheNames(t *testing.T) {
	conn := dialDesktop(t, "Library App")

	if _, err := conn.SetApp("multiwindow"); err != nil {
		t.Errorf("SetApp: %v", err)
	}
	if got := conn.App().ID(); got != conn.AppID() {
		t.Errorf("the app handle carries id %d, want %d", got, conn.AppID())
	}
	if got := conn.Store().ID(); got != conn.StoreID() {
		t.Errorf("the store handle carries id %d, want %d", got, conn.StoreID())
	}
	// Events still route by id, so a handler registered through the named
	// handle hears what the store raises.
	heard := make(chan struct{}, 2)
	conn.Store().On(client.StoreDone, func(*wire.Event) { heard <- struct{}{} })
	if err := conn.Store().List(); err != nil {
		t.Fatalf("List: %v", err)
	}
	select {
	case <-heard:
	case <-time.After(5 * time.Second):
		t.Error("the named handle heard nothing back")
	}
}
