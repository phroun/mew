package source

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/phroun/kittytk/wire"
)

func rec(id int64, name string) cached {
	v := wire.NewInt(id)
	f := wire.Fields{wire.Named("name", name), wire.Named("size", id*10)}
	return cached{id: v, fields: f, whole: true, size: sizeOf(v, f)}
}

func runOf(from, to int64) []cached {
	var out []cached
	for i := from; i <= to; i++ {
		out = append(out, rec(i, fmt.Sprintf("file-%d", i)))
	}
	return out
}

func ids(e *cachedScope) string {
	s := ""
	for i, r := range e.recs {
		if i > 0 {
			s += ","
		}
		s += wire.EncodeValue(r.id)
	}
	return s
}

// Both ends absent is the whole sequence, which is what the simplest possible
// source produces: send everything, say exhausted.
func TestAnExtentWithNoEndsIsTheWholeSequence(t *testing.T) {
	e := newCachedScope("files", nil, nil, runOf(1, 5))
	if i, ok := e.from(nil); !ok || i != 0 {
		t.Errorf("it could not serve a scope from the beginning: %d %v", i, ok)
	}
	if _, ok := e.from(wire.NewInt(3)); !ok {
		t.Error("it could not serve a scope from a record it holds")
	}
	if e.extend(wire.NewInt(5), runOf(6, 7), nil) {
		t.Error("something was appended past the end of the sequence")
	}
}

// A scope is served from the record it starts after, or from the cached scope's own
// start. An identity the cached scope knows nothing about is not its to answer.
func TestAnExtentServesFromWhatItHolds(t *testing.T) {
	e := newCachedScope("files", wire.NewInt(10), wire.NewInt(5), runOf(1, 5))
	for _, c := range []struct {
		after *wire.Value
		want  int
		ok    bool
	}{
		{wire.NewInt(10), 0, true}, // its own start
		{wire.NewInt(1), 1, true},  // the first record it holds
		{wire.NewInt(4), 4, true},
		{wire.NewInt(5), 5, true}, // its end: nothing left, but covered
		{wire.NewInt(99), 0, false},
		{nil, 0, false}, // the beginning of the sequence, which this is not
	} {
		i, ok := e.from(c.after)
		if i != c.want || ok != c.ok {
			t.Errorf("from %s gave %d,%v want %d,%v",
				wire.EncodeValue(c.after), i, ok, c.want, c.ok)
		}
	}
}

// A scope answered immediately past the end moves the end rather than making a
// second cached scope.
func TestAScopeAnsweredPastTheEndExtendsIt(t *testing.T) {
	e := newCachedScope("files", nil, wire.NewInt(3), runOf(1, 3))
	was := e.size

	if !e.extend(wire.NewInt(3), runOf(4, 6), wire.NewInt(6)) {
		t.Fatal("a run beginning exactly at the end was refused")
	}
	if ids(e) != "1,2,3,4,5,6" || wire.EncodeValue(e.end) != "6" {
		t.Errorf("it came out as %s up to %s", ids(e), wire.EncodeValue(e.end))
	}
	if e.size <= was {
		t.Error("the records went in and the size did not")
	}

	// A run that does not begin exactly at the end leaves a stretch nobody has
	// looked at, and joining across it would claim what nothing checked.
	if e.extend(wire.NewInt(9), runOf(10, 11), wire.NewInt(11)) {
		t.Error("a run with a gap before it was joined on anyway")
	}
}

// An insert inside a cached scope cuts it in two and re-queries nothing.
func TestAnInsertSlicesAnExtentInTwo(t *testing.T) {
	e := newCachedScope("files", nil, wire.NewInt(5), runOf(1, 5))
	left, right := e.slice(3)
	if left == nil || right == nil {
		t.Fatal("the cut did not make two")
	}
	if ids(left) != "1,2,3" || wire.EncodeValue(left.end) != "3" || left.start != nil {
		t.Errorf("the first half is %s up to %s", ids(left), wire.EncodeValue(left.end))
	}
	if ids(right) != "4,5" || wire.EncodeValue(right.start) != "3" {
		t.Errorf("the second half is %s from %s", ids(right), wire.EncodeValue(right.start))
	}
	if left.size+right.size != e.size {
		t.Errorf("the halves cost %d and the whole cost %d", left.size+right.size, e.size)
	}

	// And the two meet, so they go back together.
	if !left.merge(right) {
		t.Fatal("the halves would not join")
	}
	if ids(left) != "1,2,3,4,5" || left.size != e.size {
		t.Errorf("rejoined it is %s at %d bytes", ids(left), left.size)
	}
}

