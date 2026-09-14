package source

// Answering out of several sources at once.

import (
	"strings"
	"sync"
	"testing"

	"github.com/phroun/kittytk/wire"
)

// Two documents, each with items and a keyed member, so both of a PSL list's
// collections take part in the merge.
const leftDoc = `(
  (name: "alpha", size: 30),
  (name: "gamma", size: 50),
  note: (name: "left note", size: 5)
)`

const rightDoc = `(
  (name: "beta", size: 40),
  (name: "delta", size: 60)
)`

func composed(t *testing.T, in ...Include) *ComposedSource {
	t.Helper()
	c, err := NewComposedSource(in...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func twoIncludes(t *testing.T) *ComposedSource {
	t.Helper()
	return composed(t,
		Include{Name: "left", Source: mustPSL(t, leftDoc)},
		Include{Name: "right", Source: mustPSL(t, rightDoc)})
}

// Every include's records are in the outer sequence, each under a key of its
// own: the include's name, a slash, and the child's key.
func TestEveryIncludesRecordsAreInTheSequence(t *testing.T) {
	out, done := read(t, twoIncludes(t), "", "count=10")
	if out.joined() != "(left/0),(left/1),(left/note),(right/0),(right/1)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if done.Stop != wire.StopExhausted {
		t.Error("a scope holding every record did not say so")
	}
}

// Nothing shadows anything. Two includes keyed the same way both keep their
// records, because the name in front of the key is what tells them apart.
func TestTwoIncludesKeyedAlikeDoNotCollide(t *testing.T) {
	c := composed(t,
		Include{Name: "one", Source: mustPSL(t, rightDoc)},
		Include{Name: "two", Source: mustPSL(t, rightDoc)})

	out, _ := read(t, c, "", "count=10")
	if out.joined() != "(one/0),(one/1),(two/0),(two/1)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if len(out.keys) != 4 {
		t.Errorf("%d records for two includes of two", len(out.keys))
	}
}

// The query's own sort comes first, and the includes interleave under it.
func TestTheIncludesInterleaveUnderTheSort(t *testing.T) {
	out, _ := read(t, twoIncludes(t), "sort={ .size }", "count=10")
	if out.joined() != "(left/note),(left/0),(right/0),(left/1),(right/1)" {
		t.Errorf("by size the sequence is %s", out.joined())
	}
	if got := out.fields[2].Encode(); got != `{ key 0; .name "beta"; .size 40 }` {
		t.Errorf("the third record carries %s", got)
	}
}

// An include's own order is kept, which is what the merge stands on: the
// include's name is the same for all of its records, so what orders them is
// their own key, exactly as the include ordered them.
//
// Ten items make the point a text comparison would get wrong -- 10 belongs
// after 9, not between 1 and 2.
func TestAnIncludeKeepsItsOwnOrder(t *testing.T) {
	var b strings.Builder
	b.WriteString("(")
	for i := 0; i < 11; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(n: 1)")
	}
	b.WriteString(")")

	c := composed(t, Include{Name: "many", Source: mustPSL(t, b.String())})
	out, _ := read(t, c, "", "count=20")
	want := "(many/0),(many/1),(many/2),(many/3),(many/4),(many/5)," +
		"(many/6),(many/7),(many/8),(many/9),(many/10)"
	if out.joined() != want {
		t.Errorf("the sequence is %s", out.joined())
	}
}

