package client

// A reply nobody asked for is dropped, not waited on.
//
// A reply answers a batch, so one arriving with no batch outstanding is the
// other end saying something it should not have. The channel holding the reply
// in flight has room for one; handing it a second would block the reader --
// and a reader that has stopped is a connection that has gone deaf without
// saying so, which is a far worse failure than an odd statement being binned.
//
// This is not hypothetical. It took two stray replies to wedge an application
// that was otherwise serving correctly, and from the outside it looked like
// the display had stopped asking.

import (
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/phroun/kittytk/wire"
)

// standIn accepts one connection, does the handshake, and then says whatever
// it is told to say.
func standIn(t *testing.T, say ...string) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "display.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		defer nc.Close()
		scanner := wire.NewScanner(nc)
		scanner.Next() // hello
		scanner.Next() // the end that closes it
		fmt.Fprint(nc, "welcome version=1 session=1\n")
		fmt.Fprint(nc, "init app=1 store=2 host=3\n")
		for _, line := range say {
			fmt.Fprint(nc, line+"\n")
		}
		// Hold the connection open so the client keeps reading.
		select {}
	}()
	return sock
}

func TestAnUnexpectedReplyDoesNotDeafenTheConnection(t *testing.T) {
	// Two replies nobody asked for, and then an event. The event is the point:
	// if the reader has stopped on the second reply it never arrives.
	sock := standIn(t,
		"reply",
		"reply",
		"event click trinket=3",
	)
	conn, err := Dial(sock, "listening app", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	heard := make(chan struct{})
	conn.OnType("click", func(*wire.Event) { close(heard) })

	select {
	case <-heard:
	case <-time.After(3 * time.Second):
		t.Fatal("the connection went deaf after a reply nobody asked for")
	}
}

// And a reply that WAS asked for still reaches the batch waiting on it.
func TestTheReplyToABatchStillArrives(t *testing.T) {
	sock := standIn(t, "reply k=17")
	conn, err := Dial(sock, "asking app", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	done := make(chan *wire.Reply, 1)
	go func() {
		r, err := conn.Exec("k=new button")
		if err != nil {
			t.Error(err)
		}
		done <- r
	}()
	select {
	case r := <-done:
		if r == nil || r.IDs["k"] != 17 {
			t.Errorf("the reply came back as %#v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the reply never reached the batch that asked for it")
	}
}
