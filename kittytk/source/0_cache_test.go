package source

import (
	"testing"

	"github.com/phroun/kittytk/wire"
)

// slim builds an entry carrying only the fields named, which is what a query
// asking `fields={ ... }` leaves behind.
func slim(id int64, names ...string) *entry {
	f := wire.Fields{}
	for _, n := range names {
		f = append(f, wire.Named(n, id))
	}
	return newEntry(wire.NewInt(id), f, false, 0)
}

// wad is a run of entries as a source would hand them back.
func wad(from, to int64) []*entry {
	out := []*entry{}
	for i := from; i <= to; i++ {
		out = append(out, ent(i))
	}
	return out
}

func slims(from, to int64, names ...string) []*entry {
	out := []*entry{}
	for i := from; i <= to; i++ {
		out = append(out, slim(i, names...))
	}
	return out
}

// filed holds an answer the way a source's reply would arrive: everything from
// `after` up to the last record, complete because the count was reached.
func filed(c *cache, set string, carried wire.Fields, after *wire.Value, recs []*entry) {
	sc := &wire.Scope{After: after, Count: len(recs)}
	c.hold(set, carried, sc, recs, wire.Complete{
		Stop: wire.StopFilled, Watermark: recs[len(recs)-1].id,
	})
}

// ended holds an answer that ran out of records, which claims the end of the
// sequence rather than a watermark.
func ended(c *cache, set string, carried wire.Fields, after *wire.Value, recs []*entry) {
	sc := &wire.Scope{After: after, Count: len(recs) + 1}
	c.hold(set, carried, sc, recs, wire.Complete{Stop: wire.StopExhausted})
}

func asked(c *cache, set string, wanted wire.Fields, sc *wire.Scope) (string, wire.Stop, bool) {
	es, done, ok := c.serve(set, wanted, sc)
	return ids(es), done.Stop, ok
}

// inCache reports whether one record of a data set is still in the cache at all.
func inCache(c *cache, set string, id int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.at[recordKey(set, wire.NewInt(id))]) > 0
}

// A scope answered once is answered again from what was filed, rather than
// asked a second time.
func TestAnAnsweredScopeIsAnsweredAgainFromTheCache(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, "files", nil, nil, wad(1, 5))

	got, stop, ok := asked(c, "files", nil, &wire.Scope{Count: 3})
	if !ok || got != "1,2,3" || stop != wire.StopFilled {
		t.Errorf("from the beginning it gave %s / %s, held %v", got, stop, ok)
	}
	// And from the middle of it, which is where a reader scrolling would ask.
	got, stop, ok = asked(c, "files", nil, &wire.Scope{After: wire.NewInt(2), Count: 2})
	if !ok || got != "3,4" || stop != wire.StopFilled {
		t.Errorf("after 2 it gave %s / %s, held %v", got, stop, ok)
	}
}

// A scope reaching past what the run guarantees is a miss, and not a short
// answer: where the run stops is where the knowledge stops, and there may be
// more records there.
func TestAScopePastTheGuaranteeIsAMiss(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, "files", nil, nil, wad(1, 5))

	if got, _, ok := asked(c, "files", nil, &wire.Scope{Count: 9}); ok {
		t.Errorf("it answered %s for nine records out of a run of five", got)
	}
	if _, _, ok := asked(c, "files", nil, &wire.Scope{After: wire.NewInt(5), Count: 1}); ok {
		t.Error("it answered past the end of what it holds")
	}
	// A record of another sequence entirely is not held at all.
	if _, _, ok := asked(c, "colours", nil, &wire.Scope{Count: 1}); ok {
		t.Error("it answered for a sequence it has never seen")
	}
}

// A run that reaches the end of the sequence answers a scope running off it,
// because there is nothing there to be missing.
func TestARunToTheEndAnswersPastItsLastRecord(t *testing.T) {
	c := newCache(1 << 20)
	ended(c, "files", nil, nil, wad(1, 3))

	got, stop, ok := asked(c, "files", nil, &wire.Scope{Count: 9})
	if !ok || got != "1,2,3" || stop != wire.StopExhausted {
		t.Errorf("it gave %s / %s, held %v", got, stop, ok)
	}
	for _, sc := range []*wire.Scope{
		{After: wire.NewInt(3), Count: 2},
		{After: wire.NewInt(3)}, // and with no count, which is not a count of none
	} {
		got, stop, ok = asked(c, "files", nil, sc)
		if !ok || got != "" || stop != wire.StopExhausted {
			t.Errorf("past the last record it gave %s / %s, held %v", got, stop, ok)
		}
	}
}

