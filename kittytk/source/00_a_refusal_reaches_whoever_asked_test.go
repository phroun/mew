package source

// A refusal reaching whoever asked for the records.
//
// **A batch is answered by exactly one line**: a `reply` naming what it made, or an
// `error` refusing it. The reply was listened for and the refusal was not, so a
// display asking an application for a source it does not serve -- a name misspelled
// in a bundle, which is the ordinary way it happens -- waited for records that were
// never coming, for as long as the window was open.
//
// The refusal travels as a VALUE on the answer's own ending: `Complete.Error`, the
// same field a source that runs out of records mid-answer fills in. There is nothing
// thrown, nothing to unwind, and no path through the program that exists only when
// something has gone wrong.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/wire"
	"github.com/phroun/serval"
)

// refusing is an application that turns the batch down rather than answering it,
// which is what one does with a query for a source it does not serve.
func refusing(t *testing.T, why string) *ApplicationSource {
	t.Helper()
	l := &loop{}
	src := NewApplicationSource("files", func(string) error {
		l.src.Statements(wire.EncodeError(why) + "\nend\n")
		return nil
	})
	l.src = src
	return src
}

// **The reader is told, rather than waiting for records that are not coming.**
func TestARefusedQueryReachesTheReader(t *testing.T) {
	src := refusing(t, `source "ledgers": this application serves no such thing`)
	got, done := read(t, src, `source="files"`, `count=5`)

	if len(got.keys) != 0 {
		t.Errorf("a refused query answered %d records", len(got.keys))
	}
	if !got.ended {
		t.Fatal("the scope was never ended, which is the bug: the reader waits for ever")
	}
	if !strings.Contains(done.Error, "ledgers") {
		t.Errorf("it ended with %q, want the reason the application gave", done.Error)
	}
}

// A refusal carrying no reason is still a refusal, and says so. An answer that
// reached the far end and was turned down tells a reader more than silence does,
// whatever else it manages to say.
func TestARefusalWithNoReasonIsStillARefusal(t *testing.T) {
	l := &loop{}
	src := NewApplicationSource("files", func(string) error {
		l.src.Statements("error\nend\n")
		return nil
	})
	l.src = src

	_, done := read(t, src, `source="files"`, `count=5`)
	if done.Error == "" {
		t.Error("a refusal with no reason on it reached the reader as an answer")
	}
}

// An `error` refusing something this source never asked for is left alone, the same
// way a `reply` naming a query that is not ours is. A connection carries more than
// one part of a display.
func TestARefusalForSomethingElseIsLeftAlone(t *testing.T) {
	src := NewApplicationSource("files", func(string) error { return nil })
	stmt, err := wire.Parse(wire.EncodeError("something else went wrong"))
	if err != nil {
		t.Fatal(err)
	}
	if src.Inbound(stmt.Statements[0]) {
		t.Error("it took a refusal for a question nobody here had asked")
	}
}

// And a refusal ends the scope it ANSWERS, which is the FIRST one still waiting to be
// named -- because a batch carries one query and batches are answered in order.
//
// Two outstanding and the wrong one ended is the quiet version of this bug: one reader
// is told a refusal that was not its own, and the one that was refused goes on
// waiting.
func TestARefusalEndsTheFirstScopeStillWaiting(t *testing.T) {
	// An application that answers nothing until it is told to, so that two questions
	// can be outstanding at once.
	held := []string{}
	l := &loop{}
	src := NewApplicationSource("files", func(src string) error {
		held = append(held, src)
		return nil
	})
	l.src = src

	set, err := src.Open(&serval.DataSetDescriptor{Source: "files"})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	first, second := &collector{}, &collector{}
	if err := set.Read(&serval.Scope{Count: 5}, first); err != nil {
		t.Fatal(err)
	}
	if err := set.Read(&serval.Scope{Count: 7}, second); err != nil {
		t.Fatal(err)
	}
	if len(held) != 2 {
		t.Fatalf("the application was asked %d times, want two", len(held))
	}
	if first.ended || second.ended {
		t.Fatal("a scope ended before anything answered it")
	}

	// One refusal, for the first batch.
	src.Statements(wire.EncodeError("the first one, no") + "\nend\n")

	if !first.ended {
		t.Error("the refusal did not end the question it answers")
	}
	if !strings.Contains(first.done.Error, "the first one") {
		t.Errorf("the first scope ended with %q", first.done.Error)
	}
	if second.ended {
		t.Error("a refusal for the first question ended the second as well")
	}
}
