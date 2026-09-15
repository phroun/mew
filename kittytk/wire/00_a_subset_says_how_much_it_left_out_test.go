package wire

// How much of a record a subset left out, and how it says so.
//
// A subset used to answer the question that asked for it and no other. It now
// carries two counts -- how many members the record has by name, and how many
// by position -- so that whoever took it can work out what is missing, and
// sometimes work out that nothing is.
//
// `map` and `len` because those are already the two halves of a PSL node, which
// is what a record is read out of.

import (
	"testing"

	"github.com/phroun/serval"
)

// after is a result statement taken apart: the arguments past the query id,
// which is what ParseResult reads.
func after(t *testing.T, src string) []*Arg {
	t.Helper()
	script, err := Parse(src)
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	st := script.Statements[0]
	if len(st.Args) == 0 {
		t.Fatalf("%s: a result names the query it answers", src)
	}
	return st.Args[1:]
}

// The two counts cross, and come back as they went out.
func TestASubsetSaysHowManyMembersTheRecordHas(t *testing.T) {
	for _, c := range []struct {
		what string
		src  string
		want serval.Totals
	}{
		{"named members alone, which is the ordinary record",
			`result 1 id=17 fields={ .name "a" } map=5`,
			serval.Totals{Named: 5}},
		{"both, for a record with members standing by position",
			`result 1 id=17 fields={ .name "a" } map=5 len=3`,
			serval.Totals{Named: 5, Ordered: 3}},
		{"positions alone",
			`result 1 id=17 fields={ .0 "a" } len=3`,
			serval.Totals{Ordered: 3}},
		{"a record of no members at all, which says nothing",
			`result 1 id=17 fields={}`,
			serval.Totals{}},
	} {
		r, err := ParseResult(after(t, c.src))
		if err != nil {
			t.Errorf("%s: %v", c.what, err)
			continue
		}
		if r.Has != c.want {
			t.Errorf("%s: %s says %+v, want %+v", c.what, c.src, r.Has, c.want)
		}
		if r.Whole {
			t.Errorf("%s: %s came back whole", c.what, c.src)
		}
		// And out again, which is what a source relaying one does.
		out := EncodeStatement(&Statement{
			Verb: ResultVerb,
			Args: append([]*Arg{{Value: NewInt(1)}}, r.Args()...),
		})
		if out != c.src {
			t.Errorf("%s: %s went out as %s", c.what, c.src, out)
		}
	}
}

// A count of nothing is not written. Most records have no members standing by
// position, and saying so on every record of every answer would be noise.
func TestACountOfNothingIsNotWritten(t *testing.T) {
	r := &Result{
		ID:     NewInt(17),
		Fields: serval.Record{serval.Named(".name", "a")},
		Has:    serval.Totals{Named: 5},
	}
	out := EncodeStatement(&Statement{
		Verb: ResultVerb,
		Args: append([]*Arg{{Value: NewInt(1)}}, r.Args()...),
	})
	if want := `result 1 id=17 fields={ .name "a" } map=5`; out != want {
		t.Errorf("it wrote %s, want %s", out, want)
	}
}

// A WHOLE record is its own totals: what it carries is everything there is. So
// a count beside one is either saying that again or contradicting it, and there
// is no reading where it adds anything.
func TestAWholeRecordStatesNoTotals(t *testing.T) {
	// It writes none.
	r := &Result{
		ID:     NewInt(17),
		Fields: serval.Record{serval.Named(".name", "a")},
		Whole:  true,
		Has:    serval.Totals{Named: 5, Ordered: 3},
	}
	out := EncodeStatement(&Statement{
		Verb: ResultVerb,
		Args: append([]*Arg{{Value: NewInt(1)}}, r.Args()...),
	})
	if want := `result 1 id=17 record={ .name "a" }`; out != want {
		t.Errorf("a whole record wrote %s, want %s", out, want)
	}

	// And it refuses to read one.
	for _, src := range []string{
		`result 1 id=17 record={ .name "a" } map=5`,
		`result 1 id=17 record={ .name "a" } len=3`,
	} {
		if got, err := ParseResult(after(t, src)); err == nil {
			t.Errorf("%s was taken as %+v", src, got.Has)
		}
	}
}

// A count is a number of members, and there is no such thing as fewer than
// none.
func TestACountIsAWholeNumberOfMembers(t *testing.T) {
	for _, src := range []string{
		`result 1 id=17 fields={ .name "a" } map=x`,
		`result 1 id=17 fields={ .name "a" } map=-1`,
		`result 1 id=17 fields={ .name "a" } map=2.5`,
		`result 1 id=17 fields={ .name "a" } map`,
		`result 1 id=17 fields={ .name "a" } len={ 3 }`,
	} {
		if got, err := ParseResult(after(t, src)); err == nil {
			t.Errorf("%s was taken as %+v", src, got.Has)
		}
	}
}

// A field the record has not got crosses as a name with nothing under it, which
// is a guarantee rather than a silence -- and it is not counted, being
// knowledge about the record and not a member of it.
func TestAnAbsenceCrossesAsANameWithNothingUnderIt(t *testing.T) {
	src := `result 1 id=17 fields={ .name "a"; .thumbnail } map=5`
	r, err := ParseResult(after(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Fields.Has(".thumbnail") {
		t.Errorf("the absence was dropped: %s", r.Fields)
	}
	if r.Fields.Get(".thumbnail") != nil {
		t.Errorf("the absence came back carrying something: %s", r.Fields)
	}
	if got := serval.Tally(r.Fields); got != (serval.Totals{Named: 1}) {
		t.Errorf("one member and one absence tally as %+v", got)
	}
	if r.Has.Named != 5 {
		t.Errorf("the record says it has %d named members", r.Has.Named)
	}
}
