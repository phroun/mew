package trinkets

// What a list knows about where its rows are, and what it does about the rows
// it knows nothing about.

import (
	"testing"

	"github.com/phroun/serval"
)

// ids numbers identities, so a position and an identity are the same figure and
// a test can say which row it meant by saying where. Every one stands at the top,
// which is where a list's rows all stand.
func ids(from, n int) []named {
	out := make([]named, n)
	for i := range out {
		out[i] = named{id: serval.NewInt(int64(from + i))}
	}
	return out
}

// deep is the same rows with a depth each, for the tree's half of the spine.
func deep(from int, depths ...int) []named {
	out := make([]named, len(depths))
	for i, d := range depths {
		out[i] = named{id: serval.NewInt(int64(from + i)), deep: d}
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
		if cap(r.rows) > spineKept {
			t.Errorf("run %d reports %d rows and is still holding an array of %d",
				i, len(r.rows), cap(r.rows))
		}
	}
}

// --- the sequence moving, which is not the records changing ---------------
//
// A tree opening a node is not an invalidation: every level is a data set of its
// own, so no record went stale and no level reordered. What moved is where the rows
// below the mark STAND. So the spine has to be able to say that, or a view would
// have to forget what it holds to learn what it already knew.

// Rows appearing at a position push everything from there on down, and leave blanks
// behind them.
func TestRowsAppearingPushWhatIsBelowThemDown(t *testing.T) {
	s := &spine{}
	s.learn(serval.Complete{Total: serval.Exactly(100)})
	s.place(0, ids(0, 20))

	s.grew(5, 3)

	// Above the mark, nothing moved.
	for _, at := range []int{0, 4} {
		if got := rowAt(t, s, at); got != int64(at) {
			t.Errorf("row %d holds %d, and nothing above the mark moved", at, got)
		}
	}
	// At the mark, three blanks.
	for at := 5; at < 8; at++ {
		if _, ok := s.idAt(at); ok {
			t.Errorf("row %d came back named, and three rows appeared there", at)
		}
	}
	// Below it, everything stands three further down.
	if got := rowAt(t, s, 8); got != 5 {
		t.Errorf("row 8 holds %d, want the record that was at 5", got)
	}
	if got := rowAt(t, s, 22); got != 19 {
		t.Errorf("row 22 holds %d, want the record that was at 19", got)
	}
	if got := s.length(); !got.Exact || got.N != 103 {
		t.Errorf("the length reads %v, want exactly 103", got)
	}
	if s.held() != 20 {
		t.Errorf("it is holding %d identities; nothing was dropped or invented", s.held())
	}
}

// Rows going take themselves out and pull everything after them up.
func TestRowsGoingPullWhatIsAfterThemUp(t *testing.T) {
	s := &spine{}
	s.learn(serval.Complete{Total: serval.Exactly(100)})
	s.place(0, ids(0, 20))

	s.shrank(5, 3)

	if got := rowAt(t, s, 4); got != 4 {
		t.Errorf("row 4 holds %d, and nothing above the mark moved", got)
	}
	if got := rowAt(t, s, 5); got != 8 {
		t.Errorf("row 5 holds %d, want the record that was at 8", got)
	}
	if got := rowAt(t, s, 16); got != 19 {
		t.Errorf("row 16 holds %d, want the record that was at 19", got)
	}
	if got := s.length(); !got.Exact || got.N != 97 {
		t.Errorf("the length reads %v, want exactly 97", got)
	}
	if s.held() != 17 {
		t.Errorf("it is holding %d identities, want the seventeen that are left", s.held())
	}
	// And the three that went are not anywhere.
	for _, gone := range []int64{5, 6, 7} {
		if at, ok := s.posOf(serval.NewInt(gone)); ok {
			t.Errorf("record %d is still at %d", gone, at)
		}
	}
}

