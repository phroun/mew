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
	out, done := read(t, twoIncludes(t), "", "have=0 need=10")
	if out.joined() != "(left/0),(left/1),(left/note),(right/0),(right/1)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if !done.Exhausted {
		t.Error("a scope holding every record did not say so")
	}
}

// Nothing shadows anything. Two includes keyed the same way both keep their
// records, because the name in front of the key is what tells them apart.
func TestTwoIncludesKeyedAlikeDoNotCollide(t *testing.T) {
	c := composed(t,
		Include{Name: "one", Source: mustPSL(t, rightDoc)},
		Include{Name: "two", Source: mustPSL(t, rightDoc)})

	out, _ := read(t, c, "", "have=0 need=10")
	if out.joined() != "(one/0),(one/1),(two/0),(two/1)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if len(out.keys) != 4 {
		t.Errorf("%d records for two includes of two", len(out.keys))
	}
}

// The query's own sort comes first, and the includes interleave under it.
func TestTheIncludesInterleaveUnderTheSort(t *testing.T) {
	out, _ := read(t, twoIncludes(t), "sort={ .size }", "have=0 need=10")
	if out.joined() != "(left/note),(left/0),(right/0),(left/1),(right/1)" {
		t.Errorf("by size the sequence is %s", out.joined())
	}
	if got := out.fields[2].Encode(); got != `{ .name "beta"; .size 40 }` {
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
	out, _ := read(t, c, "", "have=0 need=20")
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
	first, done := read(t, c, "sort={ .size }", "have=0 need=2")
	if first.joined() != "(left/note),(left/0)" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	if done.Exhausted {
		t.Error("a scope with records past it claimed to be exhausted")
	}

	next, done := read(t, c, "sort={ .size }",
		"from="+done.Watermark.Encode()+" have=0 need=10")
	if next.joined() != "(right/0),(left/1),(right/1)" {
		t.Errorf("the rest is %s", next.joined())
	}
	if !done.Exhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// The boundary names one include, and the others are asked from the start of
// that sort value rather than being skipped: what they send that the boundary
// already covered is dropped here.
func TestABoundaryInOneIncludeDoesNotSkipTheOthers(t *testing.T) {
	c := twoIncludes(t)
	out, _ := read(t, c, "", `from={ key (left/0) } have=0 need=10`)
	if out.joined() != "(left/1),(left/note),(right/0),(right/1)" {
		t.Errorf("the scope after left/0 is %s", out.joined())
	}
}

// Order is claimed only when every include promised it, and it is claimed
// before the records, which is the only place it is worth anything.
func TestOrderIsClaimedOnlyWhenEveryIncludePromisedIt(t *testing.T) {
	ordered, _ := read(t, twoIncludes(t), "sort={ .size }", "have=0 need=10")
	if !ordered.ordered {
		t.Error("includes that were all in order made an answer that was not")
	}

	c := composed(t,
		Include{Name: "left", Source: mustPSL(t, leftDoc)},
		Include{Name: "right", Source: &jumbled{inner: mustPSL(t, rightDoc)}})
	mixed, _ := read(t, c, "sort={ .size }", "have=0 need=10")
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

	out, done := read(t, c, "sort={ .size }", "have=0 need=10")
	if out.joined() != "(left/note),(right/0)" {
		t.Fatalf("the scope is %s", out.joined())
	}
	if done.Exhausted {
		t.Fatal("includes that had more claimed the sequence was over")
	}
	// left swept to size 5 and right to size 40. Only 5 is true of both.
	if got := done.Watermark.Encode(); got != `{ .size 5; key (left/note) }` {
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

	out, _ := read(t, c, "", `from={ key (all/0) } have=0 need=10`)
	if out.joined() != "(all/1),(all/note),(other/0),(other/1)" {
		t.Errorf("the scope after all/0 is %s", out.joined())
	}
}

// ignoresBoundaries is a source that sends every record it has whatever it was
// asked from, which is the least an implementation can do and is legal.
type ignoresBoundaries struct{ inner Source }

func (e *ignoresBoundaries) Open(spec *wire.Spec) (ResultSet, error) {
	set, err := e.inner.Open(spec)
	if err != nil {
		return nil, err
	}
	return &everythingSet{inner: set}, nil
}

type everythingSet struct{ inner ResultSet }

func (e *everythingSet) Close() { e.inner.Close() }

func (e *everythingSet) Fill(f *wire.Fill, out Sink) error {
	whole := *f
	whole.From, whole.To, whole.Have = nil, nil, 0
	whole.Need = 1 << 20
	return e.inner.Fill(&whole, out)
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

	first, done := read(t, nested(), "", "have=0 need=4")
	if first.joined() != "(flat/0),(flat/1),(flat/note),(nested/deep/0)" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	if done.Exhausted {
		t.Fatal("a scope with a record past it claimed to be exhausted")
	}

	next, done := read(t, nested(), "",
		"from="+done.Watermark.Encode()+" have=0 need=10")
	if next.joined() != "(nested/deep/1)" {
		t.Errorf("the rest is %s", next.joined())
	}
	if !done.Exhausted {
		t.Error("the end of the sequence did not say so")
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
	out, done := read(t, twoIncludes(t), "sort={ .size }", "have=0 need=3")
	if out.joined() != "(left/note),(left/0),(right/0)" {
		t.Fatalf("the scope is %s", out.joined())
	}
	if done.Exhausted {
		t.Error("a scope with two records still to come said there were none")
	}
	if got := done.Watermark.Encode(); got != "{ .size 40; key (right/0) }" {
		t.Errorf("the watermark is %s", got)
	}

	// And the next scope picks up exactly the two that were held.
	next, done := read(t, twoIncludes(t), "sort={ .size }",
		"from="+done.Watermark.Encode()+" have=0 need=9")
	if next.joined() != "(left/1),(right/1)" {
		t.Errorf("the rest is %s", next.joined())
	}
	if !done.Exhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// short is a source that answers one record and stops, saying how far it got,
// which every application is free to do.
type short struct{ inner Source }

func (s *short) Open(spec *wire.Spec) (ResultSet, error) {
	set, err := s.inner.Open(spec)
	if err != nil {
		return nil, err
	}
	return &shortSet{inner: set, sort: spec.Sort}, nil
}

type shortSet struct {
	inner ResultSet
	sort  []wire.SortLevel
}

func (s *shortSet) Close() { s.inner.Close() }

func (s *shortSet) Fill(f *wire.Fill, out Sink) error {
	return s.inner.Fill(f, &stopAfterOne{out: out, sort: s.sort})
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

// Done says where it got to: the sort fields of the one record it sent, and
// its key, which is what a boundary is made of.
func (s *stopAfterOne) Done(Complete) {
	mark := make(wire.Fields, 0, len(s.sort)+1)
	for _, l := range s.sort {
		mark = append(mark, &wire.Arg{Name: l.Field, Value: s.fields.Get(l.Field)})
	}
	mark = append(mark, wire.Named(wire.KeyField, s.key))
	s.out.Done(Complete{Watermark: mark})
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
	if err := set.Fill(parseFill(t, "have=0 need=10"), out); err != nil {
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

	out, done := read(t, c, "", "have=0 need=30")
	if !done.Exhausted {
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

	out, _ := read(t, c, "", "have=0 need=30")
	for i, k := range out.keys {
		want := strings.HasPrefix(k, "(whole/")
		if out.whole[i] != want {
			t.Errorf("%s came through as whole=%v", k, out.whole[i])
		}
	}
}

// The composed key is two sort levels -- the include's name and the child's
// key -- and both take the direction the sort asked of `key`.
//
// So `key desc` is the sequence's own order reversed entire: the includes come
// back to front, and the records inside each of them do too. Half a reversal
// would be worse than a refusal -- each include would deliver in one order
// while the merge expected another, and the merge only ever sees a queue's
// head, so it would hand records on backwards and call them ordered.
func TestTheKeyIsTwoSortLevelsAndBothTakeItsDirection(t *testing.T) {
	up, _ := read(t, twoIncludes(t), "sort={ key }", "have=0 need=10")
	if up.joined() != "(left/0),(left/1),(left/note),(right/0),(right/1)" {
		t.Errorf("ascending, the sequence is %s", up.joined())
	}

	down, _ := read(t, twoIncludes(t), "sort={ key desc }", "have=0 need=10")
	if down.joined() != "(right/1),(right/0),(left/note),(left/1),(left/0)" {
		t.Errorf("descending, the sequence is %s", down.joined())
	}
	if !down.ordered {
		t.Error("a reversed sequence did not say it was in order")
	}
}

// The key can sit under a sort of its own, where it settles what that one left
// equal -- and it still carries its own direction there.
func TestTheKeyTakesItsDirectionUnderAnotherLevel(t *testing.T) {
	// Two includes holding a record apiece of the same size, so the sort ties
	// and the key decides.
	c := composed(t,
		Include{Name: "a", Source: mustPSL(t, `( (n: "one", size: 7) )`)},
		Include{Name: "b", Source: mustPSL(t, `( (n: "two", size: 7) )`)})

	up, _ := read(t, c, "sort={ .size; key }", "have=0 need=10")
	if up.joined() != "(a/0),(b/0)" {
		t.Errorf("ascending, the tie settles as %s", up.joined())
	}
	down, _ := read(t, c, "sort={ .size; key desc }", "have=0 need=10")
	if down.joined() != "(b/0),(a/0)" {
		t.Errorf("descending, the tie settles as %s", down.joined())
	}
}

// A reversed sequence is read scope by scope like any other: the watermark is
// a position in it, and the next scope carries on from there.
func TestAReversedSequenceCarriesOn(t *testing.T) {
	c := twoIncludes(t)
	first, done := read(t, c, "sort={ key desc }", "have=0 need=2")
	if first.joined() != "(right/1),(right/0)" {
		t.Fatalf("the first scope is %s", first.joined())
	}
	if done.Exhausted {
		t.Fatal("a scope with records past it claimed to be exhausted")
	}

	next, done := read(t, c, "sort={ key desc }",
		"from="+done.Watermark.Encode()+" have=0 need=10")
	if next.joined() != "(left/note),(left/1),(left/0)" {
		t.Errorf("the rest is %s", next.joined())
	}
	if !done.Exhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// A filter on the composed key is read here and asked of the includes in
// their own terms.
//
// `eq key (left/1)` is a question about one record of one include. `left` is
// asked for its own record 1; `right` is not asked anything at all, because no
// key it holds can come out under a name that is not its own.
func TestAFilterOnTheKeyReachesOneInclude(t *testing.T) {
	left, right := &spy{inner: mustPSL(t, leftDoc)}, &spy{inner: mustPSL(t, rightDoc)}
	c := composed(t,
		Include{Name: "left", Source: left},
		Include{Name: "right", Source: right})

	out, _ := read(t, c, `filter={ eq key (left/1) }`, "have=0 need=10")
	if out.joined() != "(left/1)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if right.opened {
		t.Error("the include that could hold nothing was opened anyway")
	}
	if !left.opened {
		t.Fatal("the include that could hold it was not opened")
	}
	// Which of a number, a name and a string it keys its records by is its own
	// business, so it is asked about every spelling of the text there could be.
	if got := left.spec.Filter.Encode(); got != `{ in key "1" 1 }` {
		t.Errorf("the include was asked %s", got)
	}
}

// And the record keyed by a name rather than a number comes back too.
//
// A PSL list keys its members by string and its items by number, and the text
// between the slashes says which it was for neither. So the include is asked
// about both spellings, and the one that is right matches -- a question narrow
// enough to miss would lose the record, which is the one thing that cannot
// happen.
func TestAFilterOnTheKeyReachesARecordKeyedByName(t *testing.T) {
	left := &spy{inner: mustPSL(t, leftDoc)}
	c := composed(t, Include{Name: "left", Source: left})

	out, _ := read(t, c, `filter={ eq key (left/note) }`, "have=0 need=10")
	if out.joined() != "(left/note)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if got := left.spec.Filter.Encode(); got != `{ in key "note" note }` {
		t.Errorf("the include was asked %s", got)
	}
}

// The same reading answers a range: every key an include holds starts with its
// own name, so a value outside that settles the predicate for all of them at
// once and the include is skipped or asked nothing.
func TestARangeOnTheKeyPicksTheIncludes(t *testing.T) {
	for _, c := range []struct {
		filter string
		want   string
	}{
		{`filter={ gt key (left/note) }`, "(right/0),(right/1)"},
		{`filter={ lt key (right/0) }`, "(left/0),(left/1),(left/note)"},
		{`filter={ ge key (right/0) }`, "(right/0),(right/1)"},
	} {
		out, _ := read(t, twoIncludes(t), c.filter, "have=0 need=10")
		if out.joined() != c.want {
			t.Errorf("%s gave %s, want %s", c.filter, out.joined(), c.want)
		}
	}
}

// A filter that shuts every include out is a sequence with nothing in it, and
// it says so rather than leaving anybody waiting.
func TestAFilterThatShutsEveryIncludeOutIsEmpty(t *testing.T) {
	left, right := &spy{inner: mustPSL(t, leftDoc)}, &spy{inner: mustPSL(t, rightDoc)}
	c := composed(t,
		Include{Name: "left", Source: left},
		Include{Name: "right", Source: right})

	out, done := read(t, c, `filter={ eq key (nobody/1) }`, "have=0 need=10")
	if len(out.keys) != 0 {
		t.Errorf("records came back for a name no include has: %v", out.keys)
	}
	if !done.Exhausted {
		t.Error("an empty sequence did not say it was over")
	}
	if left.opened || right.opened {
		t.Error("an include was opened for a filter it cannot satisfy")
	}
}

// A predicate this source cannot put in an include's terms is dropped on the
// way down rather than guessed at, and settled here instead -- so what the
// include sends is a superset and nothing matching is ever cut.
func TestAPredicateThatCannotBeHandedDownIsSettledHere(t *testing.T) {
	left := &spy{inner: mustPSL(t, leftDoc)}
	c := composed(t, Include{Name: "left", Source: left})

	out, _ := read(t, c, `filter={ ne key (left/0) }`, "have=0 need=10")
	if out.joined() != "(left/1),(left/note)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	// `ne` on one of its own keys is not a question the include can be asked
	// in its own terms, so it was asked nothing and answered everything.
	if left.spec.Filter != nil {
		t.Errorf("the include was asked %s", left.spec.Filter.Encode())
	}
}

// A filter mixing the key with an ordinary field keeps both: the include
// answers the field, this source answers the key.
func TestAFilterOnTheKeyAndAFieldKeepsBoth(t *testing.T) {
	left := &spy{inner: mustPSL(t, leftDoc)}
	c := composed(t,
		Include{Name: "left", Source: left},
		Include{Name: "right", Source: mustPSL(t, rightDoc)})

	out, _ := read(t, c, `filter={ lt .size 100; gt key (left/0) }`, "have=0 need=10")
	if out.joined() != "(left/1),(left/note),(right/0),(right/1)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if got := left.spec.Filter.Encode(); got != `{ lt .size 100 }` {
		t.Errorf("the include was asked %s", got)
	}
}

// An include is never asked for the composed key by name.
//
// It orders its own records by its own key once the named levels are spent, so
// that level is one it has anyway -- and asking for it by NAME would put the
// question to a reading that may not be able to answer it. A PSL source read
// for its members cannot name a record's key at all, and answers this
// perfectly well, because which way round that last level goes is said with
// `reversed` instead.
func TestAnIncludeIsNeverAskedForTheKeyByName(t *testing.T) {
	members, err := ParsePSLSource(leftDoc, Members)
	if err != nil {
		t.Fatal(err)
	}
	m := &spy{inner: members}
	c := composed(t, Include{Name: "m", Source: m})

	out, _ := read(t, c, "sort={ key desc }", "have=0 need=10")
	if out.joined() != "(m/note),(m/1),(m/0)" {
		t.Errorf("reversed, the sequence is %s", out.joined())
	}
	if len(m.spec.Sort) != 0 || !m.spec.Reversed {
		t.Errorf("the include was asked sort=%s reversed=%v",
			wire.EncodeSort(m.spec.Sort), m.spec.Reversed)
	}
}

// The same translation under a level of its own: what is asked for is the
// mirror of what is wanted, and reversing it lands on the sequence.
func TestTheKeyLevelBecomesReversedUnderAnotherLevel(t *testing.T) {
	members, err := ParsePSLSource(leftDoc, Members)
	if err != nil {
		t.Fatal(err)
	}
	m := &spy{inner: members}
	c := composed(t, Include{Name: "m", Source: m})

	// By size ascending, and the key descending inside that -- which for these
	// records is every one of them tied nowhere, so it is size order.
	out, _ := read(t, c, "sort={ size; key desc }", "have=0 need=10")
	if out.joined() != "(m/note),(m/0),(m/1)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if got := wire.EncodeSort(m.spec.Sort); got != "{ size desc }" || !m.spec.Reversed {
		t.Errorf("the include was asked sort=%s reversed=%v", got, m.spec.Reversed)
	}
}

// An ascending key needs no reversal, and the level goes away rather than
// going down -- a key names one record, so nothing written after it could
// separate two.
func TestAnAscendingKeyLevelIsSimplyDropped(t *testing.T) {
	left := &spy{inner: mustPSL(t, leftDoc)}
	c := composed(t, Include{Name: "left", Source: left})

	out, _ := read(t, c, "sort={ .size desc; key; .name }", "have=0 need=10")
	if out.joined() != "(left/1),(left/0),(left/note)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if got := wire.EncodeSort(left.spec.Sort); got != "{ .size desc }" || left.spec.Reversed {
		t.Errorf("the include was asked sort=%s reversed=%v", got, left.spec.Reversed)
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
	if err := set.Fill(parseFill(t, "have=0 need=30"), out); err != nil {
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
	if err := set.Fill(parseFill(t, "have=0 need=10"), out); err != nil {
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

func (w *withheld) Open(spec *wire.Spec) (ResultSet, error) {
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
	inner ResultSet
}

func (s *withheldSet) Close() { s.inner.Close() }

func (s *withheldSet) Fill(f *wire.Fill, out Sink) error {
	s.src.mu.Lock()
	s.src.held = append(s.src.held, func() { _ = s.inner.Fill(f, out) })
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

	read(t, c, "sort={ .size }", "have=2 need=5")
	for _, s := range []*spy{left, right} {
		if s.asked.Need != 3 || s.asked.Have != 0 {
			t.Errorf("an include was asked have=%d need=%d, want 0 and 3",
				s.asked.Have, s.asked.Need)
		}
	}
}

// spy is a source that keeps the request it was given.
type spy struct {
	inner  Source
	asked  *wire.Fill
	spec   *wire.Spec
	opened bool
}

func (s *spy) Open(spec *wire.Spec) (ResultSet, error) {
	set, err := s.inner.Open(spec)
	if err != nil {
		return nil, err
	}
	s.opened, s.spec = true, spec
	return &spySet{src: s, inner: set}, nil
}

type spySet struct {
	src   *spy
	inner ResultSet
}

func (s *spySet) Close() { s.inner.Close() }

func (s *spySet) Fill(f *wire.Fill, out Sink) error {
	s.src.asked = f
	return s.inner.Fill(f, out)
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

	whole, _ := read(t, c, "sort={ key desc }", "have=0 need=10")
	if whole.joined() != "(other/1),(other/0),(all/note),(all/1),(all/0)" {
		t.Fatalf("reversed, the sequence is %s", whole.joined())
	}

	out, _ := read(t, c, "sort={ key desc }",
		`from={ key (all/note) } have=0 need=10`)
	if out.joined() != "(all/1),(all/0)" {
		t.Errorf("the scope after all/note is %s", out.joined())
	}
}

// A boundary naming no key at all is not a position in this sequence.
//
// It says which sort value to start at and nothing about which of the records
// there are already held, so there is nothing to drop against and whatever the
// includes send stands. That is a superset, which is allowed; reading it as a
// position instead would put it at one end of the order and silently cut the
// records at the other.
func TestABoundaryWithNoKeyDropsNothing(t *testing.T) {
	c := composed(t,
		Include{Name: "all", Source: &ignoresBoundaries{inner: mustPSL(t, leftDoc)}},
		Include{Name: "other", Source: &ignoresBoundaries{inner: mustPSL(t, rightDoc)}})

	out, _ := read(t, c, "sort={ key desc }", `from={ .size 30 } have=0 need=10`)
	if out.joined() != "(other/1),(other/0),(all/note),(all/1),(all/0)" {
		t.Errorf("the sequence is %s", out.joined())
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
		`filter={ not { gt .size 100 }; eq key (left/1) }`, "have=0 need=10")
	if out.joined() != "(left/1)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	// The negation went down whole, because it names no key.
	if got := left.spec.Filter.Encode(); got != `{ not { gt .size 100 }; in key "1" 1 }` {
		t.Errorf("the include was asked %s", got)
	}
}

// A disjunction keeps every include that any branch admits, and the key
// branches still pick which.
func TestADisjunctionOnTheKeyKeepsEveryBranchsIncludes(t *testing.T) {
	out, _ := read(t, twoIncludes(t),
		`filter={ or { eq key (left/1); eq key (right/0) } }`, "have=0 need=10")
	if out.joined() != "(left/1),(right/0)" {
		t.Errorf("the sequence is %s", out.joined())
	}
}

// A negation on the key is answered here and takes the record it names out.
func TestANegationOnTheKeyTakesThatRecordOut(t *testing.T) {
	left := &spy{inner: mustPSL(t, leftDoc)}
	c := composed(t, Include{Name: "left", Source: left})

	out, _ := read(t, c, `filter={ not { eq key (left/1) } }`, "have=0 need=10")
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
		`filter={ or { ne key (nobody/0); eq .size 999 } }`, "have=0 need=10")
	if out.joined() != "(left/0),(left/1),(left/note)" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if left.spec.Filter != nil {
		t.Errorf("the include was asked %s", left.spec.Filter.Encode())
	}
}

// Which includes are worth asking at all, for the predicates that settle it
// without asking anything.
func TestWhichIncludesAreWorthAsking(t *testing.T) {
	for _, c := range []struct {
		filter string
		open   []bool // left, right
	}{
		// A set names keys of one include, so the other holds none of them.
		{`filter={ in key (left/1) (left/note) }`, []bool{true, false}},
		// Every key of an include begins with its own name, so a text
		// predicate on a symbol is false for all of them -- which is what a
		// text predicate on any symbol is.
		{`filter={ starts key "left/" }`, []bool{false, false}},
		{`filter={ ends key "/1" }`, []bool{false, false}},
		{`filter={ contains key "left" }`, []bool{false, false}},
		// A record with no key is a record there is none of.
		{`filter={ lacks key }`, []bool{false, false}},
		// And every record has one.
		{`filter={ has key }`, []bool{true, true}},
	} {
		left, right := &spy{inner: mustPSL(t, leftDoc)}, &spy{inner: mustPSL(t, rightDoc)}
		set := composed(t,
			Include{Name: "left", Source: left},
			Include{Name: "right", Source: right})
		read(t, set, c.filter, "have=0 need=10")

		got := []bool{left.opened, right.opened}
		if got[0] != c.open[0] || got[1] != c.open[1] {
			t.Errorf("%s opened left=%v right=%v, want %v",
				c.filter, got[0], got[1], c.open)
		}
	}
}

// Reversed turns a composed sequence over entire: the includes back to front,
// and the records inside each of them too.
//
// It names no field, so what goes down to every include is `reversed` as well,
// and no include is ever asked about a key by name.
func TestAComposedSourceReverses(t *testing.T) {
	forward, _ := read(t, twoIncludes(t), "", "have=0 need=10")
	if forward.joined() != "(left/0),(left/1),(left/note),(right/0),(right/1)" {
		t.Fatalf("forward, the sequence is %s", forward.joined())
	}
	mirror, _ := read(t, twoIncludes(t), "reversed", "have=0 need=10")
	if mirror.joined() != "(right/1),(right/0),(left/note),(left/1),(left/0)" {
		t.Errorf("reversed, the sequence is %s", mirror.joined())
	}
	if !mirror.ordered {
		t.Error("a reversed sequence did not say it was in order")
	}
}

// Naming the key descending and reversing an ascending one are the same
// sequence, said two ways.
func TestReversedAndAKeyLevelAgree(t *testing.T) {
	for _, spec := range []string{"sort={ key desc }", "reversed", "sort={ key } reversed"} {
		out, _ := read(t, twoIncludes(t), spec, "have=0 need=10")
		if out.joined() != "(right/1),(right/0),(left/note),(left/1),(left/0)" {
			t.Errorf("%q gave %s", spec, out.joined())
		}
	}
	// And reversing a descending key level is the sequence as it stands.
	out, _ := read(t, twoIncludes(t), "sort={ key desc } reversed", "have=0 need=10")
	if out.joined() != "(left/0),(left/1),(left/note),(right/0),(right/1)" {
		t.Errorf("reversed twice gave %s", out.joined())
	}
}

// Under a sort of its own, the mirror is the whole sequence read backwards --
// every level turned over, the one that settles ties included.
func TestAComposedSourceReversesUnderASort(t *testing.T) {
	forward, _ := read(t, twoIncludes(t), "sort={ .size }", "have=0 need=10")
	if forward.joined() != "(left/note),(left/0),(right/0),(left/1),(right/1)" {
		t.Fatalf("forward, the sequence is %s", forward.joined())
	}

	left, right := &spy{inner: mustPSL(t, leftDoc)}, &spy{inner: mustPSL(t, rightDoc)}
	c := composed(t,
		Include{Name: "left", Source: left},
		Include{Name: "right", Source: right})

	mirror, _ := read(t, c, "sort={ .size } reversed", "have=0 need=10")
	if mirror.joined() != "(right/1),(left/1),(right/0),(left/0),(left/note)" {
		t.Errorf("reversed, the sequence is %s", mirror.joined())
	}
	// What is asked of an include is the mirror of what is wanted, and
	// reversing it lands on the sequence this source is after.
	for _, s := range []*spy{left, right} {
		if got := wire.EncodeSort(s.spec.Sort); got != "{ .size }" || !s.spec.Reversed {
			t.Errorf("an include was asked sort=%s reversed=%v", got, s.spec.Reversed)
		}
	}
}