// A scope is as long as it was asked for, and the next one starts where it
// stopped.
func TestAScopeOfAComposedSourceCarriesOn(t *testing.T) {
	c := twoIncludes(t)
	seq := opened(t, c, "sort={ .size }")
	first, done := seq.scope("count=2")
	if first.joined() != "(left/note),(left/0)" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	if done.Stop == wire.StopExhausted {
		t.Error("a scope with records past it claimed to be exhausted")
	}

	next, done := seq.scope("after=" + wire.EncodeValue(done.Watermark) + " count=10")
	if next.joined() != "(right/0),(left/1),(right/1)" {
		t.Errorf("the rest is %s", next.joined())
	}
	if done.Stop != wire.StopExhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// Resuming after a record of one include carries on in all of them.
//
// None of them is shown the outer identity, which would mean nothing to it.
// Each is resumed from the last record it gave -- its own high watermark -- and
// that is the same place in the merged sequence.
func TestResumingInOneIncludeCarriesOnInTheOthers(t *testing.T) {
	c := twoIncludes(t)
	seq := opened(t, c, "")
	first, _ := seq.scope("count=1")
	if first.joined() != "(left/0)" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	out, _ := seq.scope("after=(left/0) count=10")
	if out.joined() != "(left/1),(left/note),(right/0),(right/1)" {
		t.Errorf("the scope after left/0 is %s", out.joined())
	}
}

// Order is claimed only when every include promised it, and it is claimed
// before the records, which is the only place it is worth anything.
func TestOrderIsClaimedOnlyWhenEveryIncludePromisedIt(t *testing.T) {
	ordered, _ := read(t, twoIncludes(t), "sort={ .size }", "count=10")
	if !ordered.ordered {
		t.Error("includes that were all in order made an answer that was not")
	}

	c := composed(t,
		Include{Name: "left", Source: mustPSL(t, leftDoc)},
		Include{Name: "right", Source: &jumbled{inner: mustPSL(t, rightDoc)}})
	mixed, _ := read(t, c, "sort={ .size }", "count=10")
	if mixed.ordered {
		t.Error("one include saying nothing about order still made an ordered answer")
	}
	// Every record is still there. What is given up is the order, not the data.
	if len(mixed.keys) != 5 {
		t.Errorf("the records that came back are %v", mixed.keys)
	}
}

// The watermark is the lowest of the includes', not the highest.
//
// Complete up to a point means EVERY include is complete up to it, so the one
// that swept least far holds the claim back for all of them. Both stop after a
// record here, at different places, and what goes out is the nearer of the two
// -- the further one would claim records the far end has not been sent.
func TestTheWatermarkIsTheLowestOfTheIncludes(t *testing.T) {
	c := composed(t,
		Include{Name: "left", Source: &short{inner: mustPSL(t, leftDoc)}},
		Include{Name: "right", Source: &short{inner: mustPSL(t, rightDoc)}})

	out, done := read(t, c, "sort={ .size }", "count=10")
	if out.joined() != "(left/note),(right/0)" {
		t.Fatalf("the scope is %s", out.joined())
	}
	if done.Stop == wire.StopExhausted {
		t.Fatal("includes that had more claimed the sequence was over")
	}
	// left swept to size 5 and right to size 40. Only 5 is true of both.
	if got := wire.EncodeValue(done.Watermark); got != `(left/note)` {
		t.Errorf("the watermark is %s", got)
	}
}

// An include that sends more than it was asked from is answered here, not
// passed on.
//
// Over-serving is always allowed -- an include may ignore the boundary and
// send everything it has. What it may never do is leave records out of a range
// this source then claims, so the boundary is applied again on the way through
// rather than trusted to the include.
func TestRecordsTheBoundaryAlreadyCoveredAreDropped(t *testing.T) {
	c := composed(t,
		Include{Name: "all", Source: &ignoresBoundaries{inner: mustPSL(t, leftDoc)}},
		Include{Name: "other", Source: mustPSL(t, rightDoc)})

	seq := opened(t, c, "")
	if first, _ := seq.scope("count=1"); first.joined() != "(all/0)" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	out, _ := seq.scope("after=(all/0) count=10")
	if out.joined() != "(all/1),(all/note),(other/0),(other/1)" {
		t.Errorf("the scope after all/0 is %s", out.joined())
	}
}

// ignoresBoundaries is a source that sends every record it has whatever it was
// asked from, which is the least an implementation can do and is legal.
type ignoresBoundaries struct{ inner Source }

func (e *ignoresBoundaries) Open(spec *wire.Spec) (DataSet, error) {
	set, err := e.inner.Open(spec)
	if err != nil {
		return nil, err
	}
	return &everythingSet{inner: set}, nil
}

type everythingSet struct{ inner DataSet }

func (e *everythingSet) Close() { e.inner.Close() }

func (e *everythingSet) Read(f *wire.Scope, out Sink) error {
	whole := *f
	whole.After, whole.Until = nil, nil
	whole.Count = 1 << 20
	return e.inner.Read(&whole, out)
}

// A composed source inside a composed source resumes from a boundary of its
// own making, because the name is split off the front and what is left is the
// inner source's key, slashes and all.
func TestANestedComposedSourceResumes(t *testing.T) {
	nested := func() Source {
		inner := composed(t, Include{Name: "deep", Source: mustPSL(t, rightDoc)})
		return composed(t,
			Include{Name: "flat", Source: mustPSL(t, leftDoc)},
			Include{Name: "nested", Source: inner})
	}

	seq := opened(t, nested(), "")
	first, done := seq.scope("count=4")
	if first.joined() != "(flat/0),(flat/1),(flat/note),(nested/deep/0)" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	if done.Stop == wire.StopExhausted {
		t.Fatal("a scope with a record past it claimed to be exhausted")
	}

	next, done := seq.scope("after=" + wire.EncodeValue(done.Watermark) + " count=10")
	if next.joined() != "(nested/deep/1)" {
		t.Errorf("the rest is %s", next.joined())
	}
	if done.Stop != wire.StopExhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// A sequence is the source, the sort and the filter -- not the reading of it.
//
// A reader may let one data set go and open another over the same three
// between two scopes, and the second is the same sequence: the record the first
// handed out is still placed, and the scope after it carries on where the other
// stopped. That is why what places a record is kept beside the ordering it was
// placed in rather than on whoever happened to be reading.
func TestASequenceResumesThroughADifferentReading(t *testing.T) {
	c := twoIncludes(t)

	first, done := opened(t, c, "sort={ .size }").scope("count=3")
	if first.joined() != "(left/note),(left/0),(right/0)" {
		t.Fatalf("the first scope is %s", first.joined())
	}

	// A second data set over the same sequence, the first one let go.
	next, done := opened(t, c, "sort={ .size }").scope(
		"after=" + wire.EncodeValue(done.Watermark) + " count=9")
	if next.joined() != "(left/1),(right/1)" {
		t.Errorf("the rest is %s", next.joined())
	}
	if done.Stop != wire.StopExhausted {
		t.Error("the end of the sequence did not say so")
	}

	// A different sequence is a different one, and has never placed it.
	other, done := opened(t, c, "sort={ .name }").scope(
		"after=" + wire.EncodeValue(first.done.Watermark) + " count=9")
	if done.Error == "" {
		t.Errorf("another sequence resumed from it anyway: %s", other.joined())
	}
}

// A scope that filled before it reached the end has not reached the end, even
// where every include ran out of its own.
//
// The includes here are both exhausted -- three records and two, and each was
// asked for three. The merge stops at the three the scope wanted, so two are
// still held: they are the start of the next scope, not records that are gone.
// So the answer says where it got to instead of saying there is nothing past
// it, and it says the position of the last record that actually went out
// rather than how far the includes had swept.
func TestAScopeThatFilledIsNotTheEndOfTheSequence(t *testing.T) {
	seq := opened(t, twoIncludes(t), "sort={ .size }")
	out, done := seq.scope("count=3")
	if out.joined() != "(left/note),(left/0),(right/0)" {
		t.Fatalf("the scope is %s", out.joined())
	}
	if done.Stop == wire.StopExhausted {
		t.Error("a scope with two records still to come said there were none")
	}
	if got := wire.EncodeValue(done.Watermark); got != "(right/0)" {
		t.Errorf("the watermark is %s", got)
	}

	// And the next scope picks up exactly the two that were held.
	next, done := seq.scope("after=" + wire.EncodeValue(done.Watermark) + " count=9")
	if next.joined() != "(left/1),(right/1)" {
		t.Errorf("the rest is %s", next.joined())
	}
	if done.Stop != wire.StopExhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// short is a source that answers one record and stops, saying how far it got,
// which every application is free to do.
type short struct{ inner Source }

func (s *short) Open(spec *wire.Spec) (DataSet, error) {
	set, err := s.inner.Open(spec)
	if err != nil {
		return nil, err
	}
	return &shortSet{inner: set, sort: spec.Sort}, nil
}

type shortSet struct {
	inner DataSet
	sort  []wire.SortLevel
}

func (s *shortSet) Close() { s.inner.Close() }

func (s *shortSet) Read(f *wire.Scope, out Sink) error {
	return s.inner.Read(f, &stopAfterOne{out: out, sort: s.sort})
}

type stopAfterOne struct {
	out    Sink
	sort   []wire.SortLevel
	sent   int
	key    *wire.Value
	fields wire.Fields
}

func (s *stopAfterOne) Ordered() { s.out.Ordered() }

func (s *stopAfterOne) Record(k *wire.Value, f wire.Fields) error {
	return s.keep(k, f, true)
}

func (s *stopAfterOne) Subset(k *wire.Value, f wire.Fields) error {
	return s.keep(k, f, false)
}

func (s *stopAfterOne) keep(k *wire.Value, f wire.Fields, whole bool) error {
	if s.sent > 0 {
		return nil
	}
	s.sent++
	s.key, s.fields = k, f
	if whole {
		return s.out.Record(k, f)
	}
	return s.out.Subset(k, f)
}

// Done says where it got to: the one record it sent, which is the only one it
// is in a position to claim.
func (s *stopAfterOne) Done(Complete) {
	s.out.Done(Complete{Stop: wire.StopFilled, Watermark: s.key})
}

// An include that refuses ends the scope here too, rather than leaving whoever
// asked waiting on records that are never coming.
func TestAnIncludeThatRefusesEndsTheScope(t *testing.T) {
	c := composed(t,
		Include{Name: "good", Source: mustPSL(t, leftDoc)},
		Include{Name: "bad", Source: NewApplicationSource("files",
			func(string) error { return errBroken{} })})

	set, err := c.Open(parseSpec(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	out := &collector{}
	if err := set.Read(parseScope(t, "count=10"), out); err != nil {
		t.Fatal(err)
	}
	if !out.ended || !strings.Contains(out.done.Error, "broken") {
		t.Errorf("the scope ended as %#v", out.done)
	}
}

// It composes any kind, including an application's records, another composed
// source, and an amended one.
func TestItComposesAnyKind(t *testing.T) {
	inner := composed(t, Include{Name: "deep", Source: mustPSL(t, rightDoc)})
	amended := NewAmendedSource(mustPSL(t, rightDoc))
	amended.Replace(wire.NewInt(0), wire.Fields{
		wire.Named(".name", "replaced"), wire.Named(".size", 40)})

	c := composed(t,
		Include{Name: "here", Source: mustPSL(t, leftDoc)},
		Include{Name: "app", Source: serving(t)},
		Include{Name: "nested", Source: inner},
		Include{Name: "amended", Source: amended})

	out, done := read(t, c, "", "count=30")
	if done.Stop != wire.StopExhausted {
		t.Error("the end of the sequence did not say so")
	}
	// The nested source's own composed keys arrive whole and are prefixed
	// again, which is what makes the two levels of include one address.
	if !strings.Contains(out.joined(), "(nested/deep/0),(nested/deep/1)") {
		t.Errorf("the nested include reads %s", out.joined())
	}
	if !strings.Contains(out.joined(), "(app/0),(app/1),(app/2),(app/3)") {
		t.Errorf("the application's records read %s", out.joined())
	}
	for i, k := range out.keys {
		if k == "(amended/0)" && out.fields[i].Get(".name").Str != "replaced" {
			t.Errorf("the amendment did not reach the composed answer: %s",
				out.fields[i].Encode())
		}
	}
}

// A record that came back whole goes out whole, and one that came back as a
// subset goes out as a subset: this source relays the claim and makes none of
// its own.
func TestAnIncludesClaimIsPassedThrough(t *testing.T) {
	c := composed(t,
		Include{Name: "whole", Source: mustPSL(t, rightDoc)},
		Include{Name: "part", Source: hosting(t, serveSubsets)})

	out, _ := read(t, c, "", "count=30")
	for i, k := range out.keys {
		want := strings.HasPrefix(k, "(whole/")
		if out.whole[i] != want {
			t.Errorf("%s came through as whole=%v", k, out.whole[i])
		}
	}
}

// A reversed sequence is read scope by scope like any other: the watermark is
// a position in it, and the next scope carries on from there.
func TestAReversedSequenceCarriesOn(t *testing.T) {
	c := twoIncludes(t)
	seq := opened(t, c, "")
	first, done := seq.scope("count=2 reversed")
	if first.joined() != "(right/1),(right/0)" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	if done.Stop == wire.StopExhausted {
		t.Fatal("a scope with records past it claimed to be exhausted")
	}

	next, done := seq.scope(
		"after=" + wire.EncodeValue(done.Watermark) + " count=10 reversed")
	if next.joined() != "(left/note),(left/1),(left/0)" {
		t.Errorf("the rest is %s", next.joined())
	}
	if done.Stop != wire.StopExhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// `id` names identities, and an identity says which include it came from -- so
// the includes none of them name are never opened.
//
// `id (left/1)` asks `left` about its own record 1 and asks `right` nothing at
// all, because no identity `right` hands out can begin with a name that is not
// its own.
func TestAnIDFilterReachesOneInclude(t *testing.T) {
	left, right := &spy{inner: mustPSL(t, leftDoc)}, &spy{inner: mustPSL(t, rightDoc)}
	c := composed(t,
		Include{Name: "left", Source: left},
		Include{Name: "right", Source: right})

	out, _ := read(t, c, `filter={ id (left/1) }`, "count=10")
	if out.joined() != "(left/1)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if right.opened {
		t.Error("the include that could hold nothing was opened anyway")
	}
	if !left.opened {
		t.Fatal("the include that could hold it was not opened")
	}
	// `id` composes: what goes down is the same question about the identities
	// the include knows its records by. Which of a number, a name and a string
	// that is is the include's own business, so it is asked about every
	// spelling of the text there could be.
	if got := left.spec.Filter.Encode(); got != `{ id "1" 1 }` {
		t.Errorf("the include was asked %s", got)
	}
}

// And the record identified by a name rather than a number comes back too. A
// question narrow enough to miss would lose it, which is the one thing that
// cannot happen.
func TestAnIDFilterReachesARecordKeyedByName(t *testing.T) {
	left := &spy{inner: mustPSL(t, leftDoc)}
	c := composed(t, Include{Name: "left", Source: left})

	out, _ := read(t, c, `filter={ id (left/note) }`, "count=10")
	if out.joined() != "(left/note)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if got := left.spec.Filter.Encode(); got != `{ id "note" note }` {
		t.Errorf("the include was asked %s", got)
	}
}

// A set of them reaches every include any of them names, and no others.
func TestAnIDSetReachesEveryIncludeItNames(t *testing.T) {
	left, right := &spy{inner: mustPSL(t, leftDoc)}, &spy{inner: mustPSL(t, rightDoc)}
	other := &spy{inner: mustPSL(t, rightDoc)}
	c := composed(t,
		Include{Name: "left", Source: left},
		Include{Name: "right", Source: right},
		Include{Name: "other", Source: other})

	out, _ := read(t, c, `filter={ id (left/1) (right/0) }`, "count=10")
	if out.joined() != "(left/1),(right/0)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if !left.opened || !right.opened {
		t.Error("an include the set names was not opened")
	}
	if other.opened {
		t.Error("an include the set does not name was opened")
	}
}

// A filter that names no include at all is a sequence with nothing in it, and
// it says so rather than leaving anybody waiting.
func TestAnIDFilterThatNamesNoIncludeIsEmpty(t *testing.T) {
	left, right := &spy{inner: mustPSL(t, leftDoc)}, &spy{inner: mustPSL(t, rightDoc)}
	c := composed(t,
		Include{Name: "left", Source: left},
		Include{Name: "right", Source: right})

	out, done := read(t, c, `filter={ id (nobody/1) }`, "count=10")
	if len(out.keys) != 0 {
		t.Errorf("records came back for a name no include has: %v", out.keys)
	}
	if done.Stop != wire.StopExhausted {
		t.Error("an empty sequence did not say it was over")
	}
	if left.opened || right.opened {
		t.Error("an include was opened for a filter it cannot satisfy")
	}
}

// A filter mixing identity with an ordinary field keeps both: the include
// answers the field, this source answers the identity.
func TestAFilterOnIdentityAndAFieldKeepsBoth(t *testing.T) {
	left := &spy{inner: mustPSL(t, leftDoc)}
	c := composed(t,
		Include{Name: "left", Source: left},
		Include{Name: "right", Source: mustPSL(t, rightDoc)})

	out, _ := read(t, c,
		`filter={ lt .size 100; id (left/0) (left/note) }`, "count=10")
	if out.joined() != "(left/0),(left/note)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	// The field predicate went down; the identity became the include's own.
	if got := left.spec.Filter.Encode(); got != `{ lt .size 100; id "0" 0 "note" note }` {
		t.Errorf("the include was asked %s", got)
	}
}

// `key` is a field like any other now, so a sort or a filter naming it is an
// ordinary question about whatever the include exposes under that name -- and
// it goes down untouched.
func TestKeyIsAnOrdinaryFieldName(t *testing.T) {
	left := &spy{inner: mustPSL(t, leftDoc)}
	c := composed(t, Include{Name: "left", Source: left})

	// Under Whole a PSL source exposes its own key under that name, so this
	// asks each include about ITS key, not about the identity this source made.
	out, _ := read(t, c, `filter={ eq key 1 }`, "count=10")
	if out.joined() != "(left/1)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if got := left.spec.Filter.Encode(); got != `{ eq key 1 }` {
		t.Errorf("the include was asked %s", got)
	}
}

// The names are what the records are told apart by, so a set that could not
// tell them apart is refused when it is made.
func TestTheIncludesAreCheckedWhenTheyAreGiven(t *testing.T) {
	ok := mustPSL(t, leftDoc)
	for _, bad := range [][]Include{
		{{Name: "", Source: ok}},
		{{Name: "has/slash", Source: ok}},
		{{Name: "none", Source: nil}},
		{{Name: "same", Source: ok}, {Name: "same", Source: ok}},
	} {
		if _, err := NewComposedSource(bad...); err == nil {
			t.Errorf("%#v was accepted", bad)
		}
	}
}

// Nothing waits. The includes are asked and the records reach the sink as they
// arrive, which here is during the asking.
func TestAskingEveryIncludeDoesNotWait(t *testing.T) {
	c := composed(t,
		Include{Name: "here", Source: mustPSL(t, leftDoc)},
		Include{Name: "app", Source: serving(t)})

	set, err := c.Open(parseSpec(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	out := &collector{}
	if err := set.Read(parseScope(t, "count=30"), out); err != nil {
		t.Fatal(err)
	}
	if len(out.keys) != 7 || !out.ended {
		t.Errorf("the answer arrived as %v, ended=%v", out.keys, out.ended)
	}
}

// What is held is how far the includes have drifted out of step, not the
// answer.
//
// Here one include delivers everything before the other delivers anything. The
// first one's records cannot go out while the second has said nothing -- it
// might hold a record that belongs before all of them -- so they wait. The
// moment the second speaks, the merge runs, and what was waiting was one
// include's scope rather than the whole sequence.
func TestOnlyWhatIsOutOfStepIsHeld(t *testing.T) {
	slow := &withheld{inner: mustPSL(t, rightDoc)}
	c := composed(t,
		Include{Name: "fast", Source: mustPSL(t, leftDoc)},
		Include{Name: "slow", Source: slow})

	set, err := c.Open(parseSpec(t, "sort={ .size }"))
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()

	out := &collector{}
	if err := set.Read(parseScope(t, "count=10"), out); err != nil {
		t.Fatal(err)
	}
	if len(out.keys) != 0 {
		t.Fatalf("records went out before every include had spoken: %v", out.keys)
	}

	slow.let()
	if out.joined() != "(fast/note),(fast/0),(slow/0),(fast/1),(slow/1)" {
		t.Errorf("the merged sequence is %s", out.joined())
	}
	if !out.ordered {
		t.Error("the answer did not say it was in order")
	}
}

// withheld is a source that keeps its answer until it is let go, which is what
// an include reached over a connection does until the connection answers.
type withheld struct {
	inner Source
	mu    sync.Mutex
	held  []func()
}

func (w *withheld) Open(spec *wire.Spec) (DataSet, error) {
	set, err := w.inner.Open(spec)
	if err != nil {
		return nil, err
	}
	return &withheldSet{src: w, inner: set}, nil
}

func (w *withheld) let() {
	w.mu.Lock()
	held := w.held
	w.held = nil
	w.mu.Unlock()
	for _, fn := range held {
		fn()
	}
}

type withheldSet struct {
	src   *withheld
	inner DataSet
}

func (s *withheldSet) Close() { s.inner.Close() }

func (s *withheldSet) Read(f *wire.Scope, out Sink) error {
	s.src.mu.Lock()
	s.src.held = append(s.src.held, func() { _ = s.inner.Read(f, out) })
	s.src.mu.Unlock()
	return nil
}

// Every include is asked for the whole shortfall, not a share of it.
//
// Any one of them may turn out to hold all of it, so a share would leave the
// scope short whenever the records are not spread evenly. And `have` is the
// outer reader's, not any include's: an include holds none of what the reader
// already has, so it is asked from nothing.
func TestEveryIncludeIsAskedForTheWholeShortfall(t *testing.T) {
	left, right := &spy{inner: mustPSL(t, leftDoc)}, &spy{inner: mustPSL(t, rightDoc)}
	c := composed(t,
		Include{Name: "left", Source: left},
		Include{Name: "right", Source: right})

	read(t, c, "sort={ .size }", "count=5")
	for _, s := range []*spy{left, right} {
		if s.asked.Count != 5 {
			t.Errorf("an include was asked for %d, want the whole 5", s.asked.Count)
		}
	}
}

// spy is a source that keeps the request it was given.
type spy struct {
	inner  Source
	asked  *wire.Scope
	reads  int // how many times a scope was put to it
	spec   *wire.Spec
	opened bool
}

func (s *spy) Open(spec *wire.Spec) (DataSet, error) {
	set, err := s.inner.Open(spec)
	if err != nil {
		return nil, err
	}
	s.opened, s.spec = true, spec
	return &spySet{src: s, inner: set}, nil
}

type spySet struct {
	src   *spy
	inner DataSet
}

func (s *spySet) Close() { s.inner.Close() }

func (s *spySet) Read(f *wire.Scope, out Sink) error {
	s.src.asked = f
	s.src.reads++
	return s.inner.Read(f, out)
}

// Resuming a reversed sequence part way through one include.
//
// Both includes here send everything they hold whatever they were asked from,
// so the boundary is applied here and nowhere else -- which is what makes the
// two levels of the composed key visible. Descending, `all/note` comes before
// `all/1` and `all/0`, so a boundary at `all/note` leaves the last two and
// nothing else: the other include sorts ahead of this one and is behind the
// boundary entire.
func TestAReversedBoundaryFallsInsideAnInclude(t *testing.T) {
	c := composed(t,
		Include{Name: "all", Source: &ignoresBoundaries{inner: mustPSL(t, leftDoc)}},
		Include{Name: "other", Source: &ignoresBoundaries{inner: mustPSL(t, rightDoc)}})

	seq := opened(t, c, "")
	whole, _ := seq.scope("count=10 reversed")
	if whole.joined() != "(other/1),(other/0),(all/note),(all/1),(all/0)" {
		t.Fatalf("reversed, the sequence is %s", whole.joined())
	}

	out, _ := seq.scope("after=(all/note) count=10 reversed")
	if out.joined() != "(all/1),(all/0)" {
		t.Errorf("the scope after all/note is %s", out.joined())
	}
}

// A predicate on an ordinary field says nothing here, and saying nothing is
// not saying no.
//
// The include was asked that predicate and applied it already, so a record
// that arrived has passed it. Reading it here as a yes would be wrong under a
// negation -- the negation would turn it into a no and take out a record the
// include had just vouched for -- so what this source cannot see it declines
// to answer, and only a definite no drops anything.
func TestAPredicateOnAnotherFieldIsNotAnsweredHere(t *testing.T) {
	left := &spy{inner: mustPSL(t, leftDoc)}
	c := composed(t, Include{Name: "left", Source: left})

	out, _ := read(t, c,
		`filter={ not { gt .size 100 }; id (left/1) }`, "count=10")
	if out.joined() != "(left/1)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	// The negation went down whole, because it names no key.
	if got := left.spec.Filter.Encode(); got != `{ not { gt .size 100 }; id "1" 1 }` {
		t.Errorf("the include was asked %s", got)
	}
}

// A disjunction keeps every include that any branch admits, and the key
// branches still pick which.
func TestADisjunctionOnIdentityKeepsEveryBranchsIncludes(t *testing.T) {
	out, _ := read(t, twoIncludes(t),
		`filter={ or { id (left/1); id (right/0) } }`, "count=10")
	if out.joined() != "(left/1),(right/0)" {
		t.Errorf("the sequence is %s", out.joined())
	}
}

// A negation on the key is answered here and takes the record it names out.
func TestANegationOnIdentityTakesThatRecordOut(t *testing.T) {
	left := &spy{inner: mustPSL(t, leftDoc)}
	c := composed(t, Include{Name: "left", Source: left})

	out, _ := read(t, c, `filter={ not { id (left/1) } }`, "count=10")
	if out.joined() != "(left/0),(left/note)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	// A negation of something that became a different question cannot be
	// handed down -- negating a widened filter narrows it -- so the include was
	// asked nothing and this source settled it.
	if left.spec.Filter != nil {
		t.Errorf("the include was asked %s", left.spec.Filter.Encode())
	}
}

// A branch that admits every record of an include makes the whole disjunction
// admit them, so that include is asked nothing rather than asked the other
// branches -- which would be a narrower question than the one that was put.
func TestADisjunctionBranchThatAdmitsEverythingAsksNothing(t *testing.T) {
	left := &spy{inner: mustPSL(t, leftDoc)}
	c := composed(t, Include{Name: "left", Source: left})

	out, _ := read(t, c,
		`filter={ or { not { id (nobody/0) }; eq .size 999 } }`, "count=10")
	if out.joined() != "(left/0),(left/1),(left/note)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if left.spec.Filter != nil {
		t.Errorf("the include was asked %s", left.spec.Filter.Encode())
	}
}

// Which includes are worth asking at all.
//
// Only `id` says anything about that: it names identities, and an identity
// says which include made it. Every other operator asks about a FIELD, and a
// field called `key` is a field like any other -- this source knows nothing
// about what any include holds under it, so the question goes down and every
// include is opened.
func TestWhichIncludesAreWorthAsking(t *testing.T) {
	for _, c := range []struct {
		filter string
		open   []bool // left, right
	}{
		{`filter={ id (left/1) (left/note) }`, []bool{true, false}},
		{`filter={ id (right/0) }`, []bool{false, true}},
		{`filter={ id (left/1) (right/0) }`, []bool{true, true}},
		{`filter={ id (nobody/1) }`, []bool{false, false}},
		// A field called `key` says nothing about which include holds what.
		{`filter={ eq key 1 }`, []bool{true, true}},
		{`filter={ starts key "left/" }`, []bool{true, true}},
		{`filter={ has key }`, []bool{true, true}},
	} {
		left, right := &spy{inner: mustPSL(t, leftDoc)}, &spy{inner: mustPSL(t, rightDoc)}
		set := composed(t,
			Include{Name: "left", Source: left},
			Include{Name: "right", Source: right})
		read(t, set, c.filter, "count=10")

		got := []bool{left.opened, right.opened}
		if got[0] != c.open[0] || got[1] != c.open[1] {
			t.Errorf("%s opened left=%v right=%v, want %v",
				c.filter, got[0], got[1], c.open)
		}
	}
}

// An identity composes with the slash, so it reaches through a composed source
// inside a composed source: each layer strips its own name off the front and
// asks the next about what is left.
func TestAnIDFilterReachesThroughNesting(t *testing.T) {
	deep := &spy{inner: mustPSL(t, rightDoc)}
	inner := composed(t, Include{Name: "deep", Source: deep})
	flat := &spy{inner: mustPSL(t, leftDoc)}
	c := composed(t,
		Include{Name: "flat", Source: flat},
		Include{Name: "nested", Source: inner})

	out, _ := read(t, c, `filter={ id (nested/deep/1) }`, "count=10")
	if out.joined() != "(nested/deep/1)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if flat.opened {
		t.Error("the include the identity does not name was opened")
	}
	// One name came off at each level, and what reached the bottom is the
	// identity that source knows the record by.
	if got := deep.spec.Filter.Encode(); got != `{ id "1" 1 }` {
		t.Errorf("the innermost source was asked %s", got)
	}
}

// Reversed turns a composed sequence over entire: the includes back to front,
// and the records inside each of them too.
//
// It names no field, so what goes down to every include is `reversed` as well,
// and no include is ever asked about a key by name.
func TestAComposedSourceReverses(t *testing.T) {
	forward, _ := read(t, twoIncludes(t), "", "count=10")
	if forward.joined() != "(left/0),(left/1),(left/note),(right/0),(right/1)" {
		t.Fatalf("forward, the sequence is %s", forward.joined())
	}
	mirror, _ := read(t, twoIncludes(t), "", "count=10 reversed")
	if mirror.joined() != "(right/1),(right/0),(left/note),(left/1),(left/0)" {
		t.Errorf("reversed, the sequence is %s", mirror.joined())
	}
	if !mirror.ordered {
		t.Error("a reversed sequence did not say it was in order")
	}
}

// Under a sort of its own, the mirror is the whole sequence read backwards --
// every level turned over, the one that settles ties included.
func TestAComposedSourceReversesUnderASort(t *testing.T) {
	forward, _ := read(t, twoIncludes(t), "sort={ .size }", "count=10")
	if forward.joined() != "(left/note),(left/0),(right/0),(left/1),(right/1)" {
		t.Fatalf("forward, the sequence is %s", forward.joined())
	}

	left, right := &spy{inner: mustPSL(t, leftDoc)}, &spy{inner: mustPSL(t, rightDoc)}
	c := composed(t,
		Include{Name: "left", Source: left},
		Include{Name: "right", Source: right})

	mirror, _ := read(t, c, "sort={ .size }", "count=10 reversed")
	if mirror.joined() != "(right/1),(left/1),(right/0),(left/0),(left/note)" {
		t.Errorf("reversed, the sequence is %s", mirror.joined())
	}
	// The direction goes down with the scope, not with the sequence: an
	// include prepares one ordering and is asked to walk it backwards.
	for _, s := range []*spy{left, right} {
		if got := wire.EncodeSort(s.spec.Sort); got != "{ .size }" {
			t.Errorf("an include was asked sort=%s", got)
		}
		if !s.asked.Reversed {
			t.Error("an include was not asked to walk its sequence backwards")
		}
	}
}

// An include is asked for the fields this source sorts by, whatever the query
// asked for.
//
// The merge reads a record's sort values back out of the fields it was sent.
// A sort on a field the query did not ask for would arrive here as undefined
// for every record, and the merge would then trust each include's arrival
// order over an order it could not see -- the includes would come out one
// after another instead of interleaved, and it would say they were in order.
func TestAnIncludeIsAskedForTheFieldsTheSortNeeds(t *testing.T) {
	left, right := &spy{inner: mustPSL(t, leftDoc)}, &spy{inner: mustPSL(t, rightDoc)}
	c := composed(t,
		Include{Name: "left", Source: left},
		Include{Name: "right", Source: right})

	out, _ := read(t, c, "fields={ .name } sort={ .size }", "count=10")
	if out.joined() != "(left/note),(left/0),(right/0),(left/1),(right/1)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	for _, s := range []*spy{left, right} {
		if got := s.spec.Fields.Encode(); got != "{ .name; .size }" {
			t.Errorf("an include was asked for %s", got)
		}
	}
	// What comes back is a superset of what was asked for, which is allowed.
	if got := out.fields[0].Encode(); got != `{ .name "left note"; .size 5 }` {
		t.Errorf("the first record carries %s", got)
	}
}

// Each include is resumed from the last record IT gave, not from the last one
// this source handed on.
//
// The two are different whenever the record that ended a scope came from one
// include and another had already given something before it: resuming that
// other one from where the scope ended would send its records again. They are
// dropped on the way through, so nothing comes out wrong -- it is the include
// that is asked the wrong question, and the only place to see that is the
// question.
func TestEachIncludeIsResumedFromItsOwnLastRecord(t *testing.T) {
	left := &spy{inner: mustPSL(t, leftDoc)}
	right := &spy{inner: mustPSL(t, rightDoc)}
	c := composed(t,
		Include{Name: "left", Source: left},
		Include{Name: "right", Source: right})
	seq := opened(t, c, "sort={ .size }")

	// By size: (left/note), (left/0), (right/0), (left/1), (right/1).
	first, _ := seq.scope("count=2")
	if first.joined() != "(left/note),(left/0)" {
		t.Fatalf("the first scope is %s", first.joined())
	}

	seq.scope("after=(left/0) count=9")
	if got := wire.EncodeValue(left.asked.After); got != "0" {
		t.Errorf("left was resumed after %s, want its own 0", got)
	}
	// Right had given nothing before the scope ended, so it starts where it was.
	if right.asked.After != nil {
		t.Errorf("right was resumed after %s, having given nothing",
			wire.EncodeValue(right.asked.After))
	}
}

// `until` reaches the includes in their own terms, the way `after` does.
//
// Without it they walk to the count instead of stopping, and no include ever
// reports that it joined -- so this source cannot say it either, and the asker
// is never told its two runs have become one.
func TestUntilReachesTheIncludes(t *testing.T) {
	c := twoIncludes(t)
	seq := opened(t, c, "sort={ .size }")

	// Everything placed, so the identities below mean something to this source.
	if all, _ := seq.scope("count=9"); all.joined() != "(left/note),(left/0),(right/0),(left/1),(right/1)" {
		t.Fatalf("the sequence is %s", all.joined())
	}

	out, done := seq.scope("until=(right/0) count=9")
	if out.joined() != "(left/note),(left/0)" {
		t.Errorf("the walk up to the record the asker held is %s", out.joined())
	}
	if done.Stop != wire.StopJoined {
		t.Errorf("it stopped at that record and said %q", done.Stop)
	}
}