// **Forgetting from a mark keeps what is above it**, which is very often the whole
// of what is on screen -- and floors the length there, because what is below is the
// part nobody can now count.
func TestForgettingFromAMarkKeepsWhatIsAboveIt(t *testing.T) {
	s := &spine{}
	s.learn(serval.Complete{Total: serval.Exactly(100)})
	s.place(0, ids(0, 20))

	s.forgetFrom(8)

	if got := rowAt(t, s, 7); got != 7 {
		t.Errorf("row 7 holds %d, and it is above the mark", got)
	}
	for _, at := range []int{8, 9, 19} {
		if _, ok := s.idAt(at); ok {
			t.Errorf("row %d survived a forget from 8", at)
		}
	}
	if s.held() != 8 {
		t.Errorf("it is holding %d identities, want the eight above the mark", s.held())
	}
	if got := s.length(); got.Exact || got.N != 8 {
		t.Errorf("the length reads %v, want a floor of eight", got)
	}
}

// Forgetting from the top is forgetting the lot, and then there is no floor either:
// a view that knows nothing about row nought knows nothing about the length.
func TestForgettingFromTheTopForgetsEverything(t *testing.T) {
	s := &spine{}
	s.learn(serval.Complete{Total: serval.Exactly(100)})
	s.place(0, ids(0, 20))

	s.forgetFrom(0)

	if s.held() != 0 {
		t.Errorf("it is still holding %d identities", s.held())
	}
	if got := s.length(); !got.Nothing() {
		t.Errorf("the length reads %v, want nothing known", got)
	}
}

// A run STRADDLING the mark is split, so the half above it keeps its positions and
// the half below moves. It is the case a reader is always in: the rows on screen
// are one run and the node being opened is in the middle of them.
func TestAStraddlingRunIsSplitRatherThanDropped(t *testing.T) {
	s := &spine{}
	s.place(100, ids(100, 20)) // rows 100..119

	s.grew(110, 5)

	if got := rowAt(t, s, 109); got != 109 {
		t.Errorf("row 109 holds %d, and it is above the mark", got)
	}
	if _, ok := s.idAt(112); ok {
		t.Error("a row inside the stretch that appeared came back named")
	}
	if got := rowAt(t, s, 115); got != 110 {
		t.Errorf("row 115 holds %d, want the record that was at 110", got)
	}
	if s.held() != 20 {
		t.Errorf("splitting lost or invented rows: it holds %d, want 20", s.held())
	}
}

// A tree's rows carry a DEPTH, which is what makes a subtree's end findable from
// what is held -- and so what tells a collapse how many rows are going.
func TestAHeldRowKnowsHowDeepItStands(t *testing.T) {
	s := &spine{}
	s.place(0, deep(0, 0, 1, 2, 2, 1, 0))

	for at, want := range []int{0, 1, 2, 2, 1, 0} {
		got, ok := s.deepAt(at)
		if !ok {
			t.Fatalf("row %d is blank", at)
		}
		if got != want {
			t.Errorf("row %d stands at depth %d, want %d", at, got, want)
		}
	}
	if _, ok := s.deepAt(6); ok {
		t.Error("a blank came back with a depth")
	}
	// A list's rows all stand at the top, and say so.
	flat := &spine{}
	flat.place(0, ids(0, 3))
	if got, _ := flat.deepAt(1); got != 0 {
		t.Errorf("a list's row stands at depth %d, want the top", got)
	}
}

// Nothing moves for a delta of nothing, or for one at a position that is not one.
func TestNothingShiftsForNothing(t *testing.T) {
	s := &spine{}
	s.learn(serval.Complete{Total: serval.Exactly(50)})
	s.place(0, ids(0, 10))

	s.grew(5, 0)
	s.grew(-1, 3)
	s.shrank(5, 0)
	s.shrank(-1, 3)

	if got := rowAt(t, s, 5); got != 5 {
		t.Errorf("row 5 holds %d after four shifts of nothing", got)
	}
	if got := s.length(); !got.Exact || got.N != 50 {
		t.Errorf("the length reads %v", got)
	}
}