// A scope naming no count asks for everything there is, so only a run reaching
// the end of the sequence can answer it.
func TestAScopeWithNoCountAsksToTheEnd(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, "files", nil, nil, wad(1, 4))
	if got, stop, ok := asked(c, "files", nil, &wire.Scope{}); ok {
		t.Errorf("a run stopping at 4 answered for everything: %s / %s", got, stop)
	}

	ended(c, "colours", nil, nil, wad(1, 4))
	got, stop, ok := asked(c, "colours", nil, &wire.Scope{})
	if !ok || got != "1,2,3,4" || stop != wire.StopExhausted {
		t.Errorf("a run to the end gave %s / %s, held %v", got, stop, ok)
	}
}

// A walk that reaches the record the asker already holds has joined two runs it
// held separately, and says so.
func TestAWalkThatReachesUntilIsJoined(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, "files", nil, nil, wad(1, 6))

	got, stop, ok := asked(c, "files", nil,
		&wire.Scope{After: wire.NewInt(1), Until: wire.NewInt(4), Count: 9})
	if !ok || got != "2,3" || stop != wire.StopJoined {
		t.Errorf("it gave %s / %s, held %v", got, stop, ok)
	}
}

// Read backwards a run answers the same way, the ends swapping over.
func TestAScopeReadBackwards(t *testing.T) {
	c := newCache(1 << 20)
	// Records arrive furthest-first, and the answer is guaranteed from the
	// watermark up to the record asked past.
	c.hold("files", nil, &wire.Scope{After: wire.NewInt(9), Count: 4, Reversed: true},
		[]*entry{ent(8), ent(7), ent(6), ent(5)},
		wire.Complete{Stop: wire.StopFilled, Watermark: wire.NewInt(5)})

	got, stop, ok := asked(c, "files", nil,
		&wire.Scope{After: wire.NewInt(9), Count: 2, Reversed: true})
	if !ok || got != "8,7" || stop != wire.StopFilled {
		t.Errorf("backwards from 9 it gave %s / %s, held %v", got, stop, ok)
	}
	// And forwards from inside the same run, which is the same records the
	// other way about.
	got, _, ok = asked(c, "files", nil, &wire.Scope{After: wire.NewInt(6), Count: 2})
	if !ok || got != "7,8" {
		t.Errorf("forwards from 6 it gave %s, held %v", got, ok)
	}
}

// Scrolling leaves one run rather than a run per screenful.
func TestScrollingJoinsOntoWhatIsAlreadyHeld(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, "files", nil, nil, wad(1, 3))
	filed(c, "files", nil, wire.NewInt(3), wad(4, 6))

	if n := len(c.sets["files"]); n != 1 {
		t.Errorf("two scopes back to back left %d runs", n)
	}
	got, _, ok := asked(c, "files", nil, &wire.Scope{Count: 6})
	if !ok || got != "1,2,3,4,5,6" {
		t.Errorf("the joined run reads %s, held %v", got, ok)
	}
	// And a scope crossing where the join was is one walk, not two.
	if got, _, ok := asked(c, "files", nil, &wire.Scope{After: wire.NewInt(2), Count: 3}); !ok || got != "3,4,5" {
		t.Errorf("across the join it gave %s, held %v", got, ok)
	}
}

// A run filed before the one it joins onto is joined just the same.
func TestARunFiledOutOfOrderStillJoins(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, "files", nil, wire.NewInt(3), wad(4, 6))
	filed(c, "files", nil, nil, wad(1, 3))

	if n := len(c.sets["files"]); n != 1 {
		t.Errorf("it left %d runs", n)
	}
	if got, _, ok := asked(c, "files", nil, &wire.Scope{Count: 6}); !ok || got != "1,2,3,4,5,6" {
		t.Errorf("it reads %s, held %v", got, ok)
	}
}

// What a run holds decides what it can answer, and holding more than was asked
// for is an answer.
func TestARunAnswersWhatItCovers(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, "files", wire.Fields{wire.Named("name", 0), wire.Named("size", 0)},
		nil, slims(1, 4, "name", "size"))

	if _, _, ok := asked(c, "files", wire.Fields{wire.Named("name", 0)}, &wire.Scope{Count: 2}); !ok {
		t.Error("a run carrying name and size would not answer for name")
	}
	if _, _, ok := asked(c, "files", wire.Fields{wire.Named("mode", 0)}, &wire.Scope{Count: 2}); ok {
		t.Error("a run carrying name and size answered for mode")
	}
	// The whole record is not covered by part of one.
	if _, _, ok := asked(c, "files", nil, &wire.Scope{Count: 2}); ok {
		t.Error("a run carrying two fields answered for the whole record")
	}
}

