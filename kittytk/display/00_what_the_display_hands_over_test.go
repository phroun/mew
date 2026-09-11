package display_test

// A connection is handed objects it did not build. The welcome used to carry
// their ids beside its own integers, so a client could not tell an id from a
// version by shape and had to be taught each name: adding a fourth object meant
// editing three clients before any of them could reach it.
//
// They arrive in a statement of their own now. Every field of it is an object,
// so there is nothing to mark and nothing to teach -- and because it is an
// ordinary statement rather than a handshake step, the display can say it again
// whenever it has something new to give.

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/wire"
)

func TestTheDisplaySaysWhatItHandedOver(t *testing.T) {
	conn := dialDesktop(t, "Handed App")

	names := conn.InitNames()
	if strings.Join(names, ",") != "app,host,store" {
		t.Errorf("the display handed over %v", names)
	}
	for _, name := range names {
		if conn.Init(name) == 0 {
			t.Errorf("%q was named with no id", name)
		}
	}
	// No two of them are the same object.
	seen := map[uint64]string{}
	for _, name := range names {
		id := conn.Init(name)
		if other, ok := seen[id]; ok {
			t.Errorf("%q and %q are both %d", name, other, id)
		}
		seen[id] = name
	}

	// The names this package wraps are lookups into the same record, so they
	// cannot drift from it.
	for _, c := range []struct {
		name string
		got  uint64
	}{
		{wire.AppName, conn.AppID()},
		{wire.StoreName, conn.StoreID()},
		{wire.HostName, conn.HostID()},
	} {
		if conn.Init(c.name) != c.got {
			t.Errorf("%s is %d by name and %d by its own accessor",
				c.name, conn.Init(c.name), c.got)
		}
	}

	// A name the display did not hand over is 0 rather than a guess.
	if id := conn.Init("clipboard"); id != 0 {
		t.Errorf("a name nothing handed over answered %d", id)
	}
}

// Generic by name: a client reaches one of them without this package having a
// wrapper for it.
func TestOneOfThemIsReachedByNameAlone(t *testing.T) {
	conn := dialDesktop(t, "Generic App")

	if err := conn.Given(wire.HostName).Set("dark"); err != nil {
		t.Errorf("set through the named handle: %v", err)
	}
	heard := make(chan struct{}, 2)
	conn.Given(wire.StoreName).On(client.StoreDone, func(*wire.Event) { heard <- struct{}{} })
	if err := conn.Given(wire.StoreName).Ask("inventory"); err != nil {
		t.Fatalf("ask through the named handle: %v", err)
	}
	select {
	case <-heard:
	case <-time.After(5 * time.Second):
		t.Error("the named handle heard nothing back")
	}
}

// And it is not only a handshake step: a client takes one that arrives later,
// under a name it was never taught. The display has no reason to send a second
// one yet, so the test is the one speaking here -- what is under test is the
// client's side of it.
func TestAnInitThatArrivesLaterIsTaken(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "fake.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	later := make(chan net.Conn, 1)
	go func() {
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		// Read the hello batch up to its terminator, then answer as a display.
		sc := wire.NewScanner(nc)
		for {
			text, err := sc.Next()
			if err != nil {
				return
			}
			if strings.HasPrefix(strings.TrimSpace(text), "end") {
				break
			}
		}
		fmt.Fprint(nc, "welcome version=1 session=1\n")
		fmt.Fprint(nc, "init app=6 store=7 host=8\n")
		later <- nc
	}()

	conn, err := client.Dial(sock, "Listening App", func(string) {})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if conn.AppID() != 6 || conn.StoreID() != 7 || conn.HostID() != 8 {
		t.Fatalf("the handshake left app=%d store=%d host=%d",
			conn.AppID(), conn.StoreID(), conn.HostID())
	}
	if id := conn.Init("clipboard"); id != 0 {
		t.Fatalf("a name nothing handed over answered %d", id)
	}

	// Something new, and something under a name already in hand.
	nc := <-later
	fmt.Fprint(nc, "init clipboard=99 store=77\n")

	deadline := time.Now().Add(5 * time.Second)
	for conn.Init("clipboard") == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the client never took it; it holds %v", conn.InitNames())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := conn.Init("clipboard"); got != 99 {
		t.Errorf("clipboard is %d, want 99", got)
	}
	// A name said again means what it means NOW, rather than leaving two
	// answers for one name.
	if got := conn.StoreID(); got != 77 {
		t.Errorf("the store is still %d after being handed over again", got)
	}
}
