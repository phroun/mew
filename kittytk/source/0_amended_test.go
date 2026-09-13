package source

// Amending another source's records.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/wire"
)

// four records, by size: go.mod 96, build.sh 310, README.md 2048, parser.go 14022
func amendable(t *testing.T) *Amended {
	t.Helper()
	return NewAmended(mustPSL(t, twoWays))
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

	out, _ := read(t, a, "sort={ .size }", "have=0 need=4")
	if out.joined() != "2,1,0,3" {
		t.Fatalf("the sequence is %s", out.joined())
	}
	if got := out.fields[0].Encode(); got != `{ .name "go.mod"; .size 96 }` {
		t.Errorf("the replacement carries %s", got)
	}
}

// And its own values place it, not the ones the child holds.
func TestAReplacementIsPlacedByItsOwnValues(t *testing.T) {
	a := amendable(t)
	a.Replace(key(2), fields("go.mod", 99999)) // was 96, the smallest

	out, _ := read(t, a, "sort={ .size }", "have=0 need=4")
	if out.joined() != "1,0,3,2" {
		t.Errorf("the sequence is %s", out.joined())
	}
}

// A deletion takes the record out, and the scope is still as long as it was
// asked for: the child was asked for enough to cover what would be removed.
func TestADeletionIsCoveredBeforeTheChildIsAsked(t *testing.T) {
	a := amendable(t)
	a.Delete(key(1), fields("build.sh", 310))

	out, done := read(t, a, "sort={ .size }", "have=0 need=3")
	if out.joined() != "2,0,3" {
		t.Errorf("the sequence is %s", out.joined())
	}
	if !done.Exhausted {
		t.Error("the end of the sequence did not say so")
	}
}

// A deletion this source knows nothing about is discovered rather than
// predicted: the scope comes up short, the child is asked again, and what
// the round taught means the next one over the same ground does not.
func TestADeletionWithNoPlacementIsLearned(t *testing.T) {
	a := amendable(t)
	a.Delete(key(1), nil) // nothing known about where it sat

	out, _ := read(t, a, "sort={ .size }", "have=0 need=3")
	if out.joined() != "2,0,3" {
		t.Fatalf("the sequence is %s", out.joined())
	}

	// It knows now.
	am := a.lookup(key(1))
	if am == nil || am.seen == nil {
		t.Fatalf("nothing was learned: %#v", am)
	}
	if got := am.seen.Encode(); got != `{ .name "build.sh"; .size 310 }` {
		t.Errorf("what it learned is %s", got)
	}
}

// A replacement the filter no longer admits is a record gone, exactly as a
// deletion is, and it is covered the same way.
func TestAReplacementFilteredOutIsARecordGone(t *testing.T) {
	a := amendable(t)
	a.Replace(key(1), fields("build.sh", 99999)) // the filter below wants under 3000

	out, _ := read(t, a, "filter={ lt .size 3000 } sort={ .size }", "have=0 need=2")
	if out.joined() != "2,0" {
		t.Errorf("the sequence is %s", out.joined())
	}
}

