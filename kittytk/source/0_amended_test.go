package source

// Amending another source's records.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/wire"
)

// four records, by size: go.mod 96, build.sh 310, README.md 2048, parser.go 14022
func amendable(t *testing.T) *AmendedSource {
	t.Helper()
	return NewAmendedSource(mustPSL(t, twoWays))
}

func key(i int64) *wire.Value { return wire.NewInt(i) }

func fields(name string, size int64) wire.Fields {
	return wire.Fields{wire.Named(".name", name), wire.Named(".size", size)}
}

// A replacement goes out instead of the child's record, so the values are
// right wherever they are read.
func TestAReplacementStandsInForTheChildsRecord(t *testing.T) {
	a := amendable(t)
	a.Replace(key(2), fields("go.mod", 96))

	out, _ := read(t, a, "sort={ .size }", "count=4")
	if out.joined() != "2,1,0,3" {
		t.Fatalf("the sequence is %s", out.joined())
	}
	// What Replace stated, and only that: an amendment is the record entire,
	// so the bag is exactly what was handed in.
	if got := out.fields[0].Encode(); got != `{ .name "go.mod"; .size 96 }` {
		t.Errorf("the replacement carries %s", got)
	}
}

// And its own values place it, not the ones the child holds.
func TestAReplacementIsPlacedByItsOwnValues(t *testing.T) {
	a := amendable(t)
	a.Replace(key(2), fields("go.mod", 99999)) // was 96, the smallest

	out, _ := read(t, a, "sort={ .size }", "count=4")
	if out.joined() != "1,0,3,2" {
		t.Errorf("the sequence is %s", out.joined())
	}
}

