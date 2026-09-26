package client

// Saying the answer is a person's, for as long as it takes them.
//
// A decision carries a deadline because the display cannot tell an application
// holding a question in front of somebody from one that has stopped. Reading a
// dialog takes longer than that deadline, so an application that puts one up has to
// say it is doing so -- repeatedly, because a keep-alive that never stops coming
// would excuse a hang.
//
// Conn.Asking is that, so an application author writes one line instead of a ticker.
// What is checked here is the pacing (twice per wait, off the event, not a number
// this file and the display separately believe), that stopping stops it, and that it
// gives up when the display stops recognising the question.

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phroun/kittytk/wire"
)

// listener is a stand-in display that answers every batch and remembers what it was
// told. `refuse` makes it turn each batch down, which is what a question the display
// has given up on looks like from here.
type listener struct {
	mu   sync.Mutex
	said []string
}

func (l *listener) heard() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.said...)
}

// count is how many of what it heard begin with the given text.
func (l *listener) count(prefix string) int {
	n := 0
	for _, s := range l.heard() {
		if strings.HasPrefix(s, prefix) {
			n++
		}
	}
	return n
}

func listening(t *testing.T, refuse bool) (*listener, string) {
	t.Helper()
	l := &listener{}
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
		for {
			line, err := scanner.Next()
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "end" {
				if refuse {
					fmt.Fprint(nc, "error text=\"no object with id 94\"\n")
				} else {
					fmt.Fprint(nc, "reply\n")
				}
				continue
			}
			if line == "" {
				continue
			}
			l.mu.Lock()
			l.said = append(l.said, line)
			l.mu.Unlock()
		}
	}()
	return l, sock
}

// aboutClosing is the event a display sends about a window it is about to close: the
// decision to answer, and how long it will wait for the answer.
func aboutClosing(within time.Duration) *wire.Event {
	return wire.NewEvent("window_closing").
		WithUint("window", 17).
		WithUint(wire.DecisionField, 94).
		WithInt(wire.DecisionWithinField, int(within.Milliseconds()))
}

// Twice per wait, so one ping going missing costs nothing -- and it keeps coming for
// as long as the person takes.
func TestAskingKeepsSayingSo(t *testing.T) {
	l, sock := listening(t, false)
	conn, err := Dial(sock, "an app with a question of its own", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	const within = 200 * time.Millisecond
	stop := conn.Asking(aboutClosing(within))
	defer stop()

	// Three waits' worth of somebody reading. Without the keep-alive the display
	// would have given up after the first.
	time.Sleep(3 * within)
	said := l.count("do 94 " + wire.DecisionWaiting)
	if said < 3 {
		t.Errorf("it said it was still asking %d times in %v of a person reading, want at least 3: %v",
			said, 3*within, l.heard())
	}

	// And it says nothing else. A keep-alive that answered would close a window
	// nobody agreed to close.
	for _, word := range []string{wire.DecisionAllow, wire.DecisionDeny} {
		if n := l.count("do 94 " + word); n != 0 {
			t.Errorf("the keep-alive said %q %d times, which is an answer nobody gave", word, n)
		}
	}
}

// Stopping stops it. A keep-alive outliving the question it is about is a lie about
// a person who has gone back to work -- and it would hold a window open for ever.
func TestStoppingStopsSayingIt(t *testing.T) {
	l, sock := listening(t, false)
	conn, err := Dial(sock, "an app that got its answer", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	const within = 100 * time.Millisecond
	stop := conn.Asking(aboutClosing(within))
	time.Sleep(3 * within)
	if l.count("do 94 "+wire.DecisionWaiting) == 0 {
		t.Fatal("it never said it was asking, so this proves nothing about stopping")
	}
	stop()
	stop() // twice is safe: the same answer can arrive down two paths

	settled := l.count("do 94 " + wire.DecisionWaiting)
	time.Sleep(4 * within)
	// One may still land: a ping already on the wire when stop was called cannot be
	// recalled, and it costs nothing -- it buys one more wait, and the answer that
	// follows stop settles the question inside it. What must not happen is the
	// pinging carrying on, which four more waits would show several times over.
	if now := l.count("do 94 " + wire.DecisionWaiting); now > settled+1 {
		t.Errorf("it went on saying it was asking after it stopped (%d more over %v)",
			now-settled, 4*within)
	}
}

// An event carrying no decision, or a question nobody is timing, has nothing to keep
// alive -- so nothing is sent, and the func handed back is safe to call anyway.
func TestThereIsNothingToKeepAliveWithoutADeadline(t *testing.T) {
	for _, tc := range []struct {
		name string
		ev   *wire.Event
	}{
		{"no decision at all", wire.NewEvent("click").WithUint("trinket", 3)},
		{"a question nobody is timing", wire.NewEvent("window_closing").
			WithUint(wire.DecisionField, 94).WithInt(wire.DecisionWithinField, 0)},
		{"no event", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, sock := listening(t, false)
			conn, err := Dial(sock, "an app", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()

			stop := conn.Asking(tc.ev)
			time.Sleep(150 * time.Millisecond)
			stop()
			if n := l.count("do 94 " + wire.DecisionWaiting); n != 0 {
				t.Errorf("it kept alive a question with no deadline (%d times)", n)
			}
		})
	}
}

// **Refused means the display is not holding this question any more**: it gave up,
// or somebody else answered. There is nothing left to keep alive, so it stops rather
// than shouting at a decision that is gone for as long as the application lives.
func TestAskingGivesUpWhenTheQuestionIsGone(t *testing.T) {
	l, sock := listening(t, true)
	conn, err := Dial(sock, "an app that was overtaken", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	const within = 100 * time.Millisecond
	stop := conn.Asking(aboutClosing(within))
	defer stop()

	// One refusal is enough to end it, so whatever it said by then is all it will
	// ever say.
	time.Sleep(2 * within)
	said := l.count("do 94 " + wire.DecisionWaiting)
	if said == 0 {
		t.Fatal("it never tried, so this proves nothing about giving up")
	}
	if said > 2 {
		t.Errorf("it was refused and tried %d times anyway", said)
	}
	time.Sleep(4 * within)
	if now := l.count("do 94 " + wire.DecisionWaiting); now != said {
		t.Errorf("it went on pinging a question the display refused (%d, then %d)", said, now)
	}
}