// A record this source holds that the child never sends goes out anyway. Here
// the child cannot send it, because by the child's values it is filtered out.
func TestARecordTheChildNeverSendsStillGoesOut(t *testing.T) {
	a := amendable(t)
	a.Replace(key(3), fields("parser.go", 100)) // was 14022, the filter excludes it

	out, _ := read(t, a, "filter={ lt .size 3000 } sort={ .size }", "have=0 need=4")
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
	if err := set.Fill(parseFill(t, "have=0 need=2"), first); err != nil {
		t.Fatal(err)
	}
	if first.joined() != "2,1" {
		t.Fatalf("the first scope is %s", first.joined())
	}

	a.Delete(key(1), fields("build.sh", 310))

	second := &collector{}
	if err := set.Fill(parseFill(t, "have=0 need=2"), second); err != nil {
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

	out, _ := read(t, a, "sort={ .size }", "have=0 need=1")
	if out.joined() != "2" {
		t.Errorf("the first record is %s", out.joined())
	}
	if got := out.fields[0].Encode(); got != `{ .name "go.mod"; .size 96 }` {
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
		{"another amended source", NewAmended(mustPSL(t, twoWays))},
	} {
		t.Run(kind.what, func(t *testing.T) {
			a := NewAmended(kind.child)
			a.Replace(key(2), fields("go.mod", 99999))
			a.Delete(key(1), fields("build.sh", 310))

			out, _ := read(t, a, "sort={ .size }", "have=0 need=3")
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

	out, _ := read(t, a, "sort={ .size }", "have=0 need=4")
	if !out.ordered {
		t.Error("an ordered child did not make an ordered answer")
	}
	if out.joined() != "2,1,0,3" {
		t.Errorf("the sequence is %s", out.joined())
	}
}

func TestItClaimsOrderOnlyWhenTheChildDid(t *testing.T) {
	a := NewAmended(&jumbled{inner: mustPSL(t, twoWays)})
	a.Replace(key(2), fields("go.mod", 96))

	out, _ := read(t, a, "sort={ .size }", "have=0 need=4")
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

func (j *jumbled) Open(spec *wire.Spec) (ResultSet, error) {
	set, err := j.inner.Open(spec)
	if err != nil {
		return nil, err
	}
	return &jumbledSet{inner: set}, nil
}

type jumbledSet struct{ inner ResultSet }

func (j *jumbledSet) Close() { j.inner.Close() }
func (j *jumbledSet) Fill(f *wire.Fill, out Sink) error {
	return j.inner.Fill(f, &unordered{out: out})
}

type unordered struct{ out Sink }

func (u *unordered) Ordered()                                  {} // said nothing, which is what a jumble says
func (u *unordered) Record(k *wire.Value, f wire.Fields) error { return u.out.Record(k, f) }
func (u *unordered) Subset(k *wire.Value, f wire.Fields) error { return u.out.Subset(k, f) }
func (u *unordered) Done(c Complete)                           { u.out.Done(c) }

// A child that refuses ends the scope here too, rather than leaving whoever
// asked waiting.
func TestAChildThatRefusesEndsTheScope(t *testing.T) {
	a := NewAmended(NewHosted("files", func(string) error { return errBroken{} }))
	set, err := a.Open(parseSpec(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	out := &collector{}
	if err := set.Fill(parseFill(t, "have=0 need=2"), out); err == nil {
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

	out, _ := read(t, a, "sort={ .size }",
		`from={ .size 96; key 2 } have=0 need=2`)
	if out.joined() != "1,3" {
		t.Errorf("the scope is %s", out.joined())
	}

	// And what the round taught means the next one does not come up short.
	am := a.lookup(key(0))
	if am == nil || am.seen == nil {
		t.Fatalf("nothing was learned: %#v", am)
	}
	if got := am.seen.Encode(); got != `{ .name "README.md"; .size 2048 }` {
		t.Errorf("what it learned is %s", got)
	}
}

// The child having nothing more is the end of the sequence, this source's own
// records having all gone out by then.
func TestTheEndOfTheSequenceIsTheChildsEnd(t *testing.T) {
	a := amendable(t)
	a.Replace(key(9), fields("added-by-nobody", 99999))

	out, done := read(t, a, "sort={ .size }", "have=0 need=99")
	if !done.Exhausted {
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
	a := NewAmended(child)

	out, done := read(t, a, "", "have=0 need=100")
	if child.rounds < 2 {
		t.Errorf("it gave up after %d round(s)", child.rounds)
	}
	if child.rounds > 8 {
		t.Errorf("it asked %d times", child.rounds)
	}
	if done.Exhausted {
		t.Error("a scope that never filled claimed to be exhausted")
	}
	if len(out.keys) != child.rounds {
		t.Errorf("%d records from %d rounds", len(out.keys), child.rounds)
	}
}

// dribble answers one record at a time and never says it has run out, which is
// a source that will never fill a scope however often it is asked.
type dribble struct{ rounds int }

func (d *dribble) Open(*wire.Spec) (ResultSet, error) { return d, nil }
func (d *dribble) Close()                             {}

func (d *dribble) Fill(f *wire.Fill, out Sink) error {
	d.rounds++
	out.Ordered()
	k := wire.NewInt(int64(d.rounds))
	_ = out.Record(k, wire.Fields{wire.Named(".n", int64(d.rounds))})
	out.Done(Complete{Watermark: wire.Fields{wire.Named(wire.KeyField, int64(d.rounds))}})
	return nil
}