// Runs carrying different fields are different records as far as the cache is
// concerned, and live side by side over one data set.
func TestTwoRunsOfOneSequenceCarryingDifferentFields(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, "files", nil, nil, wad(1, 4)) // whole records
	filed(c, "files", wire.Fields{wire.Named("name", 0)}, nil, slims(1, 4, "name"))

	if n := len(c.sets["files"]); n != 2 {
		t.Errorf("they left %d runs rather than two", n)
	}
	// Each answers what it carries, and the whole-record run answers both.
	if _, _, ok := asked(c, "files", nil, &wire.Scope{Count: 4}); !ok {
		t.Error("the whole-record run would not answer for the whole record")
	}
	if _, _, ok := asked(c, "files", wire.Fields{wire.Named("name", 0)}, &wire.Scope{Count: 4}); !ok {
		t.Error("nothing answered for name")
	}
	// And they never join, however their ends line up.
	for _, s := range c.sets["files"] {
		if s.n != 4 {
			t.Errorf("a run holds %d records, so the two were joined", s.n)
		}
	}
}

// A record held by two runs carrying different fields is read from whichever of
// them can answer -- and from whichever can carry on past it, which is not
// always the same one.
func TestARecordHeldTwiceIsReadFromTheRunThatCanAnswer(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, "files", nil, nil, wad(1, 3))                                          // whole, stopping at 3
	filed(c, "files", wire.Fields{wire.Named("name", 0)}, nil, slims(1, 6, "name")) // and on to 6

	name := wire.Fields{wire.Named("name", 0)}
	if got, _, ok := asked(c, "files", name, &wire.Scope{After: wire.NewInt(3), Count: 2}); !ok || got != "4,5" {
		t.Errorf("past 3 it gave %s, held %v", got, ok)
	}
	// The whole-record run is the one that answers for the whole record, and it
	// stops at 3.
	if _, _, ok := asked(c, "files", nil, &wire.Scope{After: wire.NewInt(3), Count: 2}); ok {
		t.Error("it answered for whole records past the run that holds them")
	}
	// And neither carries a field neither was asked for.
	if _, _, ok := asked(c, "files", wire.Fields{wire.Named("mode", 0)},
		&wire.Scope{After: wire.NewInt(2), Count: 2}); ok {
		t.Error("a run answered for mode, which nothing here carries")
	}
}

// An answer overlapping records already held is dropped rather than held twice,
// there being nothing yet that decides which copy is right.
func TestAnOverlappingAnswerIsNotHeldTwice(t *testing.T) {
	c := newCache(1 << 20)
	filed(c, "files", nil, nil, wad(1, 4))
	was := c.Cost()

	filed(c, "files", nil, wire.NewInt(2), wad(3, 6))
	if c.Cost() != was {
		t.Errorf("an overlapping answer took the cache from %d to %d", was, c.Cost())
	}
	if inCache(c, "files", 5) {
		t.Error("it held part of the overlapping answer")
	}
}

// sound checks everything the cache says about itself against what it is
// actually holding. A cache whose books are wrong evicts the wrong things, or
// stops evicting at all, and neither shows up as a wrong answer until much
// later -- so it is checked after anything that moves records about.
func sound(t *testing.T, c *cache) {
	t.Helper()
	cost, warm, n := 0, 0, 0
	for _, s := range c.runs {
		if s.n == 0 {
			t.Error("an emptied run is still held")
		}
		cold, held := 0, 0
		for e := s.head; e != nil; e = e.next {
			cost, n, held = cost+e.cost, n+1, held+1
			if e.warm {
				warm += e.cost
			} else {
				cold += e.cost
			}
			if e.scope != s.id {
				t.Errorf("record %s sits in run %d saying it is in %d",
					wire.EncodeValue(e.id), s.id, e.scope)
			}
		}
		if held != s.n || cold != s.cold {
			t.Errorf("a run of %d says %d, and %d of it is on probation against %d",
				held, s.n, cold, s.cold)
		}
	}
	if cost != c.cost {
		t.Errorf("the runs hold %d and the total says %d", cost, c.cost)
	}
	if warm != c.warm {
		t.Errorf("%d is protected and the total says %d", warm, c.warm)
	}
	listed := 0
	for _, es := range c.at {
		listed += len(es)
	}
	if listed != n {
		t.Errorf("the lookup names %d records and the runs hold %d", listed, n)
	}
	for set, ss := range c.sets {
		for _, s := range ss {
			if c.runs[s.id] != s {
				t.Errorf("%q lists a run the cache has forgotten", set)
			}
		}
	}
	if c.cost > c.limit && c.cost > 0 {
		t.Errorf("it holds %d against a limit of %d", c.cost, c.limit)
	}
}

