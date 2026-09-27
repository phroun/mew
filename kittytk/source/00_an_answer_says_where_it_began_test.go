package source

// Where an answer BEGAN, and why an application has to be able to say it.
//
// `Scope.From` is a position: a thumb dragged down a long sequence knows how far
// down it is and knows no identity there at all. A source honours it as well as it
// can and says where it actually began in `Complete.First`, which is the truth
// whatever `From` asked for -- and an application walking its own body may honour it
// not at all, there being no index into a sequence somebody else named.
//
// **So a naive application answers from the top and sends more records than were
// asked for, and that has to be slower rather than wrong.** It was wrong. Silence
// about `first` left the reader its own arithmetic, which for a scope that asked for
// a position means believing the position was honoured -- so the first record of a
// naive answer was placed at the nine hundredth row and nothing said so. And there
// was no way for an application to say otherwise even if it wanted to: the wire
// carries `first=` and the display parses it, and `client.Fill` could not produce
// one.

import (
	"testing"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/serval"
)

// **A naive answer to a position is placed where its records REALLY are.**
//
// The application here is the one the rest of this package's tests use: it honours
// the sort, the count and `after`, and ignores `from` -- which is what an
// application that has no index into the sequence does.
func TestANaiveAnswerToAPositionSaysItBeganAtTheBeginning(t *testing.T) {
	src := serving(t)
	got, done := read(t, src, `source="files"`, `from=2 count=2`)

	// It sent the first two records, because it reads `count` and not `from`.
	if want := []string{"0", "1"}; !sameKeys(got.keys, want) {
		t.Fatalf("the answer holds %v, want %v -- the naive application starts at"+
			" the top", got.keys, want)
	}
	// And this end says so, rather than letting them stand at the second row.
	if done.First != serval.Exactly(0) {
		t.Errorf("it says the answer began at %v, want exactly the beginning -- the"+
			" application said nothing, and nothing is what a source that ignored"+
			" the position does", done.First)
	}
}

// **An application that HONOURS a position says so, and is believed.** That is the
// one thing it has to say to be taken at its word, and it is what turns a jump into
// one round trip instead of a walk.
func TestAnApplicationThatHonoursAPositionSaysWhereItBegan(t *testing.T) {
	src := hosting(t, func(f *client.Fill) {
		at := f.From
		if at > len(appRecords) {
			at = len(appRecords)
		}
		f.First(at)
		f.Ordered()
		sent := 0
		for i := at; i < len(appRecords) && sent < f.Count; i++ {
			r := appRecords[i]
			_ = f.Record(r.key, serval.Named("key", r.key),
				serval.Named(".name", r.name), serval.Named(".size", r.size))
			sent++
		}
		_ = f.Exhausted()
	})

	got, done := read(t, src, `source="files"`, `from=2 count=2`)
	if want := []string{"2", "3"}; !sameKeys(got.keys, want) {
		t.Fatalf("the answer holds %v, want %v", got.keys, want)
	}
	if done.First != serval.Exactly(2) {
		t.Errorf("it says the answer began at %v, want exactly the second record",
			done.First)
	}
}

// And an answer that began at the beginning BECAUSE that is what was asked is the
// same answer either way: nought is nought whoever said it.
func TestAPositionOfNoughtNeedsNothingSaid(t *testing.T) {
	src := serving(t)
	_, done := read(t, src, `source="files"`, `count=2`)
	if done.First.Exact {
		t.Errorf("a scope that asked for no position was told it began at %v; there"+
			" is nothing here to answer", done.First)
	}
}

// **Silence still means silence for a scope that named a RECORD.** `after` is a
// position said better -- the reader holds the record it wants to carry on past --
// so there is nothing for this end to supply and nothing it could supply honestly:
// an application resuming from a record it was handed need not know what number
// that record is.
func TestSilenceIsLeftAloneForAScopeThatNamedARecord(t *testing.T) {
	src := serving(t)
	got, done := read(t, src, `source="files"`, `after=1 count=2`)
	if want := []string{"2", "3"}; !sameKeys(got.keys, want) {
		t.Fatalf("the answer holds %v, want %v", got.keys, want)
	}
	if done.First.Exact {
		t.Errorf("it invented a position of %v for an answer that carried on from"+
			" a record", done.First)
	}
}

// A refusal says nothing about where anything began, there being no records to
// place. Filling one in would make an error look like an answer that started at the
// top.
func TestARefusalIsNotAnAnswerThatBeganSomewhere(t *testing.T) {
	src := hosting(t, func(f *client.Fill) {
		_ = f.Fail("this source is having none of it")
	})
	_, done := read(t, src, `source="files"`, `from=2 count=2`)
	if done.Error == "" {
		t.Fatalf("the refusal did not arrive: %+v", done)
	}
	if done.First.Exact {
		t.Errorf("a refusal says it began at %v", done.First)
	}
}

// sameKeys compares what arrived with what was wanted.
func sameKeys(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
