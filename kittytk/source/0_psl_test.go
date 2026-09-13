package source

// Reading a PSL list as records, and drawing windows out of it.

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

func open(t *testing.T, text, spec string) ResultSet { return openAs(t, Whole, text, spec) }

func openAs(t *testing.T, reading Reading, text, spec string) ResultSet {
	t.Helper()
	src, err := ParsePSL(text, reading)
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

func parseFill(t *testing.T, args string) *wire.Fill {
	t.Helper()
	f, err := wire.ParseFill(statement(t, "query "+args).Args)
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

// collector is a Sink that keeps what it was given.
type collector struct {
	keys    []string
	fields  []wire.Fields
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
	c.keys = append(c.keys, wire.EncodeValue(key))
	c.fields = append(c.fields, fields)
	return nil
}

func (c *collector) Done(done Complete) { c.done, c.ended = done, true }

func (c *collector) joined() string { return strings.Join(c.keys, ",") }

// fill draws one window and reports the keys that came out of it.
func fill(t *testing.T, v ResultSet, args string) (*collector, Complete) {
	t.Helper()
	out := &collector{}
	if err := v.Fill(parseFill(t, args), out); err != nil {
		t.Fatal(err)
	}
	if !out.ended {
		t.Fatal("the stretch was never ended")
	}
	return out, out.done
}

// The ordered items and the keyed members are two collections, and they come
// out as one sequence because an integer ranks below a string: the items in
// their order, then the names in theirs. No rule of its own says so.
func TestBothOfAPSLListsCollectionsAreRecords(t *testing.T) {
	v := open(t, doc, "")
	out, done := fill(t, v, "have=0 need=10")
	if out.joined() != `0,1,2,3,"extra","plain"` {
		t.Errorf("the sequence is %s", out.joined())
	}
	if !done.Exhausted {
		t.Error("a window holding every record did not say so")
	}
	if !out.ordered {
		t.Error("the records went out in the sequence's order and did not say so")
	}
}

// A record's fields are its own members, each under a dot: the items at their
// positions, the keyed members by name.
func TestARecordsFieldsAreItsOwnMembers(t *testing.T) {
	v := open(t, doc, "")
	out, _ := fill(t, v, "have=0 need=1")
	got := out.fields[0].Encode()
	if got != `{ .0 "README.md"; .size 2048 }` {
		t.Errorf("the first record carries %s", got)
	}
}

// A record that is not a list has a key and a value and no members -- so a
// catalogue of plain strings is filterable and sortable rather than being
// nothing but keys.
func TestARecordThatIsNotAListIsAKeyAndAValue(t *testing.T) {
	v := open(t, doc, `filter={ eq key "plain" }`)
	out, _ := fill(t, v, "have=0 need=10")
	if len(out.fields) != 1 {
		t.Fatalf("%d records matched the key", len(out.fields))
	}
	if got := out.fields[0].Encode(); got != `{ value "just a string" }` {
		t.Errorf("a bare value carries %s", got)
	}
}

// A field one record has and another has not is undefined rather than an
// error, which is the whole of what a source of mixed records needs.
func TestAFieldARecordHasNotGotIsUndefined(t *testing.T) {
	v := open(t, doc, "filter={ eq .size undefined }")
	out, _ := fill(t, v, "have=0 need=10")
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
func TestASortedFilteredWindow(t *testing.T) {
	v := open(t, doc, `filter={ lt .size 1000 } sort={ .size desc }`)
	out, done := fill(t, v, "have=0 need=10")
	if out.joined() != `1,2,"extra","plain"` {
		t.Errorf("the sequence is %s", out.joined())
	}
	if !done.Exhausted {
		t.Error("the whole sequence did not say it was exhausted")
	}
}

// A window is as long as it was asked for, and what ends it says where it got
// to -- the sort fields and the key, which is what makes the boundary name
// exactly one position.
func TestAWindowStopsAndSaysWhereItGotTo(t *testing.T) {
	// `plain` is a bare string and has no first item, so it leads: undefined is
	// the bottom of the order, and it is one position like any other.
	v := open(t, doc, "sort={ .0 natural }")
	out, done := fill(t, v, "have=0 need=2")
	if out.joined() != `"plain",1` {
		t.Errorf("the first window is %s", out.joined())
	}
	if done.Exhausted {
		t.Error("a window with records past it said it was exhausted")
	}
	if got := done.Watermark.Encode(); got != `{ .0 "build.sh"; key 1 }` {
		t.Errorf("the watermark is %s", got)
	}

	// And the next window starts after it.
	next, _ := fill(t, v, "from="+done.Watermark.Encode()+" have=0 need=2")
	if next.joined() != `2,"extra"` {
		t.Errorf("the second window is %s", next.joined())
	}
}

// Have is how much of the window the far end can fill from what it holds, so a
// window already full costs nothing.
func TestAWindowAlreadyFullSendsNothing(t *testing.T) {
	v := open(t, doc, "")
	out, done := fill(t, v, "have=3 need=3")
	if len(out.keys) != 0 {
		t.Errorf("a full window was sent %d record(s)", len(out.keys))
	}
	if done.Exhausted {
		t.Error("a window that sent nothing claimed the sequence was over")
	}
}

// To is the far end saying how far its own knowledge runs. Every record inside
// it goes out whether the window is full or not, because that is what the far
// end has to merge against; past it, only the shortfall.
func TestEveryRecordInsideToGoesOut(t *testing.T) {
	v := open(t, doc, "")
	out, _ := fill(t, v, "to={ key 3 } have=4 need=4")
	if out.joined() != `0,1,2,3` {
		t.Errorf("the records inside the range are %s", out.joined())
	}
}

// A boundary is found rather than walked to: the ordering is total, so the
// position after it is a binary search. Nothing observable says so except that
// the answer is right from any point in a long sequence.
func TestAWindowStartsAfterABoundaryAnywhereInTheSequence(t *testing.T) {
	var b strings.Builder
	b.WriteString("(")
	for i := 0; i < 500; i++ {
		b.WriteString("(n: ")
		b.WriteString(itoa(i))
		b.WriteString("),")
	}
	b.WriteString(")")

	v := open(t, b.String(), "sort={ .n }")
	out, done := fill(t, v, "from={ .n 399; key 399 } have=0 need=3")
	if out.joined() != "400,401,402" {
		t.Errorf("the window after 399 is %s", out.joined())
	}
	if got := done.Watermark.Encode(); got != "{ .n 402; key 402 }" {
		t.Errorf("the watermark is %s", got)
	}
}

// A window names the fields it wants where they are fewer than the query's,
// which is how the far end asks for the skeleton of a wide stretch.
func TestAWindowAsksForFewerFieldsThanTheQuery(t *testing.T) {
	v := open(t, doc, "")
	out, _ := fill(t, v, "fields={ .size } have=0 need=1")
	if got := out.fields[0].Encode(); got != "{ .size 2048 }" {
		t.Errorf("the record carries %s", got)
	}
}

// A different sequence is a different result set, opened alongside the one it
// replaces and closed after it -- which is what keeps the source in use while
// the reader moves across.
func TestADifferentSequenceIsADifferentResultSet(t *testing.T) {
	src, err := ParsePSL(doc, Whole)
	if err != nil {
		t.Fatal(err)
	}
	byName, err := src.Open(parseSpec(t, "sort={ .0 natural }"))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := fill(t, byName, "have=0 need=1")
	if out.joined() != `"plain"` {
		t.Errorf("by name the first record is %s", out.joined())
	}

	bySize, err := src.Open(parseSpec(t, "sort={ .size desc }"))
	if err != nil {
		t.Fatal(err)
	}
	byName.Close()
	out, _ = fill(t, bySize, "have=0 need=1")
	if out.joined() != "3" {
		t.Errorf("by size the first record is %s", out.joined())
	}
}

// A result set is refused rather than opened wrong. An order that is quietly a
// little different corrupts every answer after it and looks like data.
func TestASortNobodyCanProduceIsRefused(t *testing.T) {
	src, err := ParsePSL(doc, Whole)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.Open(parseSpec(t, "sort={ .name collate=turkish }")); err == nil {
		t.Error("a collation nobody carries was accepted")
	}
}

// The same sequence is ordered once. Two result sets over it, and one opened
// again on an order somebody had before, draw on the ordering already built.
func TestOneSequenceIsOrderedOnce(t *testing.T) {
	src, err := ParsePSL(doc, Whole)
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
	if a.(*pslResultSet).ord != b.(*pslResultSet).ord {
		t.Error("two result sets over one sequence built it twice")
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
	if c.(*pslResultSet).ord == a.(*pslResultSet).ord {
		t.Error("the cache grew without limit")
	}
	out, _ := fill(t, c, "have=0 need=10")
	if out.joined() != `"plain","extra",2,1,0,3` {
		t.Errorf("the rebuilt ordering is %s", out.joined())
	}
}

// A nested list has no order of its own, so it takes the unordered rank and
// crosses as a block of its own members -- the shape a record's fields take,
// which is what it is.
func TestANestedListCrossesAsItsOwnMembers(t *testing.T) {
	v := open(t, `( (name: "figaro", tags: ("red", "blue", weight: 3)) )`, "")
	out, _ := fill(t, v, "have=0 need=1")
	want := `{ .name "figaro"; .tags { .0 "red"; .1 "blue"; .weight 3 } }`
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
	out, done := fill(t, v, "have=0 need=2")
	if out.joined() != "0,1" {
		t.Errorf("by size the window is %s", out.joined())
	}
	if got := out.fields[0].Encode(); got != `{ name "README.md"; size 2048 }` {
		t.Errorf("the first record carries %s", got)
	}
	if got := done.Watermark.Encode(); got != `{ size 310; key 1 }` {
		t.Errorf("the watermark is %s", got)
	}
}

// And under Members there is no way to name the record itself. `key` and
// `value` are members that are not there rather than the record's own, so a
// sort by either is a sort by undefined and settles on the key.
func TestMembersCannotNameTheRecordItself(t *testing.T) {
	v := openAs(t, Members, doc, "filter={ ne key undefined }")
	out, _ := fill(t, v, "have=0 need=10")
	if len(out.keys) != 0 {
		t.Errorf("`key` reached something under Members: %s", out.joined())
	}

	// A record that is not a list has no members, so it carries nothing.
	v = openAs(t, Members, doc, "")
	out, _ = fill(t, v, "have=0 need=10")
	if got := out.fields[len(out.fields)-1].Encode(); got != "{}" {
		t.Errorf("a bare value carries %s under Members", got)
	}
}

// Whole reaches a member called `key`, which the bundle format's own metadata
// has, and Members does not.
func TestOnlyWholeReachesAMemberCalledKey(t *testing.T) {
	const meta = `( _bundle: (key: "figaro", author: "Jeffrey R. Day") )`

	v := open(t, meta, `filter={ eq .key "figaro" }`)
	out, _ := fill(t, v, "have=0 need=10")
	if out.joined() != `"_bundle"` {
		t.Errorf("under Whole the member called key found %s", out.joined())
	}
	if got := out.fields[0].Encode(); got != `{ .author "Jeffrey R. Day"; .key "figaro" }` {
		t.Errorf("under Whole the record carries %s", got)
	}

	v = openAs(t, Members, meta, "")
	out, _ = fill(t, v, "have=0 need=10")
	if got := out.fields[0].Encode(); got != `{ author "Jeffrey R. Day" }` {
		t.Errorf("under Members the record carries %s", got)
	}
}

// Under Members a member called `key` is out of reach, not merely unsent: the
// field bag already carries a key, and a member answering to the same name
// would make a record's identity depend on which one was read.
func TestMembersReachesNoMemberCalledKey(t *testing.T) {
	v := openAs(t, Members, `( _bundle: (key: "figaro") )`, `filter={ eq key "figaro" }`)
	out, _ := fill(t, v, "have=0 need=10")
	if len(out.keys) != 0 {
		t.Errorf("`key` reached a member under Members: %s", out.joined())
	}
}

// Under Whole the dot is the whole of what says a name is a member, so a name
// without one reaches nothing -- including when the letters after where the dot
// would have been happen to name a member.
func TestUnderWholeANameWithoutADotIsNotAMember(t *testing.T) {
	const trap = `( (ame: "trap", name: "real") )`

	v := open(t, trap, `filter={ eq name "trap" }`)
	out, _ := fill(t, v, "have=0 need=10")
	if len(out.keys) != 0 {
		t.Errorf("an undotted name reached a member: %s", out.joined())
	}

	v = open(t, trap, `filter={ eq .name "real" }`)
	out, _ = fill(t, v, "have=0 need=10")
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
	out, _ := fill(t, v, "have=0 need=10")
	if out.joined() != "0" {
		t.Errorf("the word text found %s", out.joined())
	}

	v = open(t, kinds, `filter={ eq .kind "text" }`)
	out, _ = fill(t, v, "have=0 need=10")
	if out.joined() != "1" {
		t.Errorf("the string text found %s", out.joined())
	}

	// And a symbol goes out as one, which is what the far end reads back.
	v = open(t, kinds, "")
	out, _ = fill(t, v, "have=0 need=1")
	if got := out.fields[0].Encode(); got != `{ .kind text; .name "a" }` {
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
	out, _ := fill(t, v, "have=0 need=1")

	got := out.fields[0].Encode()
	want := `{ .addr (objectLibrary/figaro/3); .dashed kebab-case; .dated 2026-09-13; ` +
		`.digits 1x; .ok plain; .starred (*star) }`
	if got != want {
		t.Errorf("the record carries %s", got)
	}

	// And every one of them reads back as the symbol it was sent as.
	script, err := wire.Parse("result 1 fields=" + got)
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
	out, _ := fill(t, v, "have=0 need=10")
	if out.joined() != "0" {
		t.Errorf("undefined found %s", out.joined())
	}
}
