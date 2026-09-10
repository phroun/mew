package display_test

// A statement the language cannot read is the client saying something wrong,
// not something unreadable. It used to end the connection: the reader treated
// every failure as the stream having ended, so a client that mistyped one
// statement found the socket gone with nothing said about why -- while a client
// that named a property no object has got was told so and carried on.
//
// The two failures are different in kind. Framing is the transport's: the
// scanner finds statement boundaries by the language's own brace and string
// awareness, and if that fails the bytes are no longer trustworthy. Parsing is
// the language's, and it answers the way every other refusal does.

import (
	"strings"
	"testing"
	"time"
)

// alive reports whether the connection is still up.
func alive(c interface{ Closed() <-chan struct{} }) bool {
	select {
	case <-c.Closed():
		return false
	default:
		return true
	}
}

func TestAStatementThatWillNotParseIsRefusedNotHungUpOn(t *testing.T) {
	conn := dialDesktop(t, "Mistyping App")

	// Refusals the session already gave, for comparison: the connection lives.
	if _, err := conn.Exec("set host nonsense=1"); err == nil {
		t.Error("an unknown property was accepted")
	}
	if !alive(conn) {
		t.Fatal("an unknown property ended the connection")
	}

	for _, src := range []string{
		"%%%",
		"set host dark=",
		"= = =",
		"w=new window title=\"ok\" width=@",
	} {
		_, err := conn.Exec(src)
		if err == nil {
			t.Errorf("%q was accepted", src)
			continue
		}
		if !strings.Contains(err.Error(), "parse") {
			t.Errorf("%q reads %q, which does not say it could not be read", src, err)
		}
		if !alive(conn) {
			t.Fatalf("%q ended the connection", src)
		}
	}

	// And the connection is still in step afterwards: the next batch runs.
	if err := conn.Host().Set("dark"); err != nil {
		t.Errorf("after all that: %v", err)
	}
	reply, err := conn.Exec(`w=new window title="Still here" width=120 height=80`)
	if err != nil {
		t.Fatalf("building afterwards: %v", err)
	}
	if reply.IDs["w"] == 0 {
		t.Error("the batch after the bad ones surfaced nothing")
	}
}

// A batch is poisoned by one bad statement: it does not half-run. The good
// statements beside it are read -- so the stream reaches the terminator and
// stays in step -- but nothing in it is executed.
func TestOneBadStatementPoisonsItsWholeBatch(t *testing.T) {
	conn := dialDesktop(t, "Poisoning App")

	_, err := conn.Exec(`good=new window title="Good" width=120 height=80
%%%
alsogood=new window title="Also" width=120 height=80`)
	if err == nil {
		t.Fatal("the batch was accepted")
	}
	if !alive(conn) {
		t.Fatal("the batch ended the connection")
	}
	// Neither name was registered, so neither window was built.
	for _, key := range []string{"good", "alsogood"} {
		if _, err := conn.Exec("set " + key + " title=\"x\""); err == nil {
			t.Errorf("%q was built although its batch was poisoned", key)
		}
	}
}

// The other kind of failure still ends it: a byte stream the scanner cannot
// frame is not a statement that can be refused. A quote with no closing quote
// swallows every statement after it, the terminator included, so there is
// nothing left to resynchronise on.
func TestAStreamThatCannotBeFramedStillEndsIt(t *testing.T) {
	for _, src := range []string{
		"set host status=\"a\nb\"",       // a newline inside a string
		`new window title="unterminated`, // and a string that never ends
	} {
		conn := dialDesktop(t, "Unframed App")
		_, _ = conn.Exec(src)

		deadline := time.Now().Add(5 * time.Second)
		for alive(conn) {
			if time.Now().After(deadline) {
				t.Errorf("%q left the connection up", src)
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
