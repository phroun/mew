package source

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/phroun/kittytk/wire"
)

func ent(id int64) *entry {
	return newEntry(wire.NewInt(id), wire.Fields{
		wire.Named("name", fmt.Sprintf("file-%d", id)),
		wire.Named("size", id*10),
	}, true, 0)
}

// run builds a cached scope holding the records from..to, guaranteed between
// the ends given.
func run(set string, begin, end *wire.Value, from, to int64) *cachedScope {
	s := &cachedScope{id: 1, set: set, begin: begin, end: end}
	for i := from; i <= to; i++ {
		s.pushBack(ent(i))
	}
	return s
}

func walk(s *cachedScope) string {
	out := ""
	for e := s.head; e != nil; e = e.next {
		if out != "" {
			out += ","
		}
		out += wire.EncodeValue(e.id)
	}
	return out
}

// backwards reads the same run the other way, which is the check that both sets
// of links are right rather than only the ones a forward walk follows.
func backwards(s *cachedScope) string {
	out := ""
	for e := s.tail; e != nil; e = e.prev {
		if out != "" {
			out += ","
		}
		out += wire.EncodeValue(e.id)
	}
	return out
}

func ids(es []*entry) string {
	out := ""
	for _, e := range es {
		if out != "" {
			out += ","
		}
		out += wire.EncodeValue(e.id)
	}
	return out
}

// Both ends absent is the whole sequence, which is what the simplest possible
// source leaves behind: send everything, say exhausted.
func TestARunWithNoEndsIsTheWholeSequence(t *testing.T) {
	s := run("files", nil, nil, 1, 5)
	if walk(s) != "1,2,3,4,5" || backwards(s) != "5,4,3,2,1" {
		t.Errorf("it reads %s forwards and %s backwards", walk(s), backwards(s))
	}
	if s.Len() != 5 || s.Cost() <= 0 {
		t.Errorf("%d records at %d", s.Len(), s.Cost())
	}
}

// Serving is walking links from where the scope starts, either way, and it stops
// at the run's own end because that is where the guarantee stops.
func TestServingWalksTheLinksEitherWay(t *testing.T) {
	s := run("files", nil, nil, 1, 5)
	third := s.head.next.next // record 3

	if got := ids(s.serve(third, 2, false)); got != "4,5" {
		t.Errorf("forwards from 3 it served %s", got)
	}
	if got := ids(s.serve(third, 2, true)); got != "2,1" {
		t.Errorf("backwards from 3 it served %s", got)
	}
	// Asking for more than the run holds stops at the end rather than running
	// off it: past the end is past the guarantee.
	if got := ids(s.serve(third, 99, false)); got != "4,5" {
		t.Errorf("forwards from 3 for 99 it served %s", got)
	}
	// From the run's own beginning, and from its end read backwards.
	if got := ids(s.serve(nil, 2, false)); got != "1,2" {
		t.Errorf("from the beginning it served %s", got)
	}
	if got := ids(s.serve(nil, 2, true)); got != "5,4" {
		t.Errorf("from the end backwards it served %s", got)
	}
}

// An insert cuts the run in two, and nothing is copied or re-queried.
func TestAnInsertSplitsTheRun(t *testing.T) {
	s := run("files", nil, wire.NewInt(5), 1, 5)
	whole, count := s.Cost(), s.Len()
	at := s.head.next.next // record 3

	right := s.splitAfter(at, 2)
	if right == nil {
		t.Fatal("the cut made nothing")
	}
	if walk(s) != "1,2,3" || backwards(s) != "3,2,1" {
		t.Errorf("the first run is %s / %s", walk(s), backwards(s))
	}
	if walk(right) != "4,5" || backwards(right) != "5,4" {
		t.Errorf("the second run is %s / %s", walk(right), backwards(right))
	}
	if wire.EncodeValue(s.end) != "3" || wire.EncodeValue(right.begin) != "3" {
		t.Errorf("the guarantees meet at %s and %s",
			wire.EncodeValue(s.end), wire.EncodeValue(right.begin))
	}
	if s.Len()+right.Len() != count || s.Cost()+right.Cost() != whole {
		t.Errorf("the halves hold %d at %d, the whole held %d at %d",
			s.Len()+right.Len(), s.Cost()+right.Cost(), count, whole)
	}
	// Every entry says which guarantee it falls under.
	for _, c := range []struct {
		of   *cachedScope
		want cachedScopeID
	}{{s, s.id}, {right, right.id}} {
		for e := c.of.head; e != nil; e = e.next {
			if e.scope != c.want {
				t.Errorf("record %s says it is in run %d, and it is in %d",
					wire.EncodeValue(e.id), e.scope, c.want)
			}
		}
	}
}