// What the cache says it is holding is what it is holding, through eviction.
func TestTheTotalIsWhatTheRunsHold(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 10)
	for i := int64(0); i < 8; i++ {
		filed(c, "files", nil, wire.NewInt(i*3), wad(i*3+1, i*3+3))
		asked(c, "files", nil, &wire.Scope{After: wire.NewInt(i * 3), Count: 3})
		sound(t, c)
	}
}

// The point of the two segments: a flood of records nothing has asked for twice
// does not push out the records that have proved themselves.
func TestAFloodDoesNotEvictWhatHasProvedItself(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 40)

	// Five records, read twice, so they are warm.
	filed(c, "proven", nil, nil, wad(1, 5))
	for i := 0; i < 2; i++ {
		if _, _, ok := asked(c, "proven", nil, &wire.Scope{Count: 5}); !ok {
			t.Fatal("the proven run would not answer")
		}
	}

	// Then a walk of two hundred records through the same cache, each read once
	// as it goes past, which is what a scan does.
	for i := int64(0); i < 40; i++ {
		filed(c, "flood", nil, wire.NewInt(1000+i*5), wad(1000+i*5+1, 1000+i*5+5))
		asked(c, "flood", nil, &wire.Scope{After: wire.NewInt(1000 + i*5), Count: 5})
	}

	for i := int64(1); i <= 5; i++ {
		if !inCache(c, "proven", i) {
			t.Errorf("record %d was flushed out by the flood", i)
		}
	}
	if got, _, ok := asked(c, "proven", nil, &wire.Scope{Count: 5}); !ok || got != "1,2,3,4,5" {
		t.Errorf("the proven run now gives %s, held %v", got, ok)
	}
	sound(t, c)
}

// One answer larger than the cache does not evict everything else on its way in
// only to be cut down afterwards. It is cut down first.
func TestOneEnormousAnswerIsCutDownBeforeAnythingGivesWayForIt(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 20)
	filed(c, "proven", nil, nil, wad(1, 4))
	for i := 0; i < 2; i++ {
		asked(c, "proven", nil, &wire.Scope{Count: 4})
	}

	filed(c, "flood", nil, nil, wad(100, 500))

	for i := int64(1); i <= 4; i++ {
		if !inCache(c, "proven", i) {
			t.Errorf("record %d gave way to one enormous answer", i)
		}
	}
	// What was kept of the flood is its END -- the part a reader scrolling down
	// is next to -- and no more than its share.
	if !inCache(c, "flood", 500) || inCache(c, "flood", 100) {
		t.Error("it kept the wrong end of the flood")
	}
	if c.Cost() > c.Limit() {
		t.Errorf("it holds %d against a limit of %d", c.Cost(), c.Limit())
	}
}

// proven fills the cache with records that have all been read twice, spread
// over enough sequences that no one run is large enough to be taken for a
// flood.
func proven(c *cache, sets int, each int64) {
	for i := 0; i < sets; i++ {
		set := string(rune('a' + i))
		filed(c, set, nil, nil, wad(1, each))
		asked(c, set, nil, &wire.Scope{Count: int(each)})
		asked(c, set, nil, &wire.Scope{Count: int(each)})
	}
}

// A run that outgrows its share as it is scrolled loses the end the reader has
// left behind -- and that is decided by which way the reader is going, not by
// which end was read longest ago. Records that have just arrived have been read
// never, which would sort them coldest of all, and they are the very ones the
// reader is sitting on.
func TestAGrowingRunLosesTheEndTheReaderHasLeft(t *testing.T) {
	one := ent(1).cost

	down := newCache(one * 20) // so one run may hold ten
	filed(down, "files", nil, nil, wad(1, 6))
	asked(down, "files", nil, &wire.Scope{Count: 6})
	filed(down, "files", nil, wire.NewInt(6), wad(7, 12))
	if inCache(down, "files", 1) || !inCache(down, "files", 12) {
		t.Error("scrolling down lost the end it was scrolling towards")
	}
	sound(t, down)

	up := newCache(one * 20)
	up.hold("files", nil, &wire.Scope{After: wire.NewInt(100), Count: 6, Reversed: true},
		[]*entry{ent(99), ent(98), ent(97), ent(96), ent(95), ent(94)},
		wire.Complete{Stop: wire.StopFilled, Watermark: wire.NewInt(94)})
	asked(up, "files", nil, &wire.Scope{After: wire.NewInt(100), Count: 6, Reversed: true})
	up.hold("files", nil, &wire.Scope{After: wire.NewInt(94), Count: 6, Reversed: true},
		[]*entry{ent(93), ent(92), ent(91), ent(90), ent(89), ent(88)},
		wire.Complete{Stop: wire.StopFilled, Watermark: wire.NewInt(88)})
	if inCache(up, "files", 99) || !inCache(up, "files", 88) {
		t.Error("scrolling up lost the end it was scrolling towards")
	}
	sound(t, up)
}

