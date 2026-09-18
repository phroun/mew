package trinkets

// What a list knows about where its rows are, and what it does about the rows
// it knows nothing about.

import (
	"testing"

	"github.com/phroun/serval"
)

// ids numbers identities, so a position and an identity are the same figure and
// a test can say which row it meant by saying where.
func ids(from, n int) []*serval.Value {
	out := make([]*serval.Value, n)
	for i := range out {
		out[i] = serval.NewInt(int64(from + i))
	}
	return out
}

func rowAt(t *testing.T, s *spine, pos int) int64 {
	t.Helper()
	id, ok := s.idAt(pos)
	if !ok {
		t.Fatalf("row %d is blank, and something was expected there", pos)
	}
	return id.Int
}

// What was placed can be read back, by position and by identity, and the two
// agree.
func TestASpineAnswersBothWays(t *testing.T) {
	s := &spine{}
	s.place(100, ids(100, 20))

	if got := rowAt(t, s, 100); got != 100 {
		t.Errorf("row 100 holds %d", got)
	}
	if got := rowAt(t, s, 119); got != 119 {
		t.Errorf("row 119 holds %d", got)
	}
	if pos, ok := s.posOf(serval.NewInt(105)); !ok || pos != 105 {
		t.Errorf("record 105 stands at %d (%v)", pos, ok)
	}
	if _, ok := s.posOf(serval.NewInt(500)); ok {
		t.Error("it placed a record it was never told about")
	}
}

// A row outside what is held is BLANK, which is a state and not a failure. The
// list knows a row stands there because it knows how long the sequence is.
func TestARowOutsideWhatIsHeldIsBlank(t *testing.T) {
	s := &spine{}
	s.learn(serval.Complete{Total: serval.Exactly(1000)})
	s.place(100, ids(100, 20))

	for _, pos := range []int{0, 99, 120, 999} {
		if _, ok := s.idAt(pos); ok {
			t.Errorf("row %d came back named, and nothing placed it", pos)
		}
	}
	if s.rows() != 1000 {
		t.Errorf("it would draw %d rows, want 1000", s.rows())
	}
}

// Scrolling EXTENDS what is held rather than fragmenting it: a reader asks for
// what comes after what it has, and the two are one stretch.
func TestRunsThatMeetBecomeOne(t *testing.T) {
	s := &spine{}
	s.place(0, ids(0, 10))
	s.place(10, ids(10, 10))
	if len(s.runs) != 1 {
		t.Fatalf("two touching runs stayed %d runs", len(s.runs))
	}
	if got := rowAt(t, s, 15); got != 15 {
		t.Errorf("row 15 holds %d", got)
	}
	if s.held() != 20 {
		t.Errorf("it holds %d identities, want 20", s.held())
	}

	// Overlapping is the same, and what arrived last wins where they disagree.
	s.place(18, ids(1018, 5))
	if len(s.runs) != 1 {
		t.Fatalf("an overlap made %d runs", len(s.runs))
	}
	if got := rowAt(t, s, 18); got != 1018 {
		t.Errorf("row 18 holds %d, want the newer word 1018", got)
	}
	if got := rowAt(t, s, 17); got != 17 {
		t.Errorf("row 17 holds %d, and nothing newer touched it", got)
	}
}

// A run somewhere else stays its own run: a thumb dragged across a long list
// leaves the reader in two places for a moment, and neither is the other.
func TestARunSomewhereElseStaysApart(t *testing.T) {
	s := &spine{}
	s.place(0, ids(0, 10))
	s.place(500, ids(500, 10))
	if len(s.runs) != 2 {
		t.Fatalf("it made %d runs of two stretches far apart", len(s.runs))
	}
	if _, ok := s.idAt(200); ok {
		t.Error("the gap between them came back named")
	}
	if got := rowAt(t, s, 505); got != 505 {
		t.Errorf("row 505 holds %d", got)
	}
}

// The spine is a WINDOW and never a log. A reader scrolling the length of a
// long sequence leaves nothing behind it.
func TestScrollingALongWayLeavesNothingBehind(t *testing.T) {
	s := &spine{}
	s.learn(serval.Complete{Total: serval.Exactly(100000)})
	for at := 0; at < 100000; at += 40 {
		s.place(at, ids(at, 40))
	}
	if s.held() > spineKept {
		t.Errorf("it is carrying %d identities, and the cap is %d", s.held(), spineKept)
	}
	// And what it kept is where the reader ended up.
	if _, ok := s.idAt(99960); !ok {
		t.Error("it forgot the rows it had just been sent")
	}
	if _, ok := s.idAt(0); ok {
		t.Error("it is still holding the rows it started at")
	}
}

