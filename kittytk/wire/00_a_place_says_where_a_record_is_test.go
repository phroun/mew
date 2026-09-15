package wire

// A place: where a record stands, and whatever is known of it so far.
//
// The verb is the whole mechanism. A reader that does not know it skips these
// statements and is left with exactly the answer it would have got, so nothing
// here is negotiated and nothing can be misled by it.

import (
	"strings"
	"testing"

	"github.com/phroun/serval"
)

// said renders one statement of a query's answer, under whichever verb.
func said(t *testing.T, verb string, r *Result) string {
	t.Helper()
	args := append([]*Arg{{Value: NewInt(9)}}, r.Args()...)
	return EncodeStatement(&Statement{Verb: verb, Args: args})
}

// read takes one back apart, from the arguments after the query id.
func read(t *testing.T, text string) (*Result, error) {
	t.Helper()
	script, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(script.Statements) != 1 {
		t.Fatalf("%q is %d statements", text, len(script.Statements))
	}
	st := script.Statements[0]
	if st.Verb == PlaceVerb {
		return ParsePlace(st.Args[1:])
	}
	return ParseResult(st.Args[1:])
}

// A place carries an identity and whatever fields are to hand, under a verb of
// its own.
func TestAPlaceCrossesUnderItsOwnVerb(t *testing.T) {
	got := said(t, PlaceVerb, &Result{
		Place:  true,
		ID:     NewInt(42),
		Fields: serval.Record{serval.Named("name", "src/parser.go")},
	})
	if want := `place 9 id=42 fields={ name "src/parser.go" }`; got != want {
		t.Errorf("it wrote %s", got)
	}

	back, err := read(t, got)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Place || back.Whole || back.ID.Int != 42 {
		t.Errorf("it read back as %+v", back)
	}
	if n := back.Fields.Get("name"); n == nil || n.Str != "src/parser.go" {
		t.Errorf("the fields came back as %s", back.Fields)
	}
}

// A place with nothing in it is still a place: where the record stands is the
// whole of what it had to say.
func TestAPlaceMayCarryNothingAtAll(t *testing.T) {
	got := said(t, PlaceVerb, &Result{Place: true, ID: NewInt(42)})
	if want := "place 9 id=42 fields={}"; got != want {
		t.Errorf("it wrote %s", got)
	}
	if back, err := read(t, got); err != nil || len(back.Fields) != 0 {
		t.Errorf("it read back as %+v, %v", back, err)
	}
}

// Not making a claim about how much of the record there is IS the difference
// between a place and a result, so a place that made one would be a result
// under the wrong verb.
func TestAPlaceSaysNothingAboutHowMuchThereIs(t *testing.T) {
	for _, text := range []string{
		`place 9 id=42 fields={ name "x" } map=5`,
		`place 9 id=42 fields={ name "x" } len=3`,
		`place 9 id=42 record={ name "x" }`,
	} {
		if _, err := read(t, text); err == nil {
			t.Errorf("%s was read as a place", text)
		}
	}
}

// The completion riding a place ends the ORDER: every record has now been
// named, and no further one will turn up between two already sent.
func TestAPlaceSettlesTheOrderWithoutEndingTheScope(t *testing.T) {
	got := said(t, PlaceVerb, &Result{Place: true, Complete: &serval.Complete{
		Stop: serval.StopFilled, Watermark: serval.NewInt(42),
	}})
	if want := "place 9 complete watermark=42 filled"; got != want {
		t.Errorf("it wrote %s", got)
	}

	back, err := read(t, got)
	if err != nil {
		t.Fatal(err)
	}
	if back.Complete == nil || back.Complete.Stop != serval.StopFilled {
		t.Fatalf("it read back as %+v", back.Complete)
	}
	// Which is the same claim the terminator will carry, said sooner.
	if !serval.Equal(back.Complete.Watermark, serval.NewInt(42)) {
		t.Errorf("the order was settled up to %s", back.Complete.Watermark)
	}
}

// --- how many there are ---------------------------------------------------

// Weak by default and strengthened out loud, the same way round as `fields`
// against `record`.
func TestATotalIsAFloorUnlessItSaysOtherwise(t *testing.T) {
	for _, c := range []struct {
		serval.RecordCount
		want string
	}{
		{serval.AtLeast(20), "result 9 complete exhausted total=20"},
		{serval.Exactly(20), "result 9 complete exhausted total=20 exact"},
		// A sequence counted and found empty is worth saying; a floor of none
		// is what is true of every sequence there is, and is not written.
		{serval.Exactly(0), "result 9 complete exhausted total=0 exact"},
		{serval.Unknown(), "result 9 complete exhausted"},
	} {
		got := said(t, ResultVerb, &Result{Complete: &serval.Complete{
			Stop: serval.StopExhausted, Total: c.RecordCount,
		}})
		if got != c.want {
			t.Errorf("%s wrote %s", c.RecordCount, got)
		}
		back, err := read(t, got)
		if err != nil {
			t.Fatal(err)
		}
		if back.Complete.Total != c.RecordCount {
			t.Errorf("%s came back as %s", c.RecordCount, back.Complete.Total)
		}
	}
}

// A total rides a completion, so `exact` with nothing to be exact about is a
// word with no figure under it.
func TestExactWithNoTotalIsRefused(t *testing.T) {
	if _, err := read(t, `result 9 id=42 record={ name "x" } exact`); err == nil {
		t.Error("exact was read with nothing stating a total")
	}
}

// A negative total is no more a count of records than a negative `map` is a
// count of members: there is no such thing as fewer than none.
func TestATotalIsNeverNegative(t *testing.T) {
	if _, err := read(t, "result 9 complete total=-1"); err == nil {
		t.Error("a sequence was read as holding fewer than no records")
	}
}

// And it rides either completion, being the order's fact in both cases.
func TestATotalRidesEitherCompletion(t *testing.T) {
	got := said(t, PlaceVerb, &Result{Place: true, Complete: &serval.Complete{
		Stop: serval.StopExhausted, Total: serval.Exactly(3),
	}})
	if !strings.Contains(got, "total=3 exact") {
		t.Errorf("the order's completion wrote %s", got)
	}
}
