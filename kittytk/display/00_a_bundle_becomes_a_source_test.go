package display

// A bundle is a document until somebody loads it. This is that: what it
// declares, what it becomes, and which includes end up sharing one of it.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/phroun/serval"
)

// doc writes a bundle: its key and version, what it includes, and its records.
func doc(key, version, includes, body string) string {
	if includes != "" {
		includes = ", includes: ( " + includes + " )"
	}
	return fmt.Sprintf("( _bundle: ( key: %q, version: %q%s )%s )", key, version, includes, body)
}

// stock puts a bundle in a store under a key of the store's own choosing.
func stock(t *testing.T, s *appStore, at, text string) {
	t.Helper()
	if _, err := s.put(at, "psl", []byte(text)); err != nil {
		t.Fatalf("%s: %v", at, err)
	}
}

// keysOf reads every record a source holds, by identity.
func keysOf(t *testing.T, src serval.Source) []string {
	t.Helper()
	set, err := src.Open(&serval.DataSetDescriptor{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	c := &collect{}
	if err := set.Read(&serval.Scope{Count: 100}, c); err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(c.ids))
	for _, id := range c.ids {
		out = append(out, id.String())
	}
	return out
}

func has(all []string, want string) bool {
	for _, k := range all {
		if k == want {
			return true
		}
	}
	return false
}

// A bundle that includes nothing is its records and no more.
func TestALeafBundleIsItsRecords(t *testing.T) {
	s := shelves(t)
	stock(t, s, "leafItem", doc("leaf", "0.1.0", "", `, ("first"), ("second"), note: "a keyed one"`))

	got, err := s.LoadBundle("leaf", "0.1.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Trouble) != 0 {
		t.Fatalf("it reported %v", got.Trouble)
	}
	keys := keysOf(t, got.Source)
	if len(keys) != 3 {
		t.Fatalf("it holds %v", keys)
	}
	// Its metadata is ABOUT the document and is not one of its records.
	for _, gone := range []string{`"_bundle"`, `"_amendments"`} {
		if has(keys, gone) {
			t.Errorf("%s came back as a record: %v", gone, keys)
		}
	}
}

// What it includes arrives under the include's name, and what it holds of its
// own arrives under keys of its own, in one source.
func TestAnIncludedBundleArrivesUnderItsAlias(t *testing.T) {
	s := shelves(t)
	stock(t, s, "childItem", doc("child", "0.1.0", "", `, ("from the child")`))
	stock(t, s, "parentItem", doc("parent", "0.1.0",
		`kid: ( bundle: "child", want: "0.1.0" )`, `, ("from the parent")`))

	got, err := s.LoadBundle("parent", "0.1.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	keys := keysOf(t, got.Source)
	if !has(keys, "kid/0") {
		t.Errorf("the child's record is not at kid/0: %v", keys)
	}
	if !has(keys, "0") {
		t.Errorf("the parent's own record is not at 0: %v", keys)
	}
}

// THE POINT. Two includes wanting the newest get one source between them, so
// every cache and every invalidation over it is shared -- and neither can have
// resolved to a different version, because only one of them ever asked.
func TestIncludesWantingTheNewestShareOneSource(t *testing.T) {
	s := shelves(t)
	stock(t, s, "figaro09", doc("figaro", "0.1.0", "", `, ("old")`))
	stock(t, s, "figaro13", doc("figaro", "0.1.3", "", `, ("new")`))

	// Different floors, no upper bound: one selector, so one source.
	l := &loader{store: s, built: map[string]serval.Source{}, open: map[string]bool{}}
	a, err := l.build(want{selector: selector{key: "figaro"}, floor: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := l.build(want{selector: selector{key: "figaro"}, floor: "0.1.2"})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Error(">= 0.1.0 and >= 0.1.2 were given two sources; the floor is not part of the selector")
	}
	// And what they share is the NEWEST, not the one either floor named: the
	// floor says what is acceptable, never which to take.
	set, err := a.Open(&serval.DataSetDescriptor{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	c := &collect{}
	if err := set.Read(&serval.Scope{Count: 10}, c); err != nil {
		t.Fatal(err)
	}
	if len(c.fields) != 1 {
		t.Fatalf("the shared source holds %d records", len(c.fields))
	}
	if got := c.fields[0].Get(".0"); got == nil || got.Str != "new" {
		t.Errorf("it took %v, and 0.1.3 is the newest here", c.fields[0])
	}
}

// An upper bound is a different question, so it gets a source of its own --
// which is the whole of how two versions come to be in one graph.
func TestAnUpperBoundIsADifferentSelector(t *testing.T) {
	s := shelves(t)
	stock(t, s, "figaro13", doc("figaro", "0.1.3", "", `, ("new")`))
	stock(t, s, "figaro20", doc("figaro", "0.2.0", "", `, ("newer")`))

	l := &loader{store: s, built: map[string]serval.Source{}, open: map[string]bool{}}
	latest, err := l.build(want{selector: selector{key: "figaro"}})
	if err != nil {
		t.Fatal(err)
	}
	bounded := selector{key: "figaro", upper: "0.2.0"}
	held, err := l.build(want{selector: bounded, floor: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if latest == held {
		t.Error("a bounded range shared with the unbounded one")
	}
}

// Newest-wins, and the floor is a question about the answer rather than about
// which to take -- so a floor that cannot be met says what it got.
func TestAFloorIsCheckedOnWhatTheNewestIs(t *testing.T) {
	s := shelves(t)
	stock(t, s, "figaro10", doc("figaro", "0.1.0", "", `, ("only this")`))
	stock(t, s, "wants", doc("wants", "0.1.0", `figaro: ">= 0.2.0"`, ""))

	_, err := s.LoadBundle("wants", "0.1.0", nil)
	if err == nil {
		t.Fatal("a floor nothing meets was met")
	}
	if !strings.Contains(err.Error(), "0.1.0") {
		t.Errorf("the refusal does not say what it found: %v", err)
	}
}

// Optional is the author saying an absence is survivable. Mandatory is the
// author saying it is not.
func TestOptionalIsAbsentAndMandatoryStopsTheLoad(t *testing.T) {
	s := shelves(t)
	stock(t, s, "soft", doc("soft", "0.1.0",
		`missing: ( want: ">= 1.0", optional: true )`, `, ("still here")`))
	stock(t, s, "hard", doc("hard", "0.1.0", `missing: ">= 1.0"`, `, ("never seen")`))

	got, err := s.LoadBundle("soft", "0.1.0", nil)
	if err != nil {
		t.Fatalf("an optional include stopped the load: %v", err)
	}
	if len(got.Trouble) != 1 || got.Trouble[0].Alias != "missing" {
		t.Fatalf("it reported %v", got.Trouble)
	}
	if keys := keysOf(t, got.Source); len(keys) != 1 {
		t.Errorf("the rest of the bundle went with it: %v", keys)
	}

	if _, err := s.LoadBundle("hard", "0.1.0", nil); err == nil {
		t.Error("a mandatory include that is not there did not stop the load")
	}
}

// An amendment shadows one named record of what was included, and a member with
// nothing under it deletes one.
func TestAnAmendmentReplacesAndDeletesWhatWasIncluded(t *testing.T) {
	s := shelves(t)
	stock(t, s, "childItem", doc("child", "0.1.0", "", `, ("keep"), ("replace me"), ("delete me")`))
	stock(t, s, "parentItem", `( _bundle: ( key: "parent", version: "0.1.0",`+
		` includes: ( kid: ( bundle: "child", want: "0.1.0" ) ) ),`+
		` _amendments: ( "kid/1": ("stood in for"), "kid/2": nil ) )`)

	got, err := s.LoadBundle("parent", "0.1.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	keys := keysOf(t, got.Source)
	if !has(keys, "kid/0") {
		t.Errorf("an unamended record went missing: %v", keys)
	}
	if has(keys, "kid/2") {
		t.Errorf("a deleted record is still there: %v", keys)
	}
	if !has(keys, "kid/1") {
		t.Errorf("a replaced record went missing: %v", keys)
	}
}

// A ring has no resolution, so the load is refused and says which one closed
// it. One hop or several: what matters is that a bundle is reached while it is
// still being built.
func TestABundleThatIncludesItselfIsRefused(t *testing.T) {
	s := shelves(t)
	stock(t, s, "ring", doc("ring", "0.1.0", `ring: "0.1.0"`, ""))
	_, err := s.LoadBundle("ring", "0.1.0", nil)
	if err == nil {
		t.Fatal("a bundle including itself loaded")
	}
	if !strings.Contains(err.Error(), "ring") {
		t.Errorf("the refusal does not name it: %v", err)
	}

	// And the long way round, which no single bundle can see for itself.
	two := shelves(t)
	stock(t, two, "aItem", doc("a", "0.1.0", `b: "0.1.0"`, ""))
	stock(t, two, "bItem", doc("b", "0.1.0", `a: "0.1.0"`, ""))
	if _, err := two.LoadBundle("a", "0.1.0", nil); err == nil {
		t.Error("a includes b includes a, and it loaded")
	}
}

// A hash names one bundle for good, whatever versions come later.
func TestAnIncludePinnedToAHashTakesThatOne(t *testing.T) {
	s := shelves(t)
	old := doc("figaro", "0.1.0", "", `, ("the pinned one")`)
	stock(t, s, "figaro10", old)
	stock(t, s, "figaro99", doc("figaro", "9.9.9", "", `, ("the newest")`))
	stock(t, s, "pins", doc("pins", "0.1.0",
		fmt.Sprintf("figaro: %q", hashOf([]byte(old))), ""))

	got, err := s.LoadBundle("pins", "0.1.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	set, err := got.Source.Open(&serval.DataSetDescriptor{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	c := &collect{}
	if err := set.Read(&serval.Scope{Count: 10}, c); err != nil {
		t.Fatal(err)
	}
	if len(c.fields) != 1 {
		t.Fatalf("it holds %d records", len(c.fields))
	}
	if got := c.fields[0].Get(".0"); got == nil || got.Str != "the pinned one" {
		t.Errorf("the pin took %v", c.fields[0])
	}
}

// `optional` is a term of the expression, so it reads the same wherever an
// expression is written -- and a list has a member of its own for where there
// is no expression to put it in.
func TestOptionalIsSaidTheSameWayEverywhere(t *testing.T) {
	s := shelves(t)
	stock(t, s, "terse", doc("terse", "0.1.0", `missing: "1.4.2, optional"`, `, ("held")`))
	stock(t, s, "ranged", doc("ranged", "0.1.0", `missing: ">= 1.0, optional"`, `, ("held")`))
	stock(t, s, "listed", doc("listed", "0.1.0",
		`missing: ( bundle: "nowhere", want: ">= 1.0, optional" )`, `, ("held")`))
	stock(t, s, "membered", doc("membered", "0.1.0",
		`missing: ( source: "nothing registered", optional: true )`, `, ("held")`))

	for _, key := range []string{"terse", "ranged", "listed", "membered"} {
		got, err := s.LoadBundle(key, "0.1.0", nil)
		if err != nil {
			t.Errorf("%s: an optional include stopped the load: %v", key, err)
			continue
		}
		if len(got.Trouble) != 1 {
			t.Errorf("%s reported %v", key, got.Trouble)
		}
		if keys := keysOf(t, got.Source); len(keys) != 1 {
			t.Errorf("%s lost its own records: %v", key, keys)
		}
	}
}

// A live source registered by name is included like anything else, which is
// what lets a bundle wrap and amend a source of ANY kind rather than only
// other bundles.
func TestABundleIncludesALiveSource(t *testing.T) {
	s := shelves(t)
	stock(t, s, "wrapItem", doc("wrap", "0.1.0", `feed: ( source: "mail.incoming" )`,
		`, ("the wrapper's own")`))

	live := Sources{"mail.incoming": serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(0), serval.Record{serval.Named("subject", "hello")}),
		serval.NewRow(serval.NewInt(1), serval.Record{serval.Named("subject", "again")}),
	})}

	got, err := s.LoadBundle("wrap", "0.1.0", live)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Trouble) != 0 {
		t.Fatalf("it reported %v", got.Trouble)
	}
	keys := keysOf(t, got.Source)
	for _, want := range []string{"feed/0", "feed/1", "0"} {
		if !has(keys, want) {
			t.Errorf("%s is missing from %v", want, keys)
		}
	}
}

// And a bundle may amend what it wrapped, which is the point: a live source of
// somebody else's making, with this author's corrections over it.
func TestABundleAmendsALiveSource(t *testing.T) {
	s := shelves(t)
	stock(t, s, "fixItem", `( _bundle: ( key: "fix", version: "0.1.0",`+
		` includes: ( feed: ( source: "mail.incoming" ) ) ),`+
		` _amendments: ( feed/1: nil ) )`)

	live := Sources{"mail.incoming": serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(0), serval.Record{serval.Named("subject", "keep")}),
		serval.NewRow(serval.NewInt(1), serval.Record{serval.Named("subject", "drop")}),
	})}

	got, err := s.LoadBundle("fix", "0.1.0", live)
	if err != nil {
		t.Fatal(err)
	}
	keys := keysOf(t, got.Source)
	if has(keys, "feed/1") {
		t.Errorf("the amended-away record is still there: %v", keys)
	}
	if !has(keys, "feed/0") {
		t.Errorf("the rest of the live source went with it: %v", keys)
	}
}

// The two namespaces are apart: a bundle called `inbox` and a source called
// `inbox` are two things, and an include says which it means.
func TestABundleAndASourceMayShareAName(t *testing.T) {
	s := shelves(t)
	stock(t, s, "inboxItem", doc("inbox", "0.1.0", "", `, ("from the bundle")`))
	stock(t, s, "bothItem", doc("both", "0.1.0",
		`asBundle: ( bundle: "inbox", want: "0.1.0" ), asSource: ( source: "inbox" )`, ""))

	live := Sources{"inbox": serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(0), serval.Record{serval.Named("from", "the source")}),
	})}
	got, err := s.LoadBundle("both", "0.1.0", live)
	if err != nil {
		t.Fatal(err)
	}
	keys := keysOf(t, got.Source)
	if !has(keys, "asBundle/0") || !has(keys, "asSource/0") {
		t.Errorf("one namespace swallowed the other: %v", keys)
	}
}