// What is dropped is what is furthest from the reader, and never the run the
// reader is in.
func TestWhatGoesIsWhatIsFurthestAway(t *testing.T) {
	s := &spine{}
	s.place(0, ids(0, 200))       // far
	s.place(5000, ids(5000, 200)) // further still
	s.place(1000, ids(1000, 200)) // and here is the reader

	if _, ok := s.idAt(1100); !ok {
		t.Fatal("it dropped the run the reader is in")
	}
	if _, ok := s.idAt(5100); ok {
		t.Error("it kept the run furthest from the reader")
	}
	if _, ok := s.idAt(100); !ok {
		t.Error("it dropped the nearer of the two it could have")
	}
}

// A notice says the rows may have MOVED, so every position goes. How many there
// are is a different fact and survives -- which is what keeps a thumb from
// flickering to nothing and back every time the data changes underneath it.
func TestForgettingDropsPositionsAndKeepsTheLength(t *testing.T) {
	s := &spine{}
	s.learn(serval.Complete{Total: serval.Exactly(1000)})
	s.place(100, ids(100, 20))

	s.forget()

	if _, ok := s.idAt(105); ok {
		t.Error("a position survived a notice")
	}
	if s.held() != 0 {
		t.Errorf("it is still holding %d identities", s.held())
	}
	if got := s.length(); !got.Exact || got.N != 1000 {
		t.Errorf("the length is %v, and nothing said it had changed", got)
	}
}

// A figure only replaces one that says less, so an answer that could not count
// does not throw away one that could.
func TestALengthIsNotUnlearnedByAnAnswerThatCouldNotCount(t *testing.T) {
	s := &spine{}
	s.learn(serval.Complete{Total: serval.Exactly(1000)})
	s.learn(serval.Complete{Total: serval.Unknown()})
	if got := s.length(); !got.Exact || got.N != 1000 {
		t.Errorf("Unknown took the count away: %v", got)
	}

	// A floor does not replace a count either, but a count replaces a floor.
	s.learn(serval.Complete{Total: serval.AtLeast(40)})
	if got := s.length(); !got.Exact || got.N != 1000 {
		t.Errorf("a floor took the count away: %v", got)
	}

	fresh := &spine{}
	fresh.learn(serval.Complete{Total: serval.AtLeast(40)})
	if got := fresh.length(); got.Exact || got.N != 40 {
		t.Fatalf("a floor did not land: %v", got)
	}
	fresh.learn(serval.Complete{Total: serval.Exactly(90)})
	if got := fresh.length(); !got.Exact || got.N != 90 {
		t.Errorf("a count did not replace a floor: %v", got)
	}
}

// A sequence nobody has counted still has the rows somebody was sent, so there
// is something to draw before anything says how long it is.
func TestRowsFallBackToWhatWasPlaced(t *testing.T) {
	s := &spine{}
	s.place(0, ids(0, 30))
	if s.rows() != 30 {
		t.Errorf("with no length it would draw %d rows, want 30", s.rows())
	}
	s.learn(serval.Complete{Total: serval.Exactly(500)})
	if s.rows() != 500 {
		t.Errorf("with a length it would draw %d rows, want 500", s.rows())
	}
}

// Nothing placed at a negative position, and nothing placed from an empty
// answer -- both of which are what an unplaceable answer looks like arriving.
func TestNothingIsPlacedFromNothing(t *testing.T) {
	s := &spine{}
	s.place(-1, ids(0, 5))
	s.place(10, nil)
	if s.held() != 0 {
		t.Errorf("it placed %d identities out of nothing", s.held())
	}
}

// Trimming has to FREE what it drops.
//
// A slice of a long run holds the whole of its backing array alive, so cutting
// one down by reslicing would leave the memory exactly where it was while
// reporting a small window -- which is the one failure this whole type exists to
// prevent, and the one a count of identities cannot see.
func TestTrimmingActuallyFreesWhatItDrops(t *testing.T) {
	s := &spine{}
	s.learn(serval.Complete{Total: serval.Exactly(100000)})
	for at := 0; at < 100000; at += 40 {
		s.place(at, ids(at, 40))
	}
	for i, r := range s.runs {
		if cap(r.ids) > spineKept {
			t.Errorf("run %d reports %d rows and is still holding an array of %d",
				i, len(r.ids), cap(r.ids))
		}
	}
}