// The shorter side is the one relabelled, the id being opaque.
func TestTheShorterSideIsRelabelled(t *testing.T) {
	s := run("files", nil, wire.NewInt(9), 1, 9)
	at := s.head.next // record 2, so two on the left and seven on the right

	right := s.splitAfter(at, 42)
	if s.id != 42 || right.id != 1 {
		t.Errorf("the short side kept %d and the long side took %d", s.id, right.id)
	}
}

// A run cut in two goes back together, which is what two scopes answered back
// to back amount to.
func TestRunsThatMeetGoBackTogether(t *testing.T) {
	s := run("files", nil, wire.NewInt(5), 1, 5)
	whole, count := s.Cost(), s.Len()
	right := s.splitAfter(s.head.next.next, 2)

	if !s.merge(right) {
		t.Fatal("the halves would not join")
	}
	if walk(s) != "1,2,3,4,5" || backwards(s) != "5,4,3,2,1" {
		t.Errorf("rejoined it reads %s / %s", walk(s), backwards(s))
	}
	if s.Len() != count || s.Cost() != whole || wire.EncodeValue(s.end) != "5" {
		t.Errorf("rejoined it is %d at %d up to %s",
			s.Len(), s.Cost(), wire.EncodeValue(s.end))
	}
	for e := s.head; e != nil; e = e.next {
		if e.scope != s.id {
			t.Errorf("record %s still says run %d", wire.EncodeValue(e.id), e.scope)
		}
	}
}

// Two runs with anything unaccounted for between them are two runs.
func TestRunsThatDoNotMeetDoNotJoin(t *testing.T) {
	a := run("files", nil, wire.NewInt(3), 1, 3)
	if a.merge(run("files", wire.NewInt(7), wire.NewInt(9), 8, 9)) {
		t.Error("two runs with a gap between them were joined")
	}
	if a.merge(run("colours", wire.NewInt(3), wire.NewInt(5), 4, 5)) {
		t.Error("runs of two different sequences were joined")
	}
	// And one guaranteed to the end of the sequence has nothing to join to.
	whole := run("files", nil, nil, 1, 3)
	if whole.merge(run("files", wire.NewInt(3), wire.NewInt(4), 4, 4)) {
		t.Error("something was joined past the end of the sequence")
	}
	// Least obviously: a run that goes to the end of the sequence and one that
	// comes from the start of it do not meet EACH OTHER either, however they are
	// spelled. An absent end and an absent beginning are opposite claims, not two
	// halves of the same one -- joining them would say everything between the
	// first run's last record and the second run's first is here, which is the one
	// thing neither of them ever said.
	tail := run("files", wire.NewInt(3), nil, 4, 6)
	if tail.merge(run("files", nil, wire.NewInt(2), 1, 2)) {
		t.Error("the end of the sequence was joined to the start of it")
	}
	// Nor do two runs carrying different fields, however their ends line up: a
	// run half of which holds `name` and half `size` covers neither.
	named := run("files", nil, wire.NewInt(3), 1, 3)
	named.carried = wire.Fields{wire.Named("name", 0)}
	sized := run("files", wire.NewInt(3), wire.NewInt(5), 4, 5)
	sized.carried = wire.Fields{wire.Named("size", 0)}
	if named.merge(sized) {
		t.Error("runs carrying different fields were joined")
	}
}