// A run outgrowing its share stops at records that have proved themselves
// rather than eating through them. The share is a cap on what is UNPROVEN, so
// taking a proven record for it would not even bring the run under the cap.
func TestAGrowingRunDoesNotEatThroughWhatItHasProved(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 60) // so one run may hold thirty on probation

	filed(c, "files", nil, nil, wad(1, 10))
	for i := 0; i < 2; i++ {
		asked(c, "files", nil, &wire.Scope{Count: 10})
	}
	filed(c, "files", nil, wire.NewInt(10), wad(11, 25))
	filed(c, "files", nil, wire.NewInt(25), wad(26, 45))

	for i := int64(1); i <= 10; i++ {
		if !inCache(c, "files", i) {
			t.Errorf("record %d was eaten by the run it had proved itself in", i)
		}
	}
	sound(t, c)
}

// A refusal is an answer, and it is not a run: nothing is guaranteed between
// anything.
func TestARefusalIsNotFiled(t *testing.T) {
	c := newCache(1 << 20)
	c.hold("files", nil, &wire.Scope{Count: 3}, wad(1, 3),
		wire.Complete{Error: "the records are gone"})
	if c.Cost() != 0 || len(c.runs) != 0 {
		t.Errorf("a refusal left %d bytes in %d runs", c.Cost(), len(c.runs))
	}
}

// A cache too small to hold one record holds none, and does not keep a run of
// nothing to say so.
func TestACacheTooSmallForOneRecordHoldsNothing(t *testing.T) {
	c := newCache(1)
	filed(c, "files", nil, nil, wad(1, 5))
	if c.Cost() != 0 || len(c.runs) != 0 || len(c.sets) != 0 {
		t.Errorf("it holds %d in %d runs over %d sequences",
			c.Cost(), len(c.runs), len(c.sets))
	}
	sound(t, c)
}

// The protected segment cannot fill the cache: something has to be left on
// probation, or nothing new could ever prove itself.
func TestTheProtectedSegmentIsHeldUnderItsShare(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 20)
	proven(c, 4, 5)

	if c.warm == 0 {
		t.Fatal("nothing was protected at all")
	}
	if c.warm > c.limit*warmShare/warmOf {
		t.Errorf("%d of a %d cache is protected, past the share of %d",
			c.warm, c.limit, c.limit*warmShare/warmOf)
	}
}

// A cache with nothing left on probation still makes room, by cooling what has
// proved itself and taking it on the next pass. Without that it would wedge:
// full of protected records, unable to evict any of them, and unable to hold
// anything new.
func TestACacheOfNothingButProvenRecordsStillMakesRoom(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 20)
	proven(c, 4, 5)

	c.SetLimit(one * 4)
	if c.Cost() > one*4 {
		t.Errorf("it holds %d against a limit of %d", c.Cost(), one*4)
	}
	if c.Cost() == 0 {
		t.Error("it emptied itself rather than trimming")
	}
	sound(t, c)
}

// Eviction that takes a whole run leaves nothing behind it: no run of no
// records, no record in the lookup that no run holds, no cost still counted.
func TestEvictionLeavesNothingDangling(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 6)
	for i := 0; i < 6; i++ {
		filed(c, string(rune('a'+i)), nil, nil, wad(1, 4))
		asked(c, string(rune('a'+i)), nil, &wire.Scope{Count: 4})
		sound(t, c)
	}
	if len(c.runs) == 6 {
		t.Fatal("nothing was evicted, so nothing was proved")
	}
}

// Shrinking the cache evicts down to the new size at once.
func TestSettingTheLimitEvictsDownToIt(t *testing.T) {
	one := ent(1).cost
	c := newCache(one * 100)
	filed(c, "files", nil, nil, wad(1, 30))

	c.SetLimit(one * 5)
	if c.Cost() > one*5 {
		t.Errorf("after shrinking to %d it holds %d", one*5, c.Cost())
	}
	if c.Cost() == 0 {
		t.Error("it emptied itself rather than trimming")
	}
	sound(t, c)
}
