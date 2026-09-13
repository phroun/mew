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

// The key a composed source hands out is not one any include holds, so a
// sequence that orders or filters by it is refused rather than answered
// nearly.
func TestASequenceNamingTheKeyIsRefused(t *testing.T) {
	c := twoIncludes(t)
	for _, spec := range []string{
		"sort={ key }",
		"sort={ .size; key desc }",
		`filter={ eq key (left/0) }`,
		`filter={ not { starts key "left" } }`,
	} {
		if _, err := c.Open(parseSpec(t, spec)); err == nil {
			t.Errorf("%s was accepted", spec)
		}
	}
	// And one that names no key opens.
	if _, err := c.Open(parseSpec(t, "sort={ .size } filter={ lt .size 100 }")); err != nil {
		t.Errorf("a sequence naming no key was refused: %v", err)
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
	inner Source
	asked *wire.Fill
}

func (s *spy) Open(spec *wire.Spec) (ResultSet, error) {
	set, err := s.inner.Open(spec)
	if err != nil {
		return nil, err
	}
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