// A record the cache has let go of lets go of the cache.
//
// An unlinked entry that kept its links would still reach the run it was dropped
// from: nothing it was handed to could be told apart from a record still held,
// and the run could not be collected while anything remembered one evicted
// record of it -- so the cost would come off the total and not off the heap,
// which is the one thing the total is for.
func TestADroppedRecordDoesNotReachWhatIsStillHeld(t *testing.T) {
	s := run("files", nil, wire.NewInt(5), 1, 5)
	dropped := s.head

	s.trimFront(1)
	if dropped.next != nil || dropped.prev != nil {
		t.Error("a dropped record still points into the run")
	}
	if got := ids(s.serve(dropped, 3, false)); got != "" {
		t.Errorf("reading on from a dropped record served %s", got)
	}
}

// Trimming an end keeps the claim true: the records go and the end moves in with
// them, so what is guaranteed between the ends is as good as it was.
func TestTrimmingAnEndKeepsTheClaimTrue(t *testing.T) {
	s := run("files", nil, wire.NewInt(5), 1, 5)
	whole := s.Cost()

	freed := s.trimFront(2)
	if walk(s) != "3,4,5" || wire.EncodeValue(s.begin) != "2" {
		t.Errorf("trimmed at the front it is %s from %s", walk(s), wire.EncodeValue(s.begin))
	}
	if s.Cost()+freed != whole {
		t.Errorf("it freed %d and shrank by %d", freed, whole-s.Cost())
	}
	if backwards(s) != "5,4,3" {
		t.Errorf("the back links did not follow: %s", backwards(s))
	}

	freed = s.trimBack(1)
	if walk(s) != "3,4" || wire.EncodeValue(s.end) != "4" || backwards(s) != "4,3" {
		t.Errorf("trimmed at the back it is %s / %s up to %s",
			walk(s), backwards(s), wire.EncodeValue(s.end))
	}
	if freed <= 0 {
		t.Error("trimming the back freed nothing")
	}
}

// Trimming everything leaves a run of none, still saying where it was.
func TestTrimmingTheWholeRunLeavesNothing(t *testing.T) {
	s := run("files", nil, wire.NewInt(3), 1, 3)
	if freed := s.trimFront(99); freed == 0 || s.Len() != 0 || s.Cost() != 0 {
		t.Errorf("%d records at %d after trimming the lot, freeing %d",
			s.Len(), s.Cost(), freed)
	}
	if s.head != nil || s.tail != nil {
		t.Error("an emptied run still points at records")
	}
}

// What an entry costs is worked out once and kept, so that eviction gives back
// exactly what insertion took however wrong the estimate is.
func TestAnEntrysCostIsWorkedOutOnceAndKept(t *testing.T) {
	s := run("files", nil, nil, 1, 4)
	whole := s.Cost()

	// Patched after it went in, as an invalidation would, it still costs what it
	// cost.
	s.head.fields = append(s.head.fields, wire.Named("extra", "a much longer value"))
	if freed := s.trimFront(1); s.Cost()+freed != whole {
		t.Errorf("a patched entry gave back %d of the %d it took", freed, whole-s.Cost())
	}
}

// The estimate is nominal rather than exact -- but it has to keep tracking
// reality, or a byte limit stops meaning anything.
//
// Not an exact assertion: it fails when the shape of what is held changes enough
// to move the estimate off by more than a factor, and not when a field is added
// somewhere.
func TestTheEstimateTracksTheRealHeap(t *testing.T) {
	const n = 20000
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	s := run("files", nil, nil, 1, n)

	runtime.GC()
	runtime.ReadMemStats(&after)
	real := int(after.HeapAlloc - before.HeapAlloc)
	runtime.KeepAlive(s)

	ratio := float64(s.Cost()) / float64(real)
	t.Logf("estimated %d bytes, the heap grew %d, ratio %.2f", s.Cost(), real, ratio)
	if ratio < 0.5 || ratio > 2.0 {
		t.Errorf("the estimate is %.2f of the real heap, too far off to cap "+
			"anything by -- recalibrate the overheads in cachedscope.go", ratio)
	}
}