// Two cached scopes with anything unaccounted for between them are two cached scopes.
func TestExtentsThatDoNotMeetDoNotMerge(t *testing.T) {
	a := newCachedScope("files", nil, wire.NewInt(3), runOf(1, 3))
	b := newCachedScope("files", wire.NewInt(7), wire.NewInt(9), runOf(8, 9))
	if a.merge(b) {
		t.Error("two cached scopes with a gap between them were joined")
	}
	other := newCachedScope("colours", wire.NewInt(3), wire.NewInt(5), runOf(4, 5))
	if a.merge(other) {
		t.Error("cached scopes of two different sequences were joined")
	}
}

// Trimming an end keeps the cached scope true: the records go and the end moves in
// with them, so what is claimed between the ends is as good as it was.
func TestTrimmingAnEndKeepsTheClaimTrue(t *testing.T) {
	e := newCachedScope("files", nil, wire.NewInt(5), runOf(1, 5))
	whole := e.size

	freed := e.trimFront(2)
	if ids(e) != "3,4,5" || wire.EncodeValue(e.start) != "2" {
		t.Errorf("trimmed at the front it is %s from %s", ids(e), wire.EncodeValue(e.start))
	}
	if e.size+freed != whole {
		t.Errorf("it freed %d and shrank by %d", freed, whole-e.size)
	}
	// Still complete between its ends, and still serving from its own start.
	if i, ok := e.from(wire.NewInt(2)); !ok || i != 0 {
		t.Errorf("after trimming it serves from %d,%v", i, ok)
	}

	freed = e.trimBack(1)
	if ids(e) != "3,4" || wire.EncodeValue(e.end) != "4" {
		t.Errorf("trimmed at the back it is %s up to %s", ids(e), wire.EncodeValue(e.end))
	}
	if freed <= 0 {
		t.Error("trimming the back freed nothing")
	}
}

// What a record costs is worked out once and kept, so that eviction gives back
// exactly what insertion took however wrong the estimate is.
func TestARecordsCostIsWorkedOutOnceAndKept(t *testing.T) {
	e := newCachedScope("files", nil, nil, runOf(1, 4))
	whole := e.size

	// A record patched after it went in still costs what it cost.
	e.recs[0].fields = append(e.recs[0].fields, wire.Named("extra", "much longer value"))
	if freed := e.trimFront(1); e.size+freed != whole {
		t.Errorf("a patched record gave back %d of the %d it took", freed, whole-e.size)
	}
}

// The estimate is nominal, not exact -- but it has to keep tracking reality, or
// a byte limit stops meaning anything at all.
//
// Not an exact assertion: it fails when the shape of what is cached changes
// enough to move the estimate off by more than a factor, and not when a field
// is added somewhere.
func TestTheEstimateTracksTheRealHeap(t *testing.T) {
	const n = 20000
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	e := newCachedScope("files", nil, nil, runOf(1, n))

	runtime.GC()
	runtime.ReadMemStats(&after)
	real := int(after.HeapAlloc - before.HeapAlloc)
	runtime.KeepAlive(e)

	ratio := float64(e.size) / float64(real)
	t.Logf("estimated %d bytes, heap grew %d, ratio %.2f", e.size, real, ratio)
	if ratio < 0.5 || ratio > 2.0 {
		t.Errorf("the estimate is %.2f of the real heap, which is too far off to "+
			"cap anything by -- recalibrate the overheads in cached scope.go", ratio)
	}
}