// There is one of a registered source, so a version says nothing about which --
// and an author who wrote one believed something that is not so.
func TestAVersionOnARegisteredSourceIsRefused(t *testing.T) {
	s := shelves(t)
	stock(t, s, "wrongItem", doc("wrong", "0.1.0",
		`feed: ( source: "live", want: ">= 1.0" )`, ""))

	got, err := s.LoadBundle("wrong", "0.1.0", Sources{"live": serval.NewListSource(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Trouble) != 1 {
		t.Fatalf("a version on a registered source was taken: %v", got.Trouble)
	}
}

// A pin and a range in one expression is an author believing two things.
func TestAPinAndARangeTogetherAreRefused(t *testing.T) {
	if _, err := parseWant("figaro", "1.4.2, >= 2.0"); err == nil {
		t.Error("a pinned version was also given a floor")
	}
	if _, err := parseWant("figaro", "1.4.2, optional"); err != nil {
		t.Errorf("a pin with optional was refused: %v", err)
	}
}

// The namespaces are told apart by a mark rather than by luck. A bundle's
// selector reads `inbox@latest`, so a SOURCE called that would answer to a
// bundle's name if the two shared one space.
func TestASourceCannotImpersonateABundlesSelector(t *testing.T) {
	s := shelves(t)
	stock(t, s, "inboxItem", doc("inbox", "0.1.0", "", `, ("from the bundle")`))
	stock(t, s, "bothItem", doc("both", "0.1.0",
		`fromBundle: ( bundle: "inbox", want: ">= 0.1.0" ),`+
			` fromSource: ( source: "inbox@latest" )`, ""))

	live := Sources{"inbox@latest": serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(0), serval.Record{serval.Named("from", "the impostor")}),
	})}
	got, err := s.LoadBundle("both", "0.1.0", live)
	if err != nil {
		t.Fatal(err)
	}

	set, err := got.Source.Open(&serval.DataSetDescriptor{})
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	c := &collect{}
	if err := set.Read(&serval.Scope{Count: 10}, c); err != nil {
		t.Fatal(err)
	}
	// Each include got what IT named. Sharing is by selector, so two namespaces
	// sharing one would have the first built answer for both.
	want := map[string]string{"fromBundle/0": ".0", "fromSource/0": "from"}
	seen := 0
	for i, id := range c.ids {
		member, named := want[id.String()]
		if !named {
			continue
		}
		seen++
		got := c.fields[i].Get(member)
		if got == nil {
			t.Errorf("%s carries no %s: %v", id, member, c.fields[i])
			continue
		}
		if id.String() == "fromBundle/0" && got.Str != "from the bundle" {
			t.Errorf("the bundle include answered with %q", got.Str)
		}
		if id.String() == "fromSource/0" && got.Str != "the impostor" {
			t.Errorf("the source include answered with %q, not what was registered", got.Str)
		}
	}
	if seen != 2 {
		t.Errorf("only %d of the two includes arrived: %v", seen, c.ids)
	}
}

// An include names one namespace or the other. Both is an author believing two
// things, and taking either silently would hide which one was wrong.
func TestAnIncludeNamingBothNamespacesIsRefused(t *testing.T) {
	s := shelves(t)
	stock(t, s, "mixedItem", doc("mixed", "0.1.0",
		`confused: ( bundle: "somewhere", source: "elsewhere" )`, `, ("held")`))

	got, err := s.LoadBundle("mixed", "0.1.0",
		Sources{"elsewhere": serval.NewListSource(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Trouble) != 1 {
		t.Fatalf("an include naming both namespaces was taken: %v", got.Trouble)
	}
	if !strings.Contains(got.Trouble[0].Reason, "both") {
		t.Errorf("the report does not say why: %q", got.Trouble[0].Reason)
	}
}
