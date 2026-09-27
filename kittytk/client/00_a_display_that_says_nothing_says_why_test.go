package client

// A display that accepts the connection and then closes it without a word.
//
// There is one way for that to happen: a tls:// display answers inside TLS, so
// a plaintext client's hello is not the handshake it expected, and its refusal
// cannot be written at all -- the socket simply ends. Nothing on the wire can
// say so, because nothing on the wire got through, so the client says it.
//
// The other half of the same problem is a display that DOES speak and refuses:
// what it said is worth more than the line it arrived on.

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phroun/kittytk/wire"
)

// silentListener accepts one connection and closes it without writing, which
// is what a tls:// listener does to a plaintext client.
func silentListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			nc.Close()
		}
	}()
	return "tcp://" + ln.Addr().String()
}

func TestADisplayThatSaysNothingSaysWhy(t *testing.T) {
	_, err := Dial(silentListener(t), "hopeful app", nil)
	if err == nil {
		t.Fatal("dialling a display that said nothing succeeded")
	}
	// What went wrong is still in there, because that is what happened -- an
	// end-of-file or a reset, depending on the machine. What is added is the
	// one thing that causes either.
	for _, want := range []string{"said nothing", "tls://"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error reads %q, and does not mention %q", err, want)
		}
	}
}

// Over a unix socket the same silence means something else entirely, so it is
// not explained away with a guess about schemes.
func TestSilenceOnASocketIsNotBlamedOnTLS(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		if nc, err := ln.Accept(); err == nil {
			nc.Close()
		}
	}()

	_, err = Dial(sock, "hopeful app", nil)
	if err == nil {
		t.Fatal("dialling a socket that said nothing succeeded")
	}
	if strings.Contains(err.Error(), "tls://") {
		t.Errorf("a unix socket was blamed on TLS: %v", err)
	}
}

// A display that refuses says why, and that is what the caller is told.
func TestARefusedHandshakeSaysWhatTheDisplaySaid(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		defer nc.Close()
		// Hear the hello out before refusing it: a display that hangs up
		// mid-sentence is the other test.
		scanner := wire.NewScanner(nc)
		scanner.Next()
		scanner.Next()
		fmt.Fprint(nc, "error text=\"connection refused\"\n")
		select {}
	}()

	_, err = Dial(sock, "unwelcome app", nil)
	if err == nil {
		t.Fatal("a refused handshake succeeded")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("the refusal reads %q", err)
	}
	// And not as the raw line it came on.
	if strings.Contains(err.Error(), "error text=") {
		t.Errorf("the refusal was quoted rather than read: %q", err)
	}
}
