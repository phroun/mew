package source

// Reading a PSL list as records, and drawing scopes out of it.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/wire"
)

// A document with both of a PSL list's collections in it, records of differing
// shapes, and a record that is not a list at all.
const doc = `(
  ("README.md", size: 2048),
  ("build.sh", size: 310),
  ("go.mod", size: 96),
  ("src/parser.go", size: 14022),
  extra: ("notes.txt", size: 12),
  plain: "just a string"
)`

func open(t *testing.T, text, spec string) DataSet { return openAs(t, Whole, text, spec) }

func openAs(t *testing.T, reading Reading, text, spec string) DataSet {
	t.Helper()
	src, err := ParsePSLSource(text, reading)
	if err != nil {
		t.Fatal(err)
	}
	v, err := src.Open(parseSpec(t, spec))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// parseSpec reads a spec the way one arrives: off the arguments of a statement.
func parseSpec(t *testing.T, args string) *wire.Spec {
	t.Helper()
	spec, err := wire.ParseSpec(statement(t, "new query "+args).Args[1:])
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func parseScope(t *testing.T, args string) *wire.Scope {
	t.Helper()
	f, err := wire.ParseScope(statement(t, "new query "+args).Args[1:])
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func statement(t *testing.T, text string) *wire.Statement {
	t.Helper()
	script, err := wire.Parse(text)
	if err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	return script.Statements[0]
}

// collector is a Sink that keeps what it was given, and how much of each
// record it was told had come back.
type collector struct {
	keys    []string
	fields  []wire.Fields
	whole   []bool
	done    Complete
	ended   bool
	ordered bool
}

// Ordered arrives before the records, so a sink that is told late is a sink
// that was told wrong.
func (c *collector) Ordered() {
	if len(c.keys) > 0 || c.ended {
		panic("the order was declared after the records it describes")
	}
	c.ordered = true
}

func (c *collector) Record(key *wire.Value, fields wire.Fields) error {
	return c.took(key, fields, true)
}

func (c *collector) Subset(key *wire.Value, fields wire.Fields) error {
	return c.took(key, fields, false)
}

func (c *collector) took(key *wire.Value, fields wire.Fields, whole bool) error {
	c.keys = append(c.keys, wire.EncodeValue(key))
	c.fields = append(c.fields, fields)
	c.whole = append(c.whole, whole)
	return nil
}

func (c *collector) Done(done Complete) { c.done, c.ended = done, true }

func (c *collector) joined() string { return strings.Join(c.keys, ",") }

// fill draws one scope and reports the keys that came out of it.
func fill(t *testing.T, v DataSet, args string) (*collector, Complete) {
	t.Helper()
	out := &collector{}
	if err := v.Read(parseScope(t, args), out); err != nil {
		t.Fatal(err)
	}
	if !out.ended {
		t.Fatal("the scope was never ended")
	}
	return out, out.done
}

// The ordered items and the keyed members are two collections, and they come
// out as one sequence because an integer ranks below a string: the items in
// their order, then the names in theirs. No rule of its own says so.
func TestBothOfAPSLListsCollectionsAreRecords(t *testing.T) {
	v := open(t, doc, "")
	out, done := fill(t, v, "count=10")
	if out.joined() != `0,1,2,3,"extra","plain"` {
		t.Errorf("the sequence is %s", out.joined())
	}
	if done.Stop != wire.StopExhausted {
		t.Error("a scope holding every record did not say so")
	}
	if !out.ordered {
		t.Error("the records went out in the sequence's order and did not say so")
	}
}

// A record's fields are its own members, each under a dot: the items at their
// positions, the keyed members by name.
func TestARecordsFieldsAreItsOwnMembers(t *testing.T) {
	v := open(t, doc, "")
	out, _ := fill(t, v, "count=1")
	got := out.fields[0].Encode()
	if got != `{ key 0; .0 "README.md"; .size 2048 }` {
		t.Errorf("the first record carries %s", got)
	}
}

// A record that is not a list has a key and a value and no members -- so a
// catalogue of plain strings is filterable and sortable rather than being
// nothing but keys.
func TestARecordThatIsNotAListIsAKeyAndAValue(t *testing.T) {
	v := open(t, doc, `filter={ eq key "plain" }`)
	out, _ := fill(t, v, "count=10")
	if len(out.fields) != 1 {
		t.Fatalf("%d records matched the key", len(out.fields))
	}
	if got := out.fields[0].Encode(); got != `{ key "plain"; value "just a string" }` {
		t.Errorf("a bare value carries %s", got)
	}
}

// A field one record has and another has not is undefined rather than an
// error, which is the whole of what a source of mixed records needs.
func TestAFieldARecordHasNotGotIsUndefined(t *testing.T) {
	v := open(t, doc, "filter={ eq .size undefined }")
	out, _ := fill(t, v, "count=10")
	if out.joined() != `"plain"` {
		t.Errorf("the records with no size are %s", out.joined())
	}
}

// The sort and the filter are the query's, and the key is the last level.
//
// `plain` is in the answer and has no size at all. That is the rank rule doing
// what it says: undefined sits below every number, so a record with no size is
// smaller than one, and `lt` says so. A filter that means "has a size, and it
// is under a thousand" is two predicates and says both.
func TestASortedFilteredScope(t *testing.T) {
	v := open(t, doc, `filter={ lt .size 1000 } sort={ .size desc }`)
	out, done := fill(t, v, "count=10")
	if out.joined() != `1,2,"extra","plain"` {
		t.Errorf("the sequence is %s", out.joined())
	}
	if done.Stop != wire.StopExhausted {
		t.Error("the whole sequence did not say it was exhausted")
	}
}

// A scope is as long as it was asked for, and what ends it says where it got
// to -- the sort fields and the key, which is what makes the boundary name
// exactly one position.
func TestAScopeStopsAndSaysWhereItGotTo(t *testing.T) {
	// `plain` is a bare string and has no first item, so it leads: undefined is
	// the bottom of the order, and it is one position like any other.
	v := open(t, doc, "sort={ .0 natural }")
	out, done := fill(t, v, "count=2")
	if out.joined() != `"plain",1` {
		t.Errorf("the first scope is %s", out.joined())
	}
	if done.Stop == wire.StopExhausted {
		t.Error("a scope with records past it said it was exhausted")
	}
	if got := wire.EncodeValue(done.Watermark); got != `1` {
		t.Errorf("the watermark is %s", got)
	}

	// And the next scope starts after it.
	next, _ := fill(t, v, "after="+wire.EncodeValue(done.Watermark)+" count=2")
	if next.joined() != `2,"extra"` {
		t.Errorf("the second scope is %s", next.joined())
	}
}

// A scope of no records at all costs nothing and claims nothing beyond where
// it was asked from.
func TestAScopeOfNoneSendsNothing(t *testing.T) {
	v := open(t, doc, "")
	out, done := fill(t, v, "count=0")
	if len(out.keys) != 0 {
		t.Errorf("a scope of none was sent %d record(s)", len(out.keys))
	}
	if done.Stop == wire.StopExhausted {
		t.Error("a scope that sent nothing claimed the sequence was over")
	}
}

// `until` is the far end saying where its own knowledge picks up again. The
// walk stops there rather than at the count, and says so -- which is what tells
// the display the two runs it holds are now one.
func TestAWalkThatReachesUntilSaysItJoined(t *testing.T) {
	v := open(t, doc, "")
	out, done := fill(t, v, "until=3 count=10")
	if out.joined() != `0,1,2` {
		t.Errorf("the records before it are %s", out.joined())
	}
	if done.Stop != wire.StopJoined {
		t.Errorf("it stopped at the record the asker held and said %q", done.Stop)
	}
}

// An `until` this sequence does not hold says nothing and stops nothing: the
// walk runs to its count instead.
func TestAnUntilThatIsNotHereIsNotARefusal(t *testing.T) {
	v := open(t, doc, "")
	out, done := fill(t, v, `until="nobody" count=2`)
	if len(out.keys) != 2 || done.Stop != wire.StopFilled {
		t.Errorf("it came back %v, %q", out.keys, done.Stop)
	}
}

// An `after` this sequence does not hold is refused.
//
// It cannot be placed -- what put a record somewhere are its own values, and
// they belong to the record -- and answering from somewhere else would hand
// back a run the asker did not ask for, with nothing marking it as the wrong
// one.
func TestAnAfterThatIsNotHereIsRefused(t *testing.T) {
	v := open(t, doc, "")
	_, done := fill(t, v, `after="nobody" count=2`)
	if done.Error == "" {
		t.Error("a scope starting from a record that is not here was answered anyway")
	}
}

// A boundary is found rather than walked to: the ordering is total, so the
// position after it is a binary search. Nothing observable says so except that
// the answer is right from any point in a long sequence.
func TestAScopeStartsAfterABoundaryAnywhereInTheSequence(t *testing.T) {
	var b strings.Builder
	b.WriteString("(")
	for i := 0; i < 500; i++ {
		b.WriteString("(n: ")
		b.WriteString(itoa(i))
		b.WriteString("),")
	}
	b.WriteString(")")

	v := open(t, b.String(), "sort={ .n }")
	out, done := fill(t, v, "after=399 count=3")
	if out.joined() != "400,401,402" {
		t.Errorf("the scope after 399 is %s", out.joined())
	}
	if got := wire.EncodeValue(done.Watermark); got != "402" {
		t.Errorf("the watermark is %s", got)
	}
}

// A query names the fields it wants where they are fewer than the record has,
// which is how a display asks for the skeleton of a wide sequence.
func TestAQueryAsksForFewerFieldsThanTheRecordHas(t *testing.T) {
	v := open(t, doc, "fields={ .size }")
	out, _ := fill(t, v, "count=1")
	if got := out.fields[0].Encode(); got != "{ .size 2048 }" {
		t.Errorf("the record carries %s", got)
	}
}

// A different sequence is a different data set, opened alongside the one it
// replaces and closed after it -- which is what keeps the source in use while
// the reader moves across.
func TestADifferentSequenceIsADifferentDataSet(t *testing.T) {
	src, err := ParsePSLSource(doc, Whole)
	if err != nil {
		t.Fatal(err)
	}
	byName, err := src.Open(parseSpec(t, "sort={ .0 natural }"))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := fill(t, byName, "count=1")
	if out.joined() != `"plain"` {
		t.Errorf("by name the first record is %s", out.joined())
	}

	bySize, err := src.Open(parseSpec(t, "sort={ .size desc }"))
	if err != nil {
		t.Fatal(err)
	}
	byName.Close()
	out, _ = fill(t, bySize, "count=1")
	if out.joined() != "3" {
		t.Errorf("by size the first record is %s", out.joined())
	}
}

// A data set is refused rather than opened wrong. An order that is quietly a
// little different corrupts every answer after it and looks like data.
func TestASortNobodyCanProduceIsRefused(t *testing.T) {
	src, err := ParsePSLSource(doc, Whole)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.Open(parseSpec(t, "sort={ .name collate=turkish }")); err == nil {
		t.Error("a collation nobody carries was accepted")
	}
}

// The same sequence is ordered once. Two data sets over it, and one opened
// again on an order somebody had before, draw on the ordering already built.
func TestOneSequenceIsOrderedOnce(t *testing.T) {
	src, err := ParsePSLSource(doc, Whole)
	if err != nil {
		t.Fatal(err)
	}
	spec := parseSpec(t, "sort={ .size }")
	a, err := src.Open(spec)
	if err != nil {
		t.Fatal(err)
	}
	b, err := src.Open(parseSpec(t, "sort={ .size }"))
	if err != nil {
		t.Fatal(err)
	}
	if a.(*pslDataSet).ord != b.(*pslDataSet).ord {
		t.Error("two data sets over one sequence built it twice")
	}

	// And an order pushed out by newer ones is built again rather than wrong.
	for _, s := range []string{"sort={ .0 }", "sort={ key desc }", "sort={ .size desc }", "sort={ .0 desc }"} {
		if _, err := src.Open(parseSpec(t, s)); err != nil {
			t.Fatal(err)
		}
	}
	c, err := src.Open(spec)
	if err != nil {
		t.Fatal(err)
	}
	if c.(*pslDataSet).ord == a.(*pslDataSet).ord {
		t.Error("the cache grew without limit")
	}
	out, _ := fill(t, c, "count=10")
	if out.joined() != `"plain","extra",2,1,0,3` {
		t.Errorf("the rebuilt ordering is %s", out.joined())
	}
}

// A nested list has no order of its own, so it takes the unordered rank and
// crosses as a block of its own members -- the shape a record's fields take,
// which is what it is.
func TestANestedListCrossesAsItsOwnMembers(t *testing.T) {
	v := open(t, `( (name: "figaro", tags: ("red", "blue", weight: 3)) )`, "")
	out, _ := fill(t, v, "count=1")
	want := `{ key 0; .name "figaro"; .tags { key; .0 "red"; .1 "blue"; .weight 3 } }`
	if got := out.fields[0].Encode(); got != want {
		t.Errorf("the record carries %s", got)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [8]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}

// --- the shorter reading ------------------------------------------------

// Members names the members alone, which is the reading for a source whose
// records are all lists of named fields.
func TestMembersNamesTheMembersAlone(t *testing.T) {
	const table = `(
  (name: "README.md", size: 2048),
  (name: "build.sh", size: 310),
  (name: "go.mod", size: 96)
)`
	v := openAs(t, Members, table, "sort={ size desc }")
	out, done := fill(t, v, "count=2")
	if out.joined() != "0,1" {
		t.Errorf("by size the scope is %s", out.joined())
	}
	if got := out.fields[0].Encode(); got != `{ name "README.md"; size 2048 }` {
		t.Errorf("the first record carries %s", got)
	}
	if got := wire.EncodeValue(done.Watermark); got != `1` {
		t.Errorf("the watermark is %s", got)
	}
}

// Under Members there is no way to name the record itself. `key` and `value`
// are members like any other -- present if the document wrote them, undefined
// if it did not -- and neither is the record's own.
func TestMembersCannotNameTheRecordItself(t *testing.T) {
	v := openAs(t, Members, doc, "filter={ ne key undefined }")
	out, _ := fill(t, v, "count=10")
	if len(out.keys) != 0 {
		t.Errorf("`key` reached something under Members: %s", out.joined())
	}

	// A record that is not a list has no members, so it carries nothing.
	v = openAs(t, Members, doc, "")
	out, _ = fill(t, v, "count=10")
	if got := out.fields[len(out.fields)-1].Encode(); got != "{}" {
		t.Errorf("a bare value carries %s under Members", got)
	}
}

// A member called `key` -- which the bundle format's own metadata has -- is
// reachable under both readings, because it is data and not an identity.
//
// Under Whole it wears a dot like every other member, and `key` without one is
// the record's own key as a field. Under Members it is simply the member, under
// its own name. Neither reading hides it: what names the record travels beside
// the bag, so there is nothing here for a member to collide with.
func TestBothReadingsReachAMemberCalledKey(t *testing.T) {
	const meta = `( _bundle: (key: "figaro", author: "Jeffrey R. Day") )`

	v := open(t, meta, `filter={ eq .key "figaro" }`)
	out, _ := fill(t, v, "count=10")
	if out.joined() != `"_bundle"` {
		t.Errorf("under Whole the member called key found %s", out.joined())
	}

	v = openAs(t, Members, meta, `filter={ eq key "figaro" }`)
	out, _ = fill(t, v, "count=10")
	if out.joined() != `"_bundle"` {
		t.Errorf("under Members the member called key found %s", out.joined())
	}
	if got := out.fields[0].Encode(); got != `{ author "Jeffrey R. Day"; key "figaro" }` {
		t.Errorf("under Members the record carries %s", got)
	}
}

// Under Whole a record carries its own key and value as fields, beside the
// members.
//
// They are a convenience, not the identity: sort on them, show them in a
// column, filter by them. What this end works by never appears in the bag at
// all, which is what lets these two be ordinary.
func TestWholeCarriesTheKeyAndValueAsFields(t *testing.T) {
	v := open(t, `( alpha: (size: 10) )`, "")
	out, _ := fill(t, v, "count=10")
	got := out.fields[0].Encode()
	if !strings.Contains(got, `key "alpha"`) || !strings.Contains(got, ".size 10") {
		t.Errorf("under Whole the record carries %s", got)
	}
}

// Under Whole the dot is the whole of what says a name is a member, so a name
// without one reaches nothing -- including when the letters after where the dot
// would have been happen to name a member.
func TestUnderWholeANameWithoutADotIsNotAMember(t *testing.T) {
	const trap = `( (ame: "trap", name: "real") )`

	v := open(t, trap, `filter={ eq name "trap" }`)
	out, _ := fill(t, v, "count=10")
	if len(out.keys) != 0 {
		t.Errorf("an undotted name reached a member: %s", out.joined())
	}

	v = open(t, trap, `filter={ eq .name "real" }`)
	out, _ = fill(t, v, "count=10")
	if out.joined() != "0" {
		t.Errorf("the dotted name found %s", out.joined())
	}
}

// --- a bare word is a symbol --------------------------------------------

// PSL writes a bare word and a quoted string differently, and so does the
// wire: a symbol ranks between a number and a string, is compared exactly, and
// takes no collation. So the two spellings reach a filter as the two different
// questions they were written as.
func TestABareWordIsASymbolAndAQuotedOneIsAString(t *testing.T) {
	const kinds = `(
  (name: "a", kind: text),
  (name: "b", kind: "text")
)`
	v := open(t, kinds, "filter={ eq .kind text }")
	out, _ := fill(t, v, "count=10")
	if out.joined() != "0" {
		t.Errorf("the word text found %s", out.joined())
	}

	v = open(t, kinds, `filter={ eq .kind "text" }`)
	out, _ = fill(t, v, "count=10")
	if out.joined() != "1" {
		t.Errorf("the string text found %s", out.joined())
	}

	// And a symbol goes out as one, which is what the far end reads back.
	v = open(t, kinds, "")
	out, _ = fill(t, v, "count=1")
	if got := out.fields[0].Encode(); got != `{ key 0; .kind text; .name "a" }` {
		t.Errorf("the record carries %s", got)
	}
}

// A bare token is a number or a symbol and is told apart by what it says, so
// most of what PSL calls a symbol is a symbol here too: a hyphen, a leading
// digit and a date all cross as themselves.
//
// And the rest crosses as a symbol as well, bracketed rather than bare. Nothing
// turns into a string on the way, so a bundle address stays an address.
func TestEverySymbolCrossesAsASymbol(t *testing.T) {
	v := open(t, `( (ok: plain, digits: 1x, dashed: kebab-case, dated: 2026-09-13,
	                starred: *star, addr: objectLibrary/figaro/3) )`, "")
	out, _ := fill(t, v, "count=1")

	got := out.fields[0].Encode()
	want := `{ key 0; .addr (objectLibrary/figaro/3); .dashed kebab-case; ` +
		`.dated 2026-09-13; .digits 1x; .ok plain; .starred (*star) }`
	if got != want {
		t.Errorf("the record carries %s", got)
	}

	// And every one of them reads back as the symbol it was sent as.
	script, err := wire.Parse("result 1 record=" + got)
	if err != nil {
		t.Fatalf("what went out does not read back: %v", err)
	}
	back, err := wire.ParseFields(script.Statements[0].Args[1].Value)
	if err != nil {
		t.Fatal(err)
	}
	for name, word := range map[string]string{
		".dated":   "2026-09-13",
		".starred": "*star",
		".addr":    "objectLibrary/figaro/3",
	} {
		v := back.Get(name)
		if v == nil || v.Kind != wire.WordValue || v.Word != word {
			t.Errorf("%s read back as %#v", name, v)
		}
	}
}

// `undefined` says the same thing in both languages, so it crosses as the word
// rather than as an identifier that happens to be spelled that way.
func TestTheWordUndefinedCrossesAsUndefined(t *testing.T) {
	v := open(t, `( (thumbnail: undefined), (thumbnail: "x") )`, "filter={ eq .thumbnail undefined }")
	out, _ := fill(t, v, "count=10")
	if out.joined() != "0" {
		t.Errorf("undefined found %s", out.joined())
	}
}

// Reversed walks the stated sequence from its end.
//
// Every level turns over, the one the sort does not write included -- which is
// what makes it the exact mirror. `desc` on a named level turns that one over
// and leaves the records it ties facing the way they were, which is a third
// sequence again.
func TestReversedIsTheMirrorOfTheSequence(t *testing.T) {
	// Two records tied on size, so the level that settles ties shows.
	const tied = `( (n: "a", size: 10), (n: "b", size: 10), (n: "c", size: 99) )`

	for _, c := range []struct{ spec, scope, want string }{
		{"sort={ .size }", "count=10", "0,1,2"},
		{"sort={ .size }", "count=10 reversed", "2,1,0"},
		{"sort={ .size desc }", "count=10", "2,0,1"},
		{"", "count=10", `0,1,2`},
		{"", "count=10 reversed", `2,1,0`},
	} {
		v := open(t, tied, c.spec)
		out, _ := fill(t, v, c.scope)
		if out.joined() != c.want {
			t.Errorf("%q %q gave %s, want %s", c.spec, c.scope, out.joined(), c.want)
		}
	}
}

// It names no field, so a reading that cannot name a record's identity answers
// it perfectly well. That is the whole reason it exists: under Members `key` is
// a member, not the record, so no sort level can reach what orders the
// sequence and only this can turn it over.
func TestReversedNeedsNoNameForTheIdentity(t *testing.T) {
	src, err := ParsePSLSource(doc, Members)
	if err != nil {
		t.Fatal(err)
	}
	set, err := src.Open(parseSpec(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := fill(t, set, "count=10 reversed")
	if out.joined() != `"plain","extra",3,2,1,0` {
		t.Errorf("reversed, the sequence is %s", out.joined())
	}
}

// One sequence read two ways is one sequence.
//
// That is what moving the direction onto the scope buys: the ordering prepared
// for reading up is the same one read down, so scrolling back through a list
// costs a walk rather than another sort of every record in it.
func TestOneSequenceIsReadBothWays(t *testing.T) {
	src, err := ParsePSLSource(doc, Whole)
	if err != nil {
		t.Fatal(err)
	}
	set, err := src.Open(parseSpec(t, "sort={ .size }"))
	if err != nil {
		t.Fatal(err)
	}
	first, _ := fill(t, set, "count=99")
	second, _ := fill(t, set, "count=99 reversed")
	if first.joined() == second.joined() {
		t.Errorf("both read as %s", first.joined())
	}
	if got := reverseOf(second.joined()); got != first.joined() {
		t.Errorf("backwards it reads %s, which is not %s turned over",
			second.joined(), first.joined())
	}
}

// reverseOf turns a comma-separated run of keys around.
func reverseOf(s string) string {
	parts := strings.Split(s, ",")
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, ",")
}

// A scope that sent nothing is complete up to where it was asked from.
//
// Vacuously, and usefully: the asker needs somewhere to carry on from, and
// where it asked from is somewhere it is still complete up to. Claiming
// nothing at all would leave it with no way to ask for the next scope but to
// start again.
func TestAScopeThatSentNothingIsCompleteUpToWhereItStarted(t *testing.T) {
	v := open(t, doc, "")
	_, done := fill(t, v, "after=1 count=0")
	if got := wire.EncodeValue(done.Watermark); got != "1" {
		t.Errorf("a scope of none from 1 claims %s", got)
	}
}