// A deletion takes the record out, and the scope is still as long as it was
// asked for: the child was asked for enough to cover what would be removed.
func TestADeletionIsCoveredBeforeTheChildIsAsked(t *testing.T) {
	a := amendable(t)
	a.Delete(key(1), fields("build.sh", 310))

	out, done := read(t, a, "sort={ .size }", "count=3")
	if out.joined() != "2,0,3" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if done.Stop != wire.StopExhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// A deletion this source knows nothing about is discovered rather than
// predicted: the scope comes up short, the child is asked again, and what
// the round taught means the next one over the same ground does not.
func TestADeletionWithNoPlacementIsLearned(t *testing.T) {
	a := amendable(t)
	a.Delete(key(1), nil) // nothing known about where it sat

	out, _ := read(t, a, "sort={ .size }", "count=3")
	if out.joined() != "2,0,3" {
		t.Fatalf("the sequence is %s", out.joined())
	}

	// It knows now.
	am := a.lookup(key(1))
	if am == nil || am.seen == nil {
		t.Fatalf("nothing was learned: %#v", am)
	}
	if got := am.seen.Encode(); got != `{ key 1; .name "build.sh"; .size 310 }` {
		t.Errorf("what it learned is %s", got)
	}
}

// A replacement the filter no longer admits is a record gone, exactly as a
// deletion is, and it is covered the same way.
func TestAReplacementFilteredOutIsARecordGone(t *testing.T) {
	a := amendable(t)
	a.Replace(key(1), fields("build.sh", 99999)) // the filter below wants under 3000

	out, _ := read(t, a, "filter={ lt .size 3000 } sort={ .size }", "count=2")
	if out.joined() != "2,0" {
		t.Errorf("the sequence is %s", out.joined())
	}
}

// A record this source holds that the child never sends goes out anyway. Here
// the child cannot send it, because by the child's values it is filtered out.
func TestARecordTheChildNeverSendsStillGoesOut(t *testing.T) {
	a := amendable(t)
	a.Replace(key(3), fields("parser.go", 100)) // was 14022, the filter excludes it

	out, _ := read(t, a, "filter={ lt .size 3000 } sort={ .size }", "count=4")
	if out.joined() != "2,3,1,0" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if got := out.fields[1].Encode(); got != `{ .name "parser.go"; .size 100 }` {
		t.Errorf("the record this source supplied carries %s", got)
	}
}

// Amendments are not a query. They change whenever, and a scope is answered
// against what is held when it is asked.
func TestAmendmentsChangeBetweenScopes(t *testing.T) {
	a := amendable(t)
	set, err := a.Open(parseSpec(t, "sort={ .size }"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	first := &collector{}
	if err := set.Read(parseScope(t, "count=2"), first); err != nil {
		t.Fatal(err)
	}
	if first.joined() != "2,1" {
		t.Fatalf("the first scope is %s", first.joined())
	}

	a.Delete(key(1), fields("build.sh", 310))

	second := &collector{}
	if err := set.Read(parseScope(t, "count=2"), second); err != nil {
		t.Fatal(err)
	}
	if second.joined() != "2,0" {
		t.Errorf("the second scope is %s", second.joined())
	}
}

// Forgetting an amendment leaves the child's own record to stand.
func TestForgettingAnAmendmentLetsTheChildStand(t *testing.T) {
	a := amendable(t)
	a.Replace(key(2), fields("go.mod", 99999))
	a.Forget(key(2))

	out, _ := read(t, a, "sort={ .size }", "count=1")
	if out.joined() != "2" {
		t.Errorf("the first record is %s", out.joined())
	}
	if got := out.fields[0].Encode(); got != `{ key 2; .name "go.mod"; .size 96 }` {
		t.Errorf("the child's record carries %s", got)
	}
}

// It wraps any kind, including an application's records -- and including
// another amended source.
func TestItWrapsAnyKind(t *testing.T) {
	for _, kind := range []struct {
		what  string
		child Source
	}{
		{"records that are here", mustPSL(t, twoWays)},
		{"records an application has", serving(t)},
		{"another amended source", NewAmendedSource(mustPSL(t, twoWays))},
	} {
		t.Run(kind.what, func(t *testing.T) {
			a := NewAmendedSource(kind.child)
			a.Replace(key(2), fields("go.mod", 99999))
			a.Delete(key(1), fields("build.sh", 310))

			out, _ := read(t, a, "sort={ .size }", "count=3")
			if out.joined() != "0,3,2" {
				t.Errorf("the sequence is %s", out.joined())
			}
		})
	}
}

// What it claims about order is what is true of what it sent, which is the
// child's claim: ours went out in the sequence's order either way. And the
// claim is passed on before the records, which is what lets whoever is reading
// act on them as they arrive.
func TestAnOrderedChildMakesAnOrderedAnswer(t *testing.T) {
	a := amendable(t)
	a.Replace(key(2), fields("go.mod", 96))

	out, _ := read(t, a, "sort={ .size }", "count=4")
	if !out.ordered {
		t.Error("an ordered child did not make an ordered answer")
	}
	if out.joined() != "2,1,0,3" {
		t.Errorf("the sequence is %s", out.joined())
	}
}

func TestItClaimsOrderOnlyWhenTheChildDid(t *testing.T) {
	a := NewAmendedSource(&jumbled{inner: mustPSL(t, twoWays)})
	a.Replace(key(2), fields("go.mod", 96))

	out, _ := read(t, a, "sort={ .size }", "count=4")
	if out.ordered {
		t.Error("a jumble was claimed to be in order")
	}
	if len(out.keys) != 4 {
		t.Errorf("the records that came back are %v", out.keys)
	}
}

// jumbled is a source that answers correctly and says nothing about order,
// which every application is free to do.
type jumbled struct{ inner Source }

func (j *jumbled) Open(spec *wire.Spec) (DataSet, error) {
	set, err := j.inner.Open(spec)
	if err != nil {
		return nil, err
	}
	return &jumbledSet{inner: set}, nil
}

type jumbledSet struct{ inner DataSet }

func (j *jumbledSet) Close() { j.inner.Close() }
func (j *jumbledSet) Read(f *wire.Scope, out Sink) error {
	return j.inner.Read(f, &unordered{out: out})
}

type unordered struct{ out Sink }

func (u *unordered) Ordered()                                  {} // said nothing, which is what a jumble says
func (u *unordered) Record(k *wire.Value, f wire.Fields) error { return u.out.Record(k, f) }
func (u *unordered) Subset(k *wire.Value, f wire.Fields) error { return u.out.Subset(k, f) }
func (u *unordered) Done(c Complete)                           { u.out.Done(c) }

// A child that refuses ends the scope here too, rather than leaving whoever
// asked waiting.
func TestAChildThatRefusesEndsTheScope(t *testing.T) {
	a := NewAmendedSource(NewApplicationSource("files", func(string) error { return errBroken{} }))
	set, err := a.Open(parseSpec(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	out := &collector{}
	if err := set.Read(parseScope(t, "count=2"), out); err == nil {
		t.Fatal("a broken child was not reported")
	}
	if !out.ended || !strings.Contains(out.done.Error, "broken") {
		t.Errorf("the scope ended as %#v", out.done)
	}
}

// A deletion whose placement is stale is discovered, not predicted.
//
// The scope is asked for from a boundary, and this source believes the
// deleted record sits before it -- so it asks the child for no extra. The
// child sends the record anyway, it is dropped, and the scope is one short.
// So the child is asked again from where it got to, and the scope is filled.
func TestAStaleDeletionCostsASecondQuestion(t *testing.T) {
	a := amendable(t)
	// It really sits at 2048, between build.sh and parser.go. This source
	// believes it sits at 1, before the whole scope.
	a.Delete(key(0), fields("README.md", 1))

	seq := opened(t, a, "sort={ .size }")
	if head, _ := seq.scope("count=2"); head.joined() != "2,1" {
		t.Fatalf("the first scope is %s", head.joined())
	}
	out, _ := seq.scope("after=1 count=2")
	if out.joined() != "3" {
		t.Errorf("the scope is %s", out.joined())
	}

	// And what the round taught means the next one does not come up short.
	am := a.lookup(key(0))
	if am == nil || am.seen == nil {
		t.Fatalf("nothing was learned: %#v", am)
	}
	if got := am.seen.Encode(); got != `{ key 0; .name "README.md"; .size 2048 }` {
		t.Errorf("what it learned is %s", got)
	}
}

// The child having nothing more is the end of the sequence, this source's own
// records having all gone out by then.
func TestTheEndOfTheSequenceIsTheChildsEnd(t *testing.T) {
	a := amendable(t)
	a.Replace(key(9), fields("added-by-nobody", 99999))

	out, done := read(t, a, "sort={ .size }", "count=99")
	if done.Stop != wire.StopExhausted {
		t.Error("the end of the sequence did not say so")
	}
	if out.keys[len(out.keys)-1] != "9" {
		t.Errorf("the last record is %v", out.keys)
	}
}

// A child that always answers short does not get asked forever.
//
// Each round teaches something, so a second is nearly always enough. A child
// that never catches up is a child that is wrong, and the cap is what keeps
// being wrong from becoming being stuck.
func TestAChildThatNeverCatchesUpIsNotAskedForever(t *testing.T) {
	child := &dribble{}
	a := NewAmendedSource(child)

	out, done := read(t, a, "", "count=100")
	if child.rounds < 2 {
		t.Errorf("it gave up after %d round(s)", child.rounds)
	}
	if child.rounds > 8 {
		t.Errorf("it asked %d times", child.rounds)
	}
	if done.Stop == wire.StopExhausted {
		t.Error("a scope that never filled claimed to be exhausted")
	}
	if len(out.keys) != child.rounds {
		t.Errorf("%d records from %d rounds", len(out.keys), child.rounds)
	}
}

// dribble answers one record at a time and never says it has run out, which is
// a source that will never fill a scope however often it is asked.
type dribble struct{ rounds int }

func (d *dribble) Open(*wire.Spec) (DataSet, error) { return d, nil }
func (d *dribble) Close()                             {}

func (d *dribble) Read(f *wire.Scope, out Sink) error {
	d.rounds++
	out.Ordered()
	k := wire.NewInt(int64(d.rounds))
	_ = out.Record(k, wire.Fields{wire.Named(".n", int64(d.rounds))})
	out.Done(Complete{Stop: wire.StopFilled, Watermark: k})
	return nil
}

// Reversed reaches the amendments too: this source's own records are placed by
// the same levels the child's are, so the merge holds whichever way the
// sequence is read.
func TestAnAmendedSourceReverses(t *testing.T) {
	forward := amendable(t)
	forward.Replace(key(2), fields("go.mod", 96))
	up, _ := read(t, forward, "sort={ .size }", "count=4")
	if up.joined() != "2,1,0,3" {
		t.Fatalf("forward, the sequence is %s", up.joined())
	}

	mirror := amendable(t)
	mirror.Replace(key(2), fields("go.mod", 96))
	down, _ := read(t, mirror, "sort={ .size }", "count=4 reversed")
	if down.joined() != "3,0,1,2" {
		t.Errorf("reversed, the sequence is %s", down.joined())
	}
	if !down.ordered {
		t.Error("a reversed sequence did not say it was in order")
	}
}

// --- records of this source's own ----------------------------------------

// An addition is a record this source holds, not a statement about one of the
// child's. It goes out in its sorted place among them.
func TestAnAdditionGoesOutInItsPlace(t *testing.T) {
	a := amendable(t)
	a.Add(key(90), fields("added.go", 200))

	out, _ := read(t, a, "sort={ .size }", "count=9")
	if out.joined() != "2,90,1,0,3" {
		t.Errorf("the sequence is %s", out.joined())
	}
}

// The count is the count, whoever the records came from.
//
// Ours are not extra on top of the scope: a scope of three is three records,
// and what is left over is the start of the next one rather than surplus
// stapled to this one. Without that, a source holding a thousand additions
// would answer every scope with the thousand that sort ahead of it.
func TestAdditionsCountTowardsTheScope(t *testing.T) {
	a := amendable(t)
	for i := 90; i < 95; i++ {
		a.Add(key(int64(i)), fields("added", int64(i)))
	}

	out, done := read(t, a, "sort={ .size }", "count=3")
	if len(out.keys) != 3 {
		t.Errorf("a scope of three came back %d long: %s", len(out.keys), out.joined())
	}
	if done.Stop != wire.StopFilled {
		t.Errorf("a scope that filled said %q", done.Stop)
	}
}

// A scope can end on a record of this source's own, and the next one carries
// on from it.
//
// The child has never heard of that identity, so it is not the one handed
// down: what goes down is the last identity the child itself gave before ours
// crossed, which is the same place in the merged sequence.
func TestAScopeResumesFromARecordOfOurOwn(t *testing.T) {
	a := amendable(t)
	for i := 90; i < 95; i++ {
		a.Add(key(int64(i)), fields("added", int64(i)))
	}
	seq := opened(t, a, "sort={ .size }")

	first, done := seq.scope("count=3")
	if first.joined() != "90,91,92" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	if done.Watermark == nil {
		t.Fatal("a scope that filled claimed nothing")
	}

	next, done := seq.scope("after=" + wire.EncodeValue(done.Watermark) + " count=9")
	if next.joined() != "93,94,2,1,0,3" {
		t.Errorf("the rest is %s", next.joined())
	}
	if done.Stop != wire.StopExhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// Where an addition's key turns out to be the child's after all, the child's
// record is the one that stands -- the opposite way round from a replacement.
//
// Nothing is asked to find that out. It surfaces when the child's copy
// arrives, and this source writes it down: the scope it surfaced on keeps
// ours, because ours had already crossed and one identity twice is worse than
// either winning, and every scope after it has the child's.
func TestAClashingAdditionLosesToTheChild(t *testing.T) {
	a := amendable(t)
	// Key 1 is the child's build.sh at size 310. Ours sorts ahead of it.
	a.Add(key(1), fields("mine.go", 5))
	seq := opened(t, a, "sort={ .size }")

	first, _ := seq.scope("count=9")
	if got := first.fields[0].Encode(); got != `{ .name "mine.go"; .size 5 }` {
		t.Errorf("the scope it surfaced on carries %s first", got)
	}
	if n := strings.Count(first.joined(), "1"); n != 1 {
		t.Errorf("one identity crossed twice: %s", first.joined())
	}

	// And from here on the child's record is the one that stands.
	next, _ := seq.scope("count=9")
	if next.joined() != "2,1,0,3" {
		t.Errorf("the scope after the clash is %s", next.joined())
	}
	if got := next.fields[1].Encode(); got != `{ key 1; .name "build.sh"; .size 310 }` {
		t.Errorf("the child's record came back as %s", got)
	}
}

// An addition does not make the child be asked for more, the way a deletion
// does: it fills a place in the scope that the child then need not.
func TestAnAdditionAsksTheChildForLess(t *testing.T) {
	s := &spy{inner: mustPSL(t, twoWays)}
	a := NewAmendedSource(s)
	a.Add(key(90), fields("added", 5))

	read(t, a, "sort={ .size }", "count=3")
	if s.asked.Count != 2 {
		t.Errorf("the child was asked for %d, want 2 of the 3", s.asked.Count)
	}
}

// The other way round: the child's copy arrives first, so the child's goes out
// and ours is taken out of what is still to come.
//
// Without that, ours would follow its own copy a moment later and the same
// identity would cross twice in one scope -- which is the thing the rule is
// there to stop, whichever of the two happens to sort first.
func TestAClashingAdditionIsDroppedWhenTheChildsCameFirst(t *testing.T) {
	a := amendable(t)
	// Key 1 is the child's build.sh at size 310. Ours sorts after it.
	a.Add(key(1), fields("mine.go", 9999))

	out, _ := read(t, a, "sort={ .size }", "count=9")
	if out.joined() != "2,1,0,3" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if got := out.fields[1].Encode(); got != `{ key 1; .name "build.sh"; .size 310 }` {
		t.Errorf("the child's record came back as %s", got)
	}
}

// A scope that filled with records of ours still to come has not reached the
// end, however exhausted the child is.
//
// The child having nothing more says nothing about this source: what is left
// over is ours, and it is the start of the next scope rather than nothing.
func TestOursLeftOverIsNotTheEndOfTheSequence(t *testing.T) {
	a := amendable(t)
	for i := 90; i < 95; i++ {
		a.Add(key(int64(i)), fields("added", int64(90000+i)))
	}
	seq := opened(t, a, "sort={ .size }")

	out, done := seq.scope("count=6")
	if len(out.keys) != 6 {
		t.Fatalf("a scope of six came back %d long: %s", len(out.keys), out.joined())
	}
	if done.Stop != wire.StopFilled {
		t.Errorf("a scope with three of ours still to come said %q", done.Stop)
	}

	next, done := seq.scope("after=" + wire.EncodeValue(done.Watermark) + " count=9")
	if next.joined() != "92,93,94" {
		t.Errorf("the rest is %s", next.joined())
	}
	if done.Stop != wire.StopExhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// A walk that reached the record the asker already held stops there, and this
// source's own records do not run past it.
//
// `until` says the asker holds that record and everything beyond it -- ours
// included, since ours crossed the same way when it got them. So nothing left
// of ours goes out, nothing is asked of the child again, and the claim stops
// where the walk did rather than at some record of ours past it.
func TestOursDoNotRunPastAJoinedWalk(t *testing.T) {
	a := amendable(t)
	a.Add(key(90), fields("added", 99999)) // past everything

	out, done := read(t, a, "sort={ .size }", "until=0 count=9")
	if out.joined() != "2,1" {
		t.Errorf("the walk to the record the asker held is %s", out.joined())
	}
	if done.Stop != wire.StopJoined {
		t.Errorf("it stopped at that record and said %q", done.Stop)
	}
	if got := wire.EncodeValue(done.Watermark); got != "1" {
		t.Errorf("the claim reaches %s, which is past where the walk stopped", got)
	}
}

// A walk that joined is over, and nothing is asked again on the strength of it
// being shorter than the count.
//
// The count is not short -- the records past `until` are ones the asker
// already has -- so going back to the child for more would ask it to walk
// ground the asker told it not to, four times over before giving up.
func TestAJoinedWalkAsksTheChildNothingMore(t *testing.T) {
	s := &spy{inner: mustPSL(t, twoWays)}
	a := NewAmendedSource(s)
	a.Add(key(90), fields("added", 99999))

	read(t, a, "sort={ .size }", "until=0 count=9")
	if s.reads != 1 {
		t.Errorf("the child was asked %d times for a walk that had joined", s.reads)
	}
}

// Joined outranks filled where a walk did both.
//
// Reaching the record the asker already held says its two runs are now one,
// which is the fact worth having; that the count also happened to come out
// even says nothing.
func TestAWalkThatJoinedAndFilledSaysItJoined(t *testing.T) {
	a := amendable(t)
	out, done := read(t, a, "sort={ .size }", "until=0 count=2")
	if out.joined() != "2,1" {
		t.Fatalf("the walk is %s", out.joined())
	}
	if done.Stop != wire.StopJoined {
		t.Errorf("a walk that reached the asker's own record said %q", done.Stop)
	}
}

// The child is never asked for a negative number of records.
//
// More additions than the scope is long means this source fills it alone and
// wants nothing from the child -- which is nought, not minus two. A source
// whose records are here shrugs that off; one across the wire would have the
// whole query refused, `count` being a number of records and there being no
// such thing as fewer than none of them.
func TestTheChildIsNeverAskedForFewerThanNone(t *testing.T) {
	s := &spy{inner: mustPSL(t, twoWays)}
	a := NewAmendedSource(s)
	for i := 90; i < 95; i++ {
		a.Add(key(int64(i)), fields("added", int64(i)))
	}

	read(t, a, "sort={ .size }", "count=3")
	if s.asked.Count < 0 {
		t.Errorf("the child was asked for %d records", s.asked.Count)
	}
}

// An addition the filter does not admit takes nothing out of the child's
// answer, so it buys the child no extra to send.
//
// Slack is for what this source REMOVES -- a deletion, or a replacement whose
// new values no longer match and so loses the child's record too. An addition
// that does not match was never in the sequence to begin with and removes
// nothing.
func TestAnAdditionTheFilterDropsIsNotSlack(t *testing.T) {
	s := &spy{inner: mustPSL(t, twoWays)}
	a := NewAmendedSource(s)
	a.Add(key(90), fields("added", 99999)) // outside the filter below

	read(t, a, "sort={ .size } filter={ lt .size 1000 }", "count=2")
	if s.asked.Count != 2 {
		t.Errorf("the child was asked for %d, want the 2 the scope wanted", s.asked.Count)
	}
}

// --- the order the amendments are arranged in ----------------------------

// The arrangement is made once for a sequence and every scope of it is a search
// into the result, so two scopes of one sequence arrange nothing twice.
func TestTheAmendmentsAreArrangedOncePerSequence(t *testing.T) {
	a := amendable(t)
	a.Add(key(90), fields("added", 5))
	seq := opened(t, a, "sort={ .size }")

	seq.scope("count=2")
	built := a.order(parseSpec(t, "sort={ .size }"))
	seq.scope("count=2")
	again := a.order(parseSpec(t, "sort={ .size }"))

	if built != again {
		t.Error("a second scope of one sequence arranged the amendments again")
	}
}

// Ordering can be a hint; membership cannot.
//
// An amendment made between two scopes is not in an arrangement built before
// it, and nothing about walking that arrangement would ever find it -- so what
// is held carries a generation, and a source amended since arranges it again.
func TestAnAmendmentMadeBetweenScopesIsSeen(t *testing.T) {
	a := amendable(t)
	seq := opened(t, a, "sort={ .size }")

	if first, _ := seq.scope("count=9"); first.joined() != "2,1,0,3" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	a.Add(key(90), fields("added", 5))

	next, _ := seq.scope("count=9")
	if next.joined() != "90,2,1,0,3" {
		t.Errorf("the scope after the addition is %s", next.joined())
	}
}

// An addition taken back between two scopes stops being in it.
//
// Nothing but the arrangement knows an addition exists -- no record of the
// child's will arrive to prompt a second look at it -- so it goes out for as
// long as the arrangement says it is there. Nothing else in this test amends
// anything, because an amendment of any other kind would rearrange what is
// held and hide that.
func TestAnAdditionForgottenBetweenScopesIsGone(t *testing.T) {
	a := amendable(t)
	a.Add(key(90), fields("added", 5))
	seq := opened(t, a, "sort={ .size }")

	if first, _ := seq.scope("count=9"); first.joined() != "90,2,1,0,3" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	a.Forget(key(90))

	next, _ := seq.scope("count=9")
	if next.joined() != "2,1,0,3" {
		t.Errorf("the scope after forgetting it is %s", next.joined())
	}
}

// A forgotten deletion comes right whether or not anything is rearranged: a
// record of the child's is looked up in what is held as it arrives, so the
// child's own copy stands the moment nothing is held against it.
func TestADeletionForgottenBetweenScopesIsGone(t *testing.T) {
	a := amendable(t)
	a.Delete(key(1), fields("build.sh", 310))
	seq := opened(t, a, "sort={ .size }")

	if first, _ := seq.scope("count=9"); first.joined() != "2,0,3" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	a.Forget(key(1))

	next, _ := seq.scope("count=9")
	if next.joined() != "2,1,0,3" {
		t.Errorf("the scope after forgetting it is %s", next.joined())
	}
}


// A deletion whose placement was learned stops being counted against every
// scope and takes its place in the order, which is a change to what is held
// like any other.
func TestALearnedPlacementRearrangesWhatIsHeld(t *testing.T) {
	a := amendable(t)
	a.Delete(key(0), nil) // README.md, 2048, placement unknown
	seq := opened(t, a, "sort={ .size }")

	before := a.order(parseSpec(t, "sort={ .size }"))
	if before.unplaced != 1 {
		t.Fatalf("a deletion with no placement was counted %d times", before.unplaced)
	}
	seq.scope("count=9") // the child sends it, and the round teaches where it sat

	after := a.order(parseSpec(t, "sort={ .size }"))
	if after.unplaced != 0 {
		t.Errorf("the placement was learned and it is still counted for every scope")
	}
	if len(after.gone) != 1 {
		t.Errorf("it did not take its place in the order: %d removals", len(after.gone))
	}
}

// Two sequences over one source are arranged separately: a different sort puts
// the same amendments in a different order.
func TestEachSequenceArrangesTheAmendmentsItsOwnWay(t *testing.T) {
	a := amendable(t)
	a.Add(key(90), fields("aaa.go", 99999))

	up, _ := read(t, a, "sort={ .size }", "count=9")
	if up.joined() != "2,1,0,3,90" {
		t.Errorf("by size the sequence is %s", up.joined())
	}
	// Exact collation, so README.md leads: an uppercase R sorts below a lowercase a.
	byName, _ := read(t, a, "sort={ .name }", "count=9")
	if byName.joined() != "0,90,1,2,3" {
		t.Errorf("by name the sequence is %s", byName.joined())
	}
}

// A reversed scope resumes after one of ours without sending it again.
//
// The order is arranged the way the sequence runs and read backwards, so the
// boundary record sits somewhere in it and the walk has to stop just short of
// it rather than just past it -- the mirror of what a forward walk does, and
// the one place the two directions cannot share a search.
func TestAReversedScopeResumesAfterOneOfOurs(t *testing.T) {
	a := amendable(t)
	a.Add(key(90), fields("added", 99998))
	a.Add(key(91), fields("added", 99999))
	seq := opened(t, a, "sort={ .size }")

	first, done := seq.scope("count=2 reversed")
	if first.joined() != "91,90" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	if got := wire.EncodeValue(done.Watermark); got != "90" {
		t.Fatalf("the claim reaches %s", got)
	}

	next, done := seq.scope("after=90 count=9 reversed")
	if next.joined() != "3,0,1,2" {
		t.Errorf("the rest is %s", next.joined())
	}
	if done.Stop != wire.StopExhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// A deletion nobody has seen the record of is covered for every scope.
//
// Not knowing where it sat is not knowing which scope it falls in, so the child
// is asked for one more than the scope wants. Getting that wrong is not visibly
// wrong -- the scope comes up short and the round trip that fixes it hides the
// cost -- so it is the ask that is checked here, not the answer.
func TestAnUnplacedDeletionIsCoveredInTheAsk(t *testing.T) {
	s := &spy{inner: mustPSL(t, twoWays)}
	a := NewAmendedSource(s)
	a.Delete(key(0), nil)

	read(t, a, "sort={ .size }", "count=3")
	if s.asked.Count != 4 {
		t.Errorf("the child was asked for %d, want the 3 plus the one that may go", s.asked.Count)
	}
}

// Only the removals past the boundary are covered, and finding them is a search
// -- which is a search of a run that has to be in order for it to land.
func TestOnlyTheRemovalsPastTheBoundaryAreCovered(t *testing.T) {
	s := &spy{inner: mustPSL(t, twoWays)}
	a := NewAmendedSource(s)
	// go.mod at 96 sits before the boundary; README.md at 2048 sits after it.
	a.Delete(key(2), fields("go.mod", 96))
	a.Delete(key(0), fields("README.md", 2048))
	seq := opened(t, a, "sort={ .size }")

	// Walk as far as build.sh (310), then carry on past it.
	if first, _ := seq.scope("count=1"); first.joined() != "1" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	seq.scope("after=1 count=2")
	if s.asked.Count != 3 {
		t.Errorf("the child was asked for %d, want the 2 plus the one removal past "+
			"the boundary", s.asked.Count)
	}
}

// A replacement made between two scopes goes out in the second.
//
// This is the one that would lose a record rather than merely misjudge one: the
// child's copy is dropped the moment anything is held against its key, so a
// replacement the arrangement has not caught up with takes the child's record
// out and puts nothing back.
func TestAReplacementMadeBetweenScopesIsSeen(t *testing.T) {
	a := amendable(t)
	seq := opened(t, a, "sort={ .size }")

	if first, _ := seq.scope("count=9"); first.joined() != "2,1,0,3" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	a.Replace(key(2), fields("go.mod", 99999)) // was the smallest; now the largest

	next, _ := seq.scope("count=9")
	if next.joined() != "1,0,3,2" {
		t.Errorf("the scope after the replacement is %s", next.joined())
	}
}

// A deletion made between two scopes is covered in the ask for the second.
//
// It would come right either way -- a record of the child's is looked up in
// what is held as it arrives, so the child's copy is dropped whether or not
// anything was rearranged -- but the scope would come up short and cost a round
// trip to fill. So it is the ask that is checked, not the answer.
func TestADeletionMadeBetweenScopesIsCoveredInTheAsk(t *testing.T) {
	s := &spy{inner: mustPSL(t, twoWays)}
	a := NewAmendedSource(s)
	seq := opened(t, a, "sort={ .size }")

	if first, _ := seq.scope("count=1"); first.joined() != "2" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	a.Delete(key(0), fields("README.md", 2048)) // past the boundary

	seq.scope("after=2 count=2")
	if s.asked.Count != 3 {
		t.Errorf("the child was asked for %d, want the 2 plus the one that goes",
			s.asked.Count)
	}
}

// Both runs of the arrangement are in the sequence's order.
//
// A scope finds its place in each by searching, and a search of a run that is
// not in order lands somewhere arbitrary. What goes out would still come out
// right -- that run is walked, not searched past -- but the removals are only
// ever counted by searching, so an unsorted one miscounts the shortfall.
//
// Asserted on the arrangement rather than through an answer, because what is
// held arrives from a map: an unsorted run comes out sorted often enough by
// chance that an answer would pass half the time.
func TestBothRunsOfTheArrangementAreInOrder(t *testing.T) {
	a := amendable(t)
	for i, size := range []int64{700, 100, 900, 300, 500} {
		a.Add(key(int64(90+i)), fields("added", size))
	}
	for i, size := range []int64{800, 200, 1000, 400, 600} {
		a.Delete(key(int64(80+i)), fields("gone", size))
	}

	o := a.order(parseSpec(t, "sort={ .size }"))
	levels := ordering1(parseSpec(t, "sort={ .size }"))
	for _, run := range []struct {
		what string
		at   [][]*wire.Value
	}{{"what goes out", o.outAt}, {"what is taken out", o.goneAt}} {
		if len(run.at) != 5 {
			t.Fatalf("%s holds %d, want 5", run.what, len(run.at))
		}
		for i := 1; i < len(run.at); i++ {
			if wire.CompareLevels(run.at[i-1], run.at[i], levels) >= 0 {
				t.Errorf("%s is out of order at %d", run.what, i)
			}
		}
	}
}
