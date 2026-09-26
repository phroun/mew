package display

// The whole path, end to end: a bundle on disk, a statement naming it, and a
// trinket reading its records.
//
// Every piece of this existed before and none of them were joined. The store
// held bundles, the loader assembled them, the trinket took a name -- and
// nothing turned the name into the store. This is the join, and it is checked by
// walking all of it rather than by any part of it saying it would work.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/serval"
)

// onA connection whose store is the one given, which is what the desktop does
// for a real one after the handshake.
func onStore(t *testing.T, s *appStore) *protocol.BindContext {
	t.Helper()
	ctx := &protocol.BindContext{}
	trinkets.SetSourceFinder(ctx, func(name string) (serval.Source, error) {
		key, version, isBundle := trinkets.BundleName(name)
		if !isBundle {
			return trinkets.LookupSource(name)
		}
		loaded, err := s.LoadBundle(key, version, Sources(trinkets.LiveSources()))
		if err != nil {
			return nil, err
		}
		return loaded.Source, nil
	})
	return ctx
}

// A bundle filed in a store, named by a statement, read as rows.
func TestAStatementNamesABundleAndGetsItsRows(t *testing.T) {
	s := shelves(t)
	stock(t, s, "figures-1.0.0", doc("figures", "1.0.0", "",
		`, ("the first"), ("the second"), ("the third")`))

	src, err := trinkets.LookupSourceOn(onStore(t, s), "bundle:figures")
	if err != nil {
		t.Fatalf("the name did not reach the store: %v", err)
	}

	l := trinkets.NewListView()
	l.SetSource(src)
	// A bundle's records are read WHOLE, so a list's members wear a dot: these
	// are `("the first")`, whose text stands at position nought. The default
	// names are a made source's, and a source of somebody else's making has its
	// own -- which is what naming the field is for.
	l.SetFields(".0", ".0")

	if l.Count() != 3 {
		t.Fatalf("it counts %d rows, want 3", l.Count())
	}
	if got := l.Item(1); got == nil || got.Text != "the second" {
		t.Errorf("row 1 is %v", got)
	}
}

// A version rides after an `@`, and picks that one rather than the newest.
func TestAStatementCanNameAVersion(t *testing.T) {
	s := shelves(t)
	stock(t, s, "figures-1.0.0", doc("figures", "1.0.0", "", `, ("old")`))
	stock(t, s, "figures-2.0.0", doc("figures", "2.0.0", "", `, ("new"), ("newer")`))

	ctx := onStore(t, s)

	src, err := trinkets.LookupSourceOn(ctx, "bundle:figures@1.0.0")
	if err != nil {
		t.Fatalf("naming a version: %v", err)
	}
	l := trinkets.NewListView()
	l.SetSource(src)
	l.SetFields(".0", ".0")
	if l.Count() != 1 || l.Item(0).Text != "old" {
		t.Errorf("pinned to 1.0.0 it read %d rows starting %v", l.Count(), l.Item(0))
	}

	// And with no version, the newest there is.
	src, err = trinkets.LookupSourceOn(ctx, "figures")
	if err != nil {
		t.Fatalf("naming no version: %v", err)
	}
	l.SetSource(src)
	l.SetFields(".0", ".0")
	if l.Count() != 2 {
		t.Errorf("with no version it read %d rows, want the newest bundle's 2", l.Count())
	}
}

// A bundle's own `source:` includes reach the SAME registry a trinket's name
// does, so there is one set of live names rather than two.
func TestABundlesLiveIncludesReachTheSameNames(t *testing.T) {
	s := shelves(t)
	stock(t, s, "wrapper-1.0.0", doc("wrapper", "1.0.0",
		`inbox: ( source: "test.join.inbox" )`, `, ("one of the wrapper's own")`))

	trinkets.RegisterSource("test.join.inbox", serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(0), serval.Record{serval.Named("display", "a message")}),
	}))
	defer trinkets.UnregisterSource("test.join.inbox")

	src, err := trinkets.LookupSourceOn(onStore(t, s), "bundle:wrapper")
	if err != nil {
		t.Fatalf("the bundle would not load: %v", err)
	}
	keys := keysOf(t, src)
	if !has(keys, "inbox/0") {
		t.Errorf("the registered source is not in it: %v", keys)
	}
	if !has(keys, "0") {
		t.Errorf("the bundle's own record is not in it: %v", keys)
	}
}

// **The store is per connection, so the answer is.** Two connections naming the
// same bundle get the bundle each of them has, which is the whole reason this
// hangs off the connection rather than the process.
func TestTwoConnectionsNamingOneBundleGetTheirOwn(t *testing.T) {
	first, second := shelves(t), shelves(t)
	stock(t, first, "shared-1.0.0", doc("shared", "1.0.0", "", `, ("the first app's")`))
	stock(t, second, "shared-1.0.0", doc("shared", "1.0.0", "",
		`, ("the second app's"), ("and another")`))

	one, err := trinkets.LookupSourceOn(onStore(t, first), "shared")
	if err != nil {
		t.Fatal(err)
	}
	two, err := trinkets.LookupSourceOn(onStore(t, second), "shared")
	if err != nil {
		t.Fatal(err)
	}

	a, b := trinkets.NewListView(), trinkets.NewListView()
	a.SetSource(one)
	b.SetSource(two)
	a.SetFields(".0", ".0")
	b.SetFields(".0", ".0")
	if a.Count() != 1 || b.Count() != 2 {
		t.Fatalf("the two read %d and %d rows, want 1 and 2", a.Count(), b.Count())
	}
	if got := a.Item(0); got == nil || got.Text != "the first app's" {
		t.Errorf("the first connection read %v", got)
	}
	if got := b.Item(0); got == nil || got.Text != "the second app's" {
		t.Errorf("the second connection read %v", got)
	}
}

// With no connection there is no store, so a bundle name says so rather than
// finding somebody else's. That is what an application built in Go, naming a
// bundle it has no desktop to look it up in, is told.
func TestWithNoConnectionABundleNameSaysSo(t *testing.T) {
	trinkets.SetBundleLoader(nil)
	_, err := trinkets.LookupSourceOn(nil, "bundle:figures")
	if err == nil {
		t.Fatal("it found a bundle with no store to find one in")
	}
	if !strings.Contains(err.Error(), "knows how to find") {
		t.Errorf("the refusal reads as a missing bundle: %v", err)
	}
}

// A bundle the store has not got is refused, and the refusal names it.
func TestABundleAStoreHasNotGotIsRefused(t *testing.T) {
	s := shelves(t)
	stock(t, s, "figures-1.0.0", doc("figures", "1.0.0", "", `, ("one")`))

	_, err := trinkets.LookupSourceOn(onStore(t, s), "bundle:nothingHere")
	if err == nil {
		t.Fatal("it found a bundle nothing filed")
	}
	if !strings.Contains(err.Error(), "nothingHere") {
		t.Errorf("the refusal does not name what was asked for: %v", err)
	}
}

// A record with a NAMED member is named with a dot too, that being what the
// Whole reading calls a list's members. A bundle carrying `(caption: "...")`
// rows is read by naming `.caption`.
func TestABundlesNamedMembersWearADot(t *testing.T) {
	s := shelves(t)
	stock(t, s, "captioned-1.0.0", doc("captioned", "1.0.0", "",
		`, (caption: "first", weight: 10), (caption: "second", weight: 20)`))

	src, err := trinkets.LookupSourceOn(onStore(t, s), "captioned")
	if err != nil {
		t.Fatal(err)
	}
	l := trinkets.NewListView()
	l.SetSource(src)
	l.SetFields(".caption", ".weight")

	if got := l.Item(0); got == nil || got.Text != "first" {
		t.Errorf("row 0 shows %v, want the caption", got)
	}
	if v := l.ValueAt(1); v == nil || !v.IsInt || v.Int != 20 {
		t.Errorf("row 1 means %v, want the weight 20", v)
	}
}

// --- a bundle says what shape its records are ----------------------------

// storeHolding is a store with one bundle in it, filed under its own key.
func storeHolding(t *testing.T, key, text string) *appStore {
	t.Helper()
	s := shelves(t)
	stock(t, s, key+"-1.0.0", text)
	return s
}

// A bundle whose records are a hierarchy, saying so.
//
// **The field names wear a DOT**, because a bundle's records do: read under
// serval's `Whole`, a record that is a list names its members `.name` and `.up`.
// It is the same rule `display=` and `value=` follow on a list, and it is the one
// thing about this that catches people.
//
// `up` is the parent's KEY, and a positional record's key is its index -- so
// local's `.up: 0` puts it under usr, the first record of the document.
const filesBundle = `(
  _bundle: (
    key: "files", version: "1.0.0",
    tree: ( parent: ".up", order: ".rank", label: ".name", children: ".kids" )
  ),
  ( name: "usr",   rank: 2, kids: 1 ),
  ( name: "etc",   rank: 1, kids: 0 ),
  ( name: "local", up: 0, rank: 1, kids: 0 )
)`

// **A bundle can say what shape its own records are**, and the source it becomes
// carries the saying -- so a reader given only the source can ask.
func TestABundleSaysWhatShapeItsRecordsAre(t *testing.T) {
	s := storeHolding(t, "files", filesBundle)
	src, err := trinkets.LookupSourceOn(onStore(t, s), "bundle:files")
	if err != nil {
		t.Fatalf("loading it: %v", err)
	}
	hint, said := serval.TreeHintOf(src)
	if !said {
		t.Fatal("the bundle said what its records are, and the source it became did not")
	}
	if hint.Parent != ".up" || hint.Order != ".rank" ||
		hint.Label != ".name" || hint.Children != ".kids" {
		t.Errorf("it says %+v", hint)
	}
}

// A bundle that says nothing about its shape says nothing, rather than an empty
// hint that reads as one.
func TestABundleThatSaysNothingSaysNothing(t *testing.T) {
	s := storeHolding(t, "plain", `(_bundle: (key: "plain", version: "1.0.0"), ("a record"))`)
	src, err := trinkets.LookupSourceOn(onStore(t, s), "bundle:plain")
	if err != nil {
		t.Fatalf("loading it: %v", err)
	}
	if _, said := serval.TreeHintOf(src); said {
		t.Error("a bundle with no tree block says something about its shape")
	}
}

// **A hint that cannot mean what it says is a REPORT, not a refusal.** The records
// are all still there and the author's mistake is about their shape, so the bundle
// loads, reads as a flat list, and says why it is not a tree. Refusing would lose
// the records over a sentence about them.
func TestAContradictoryHintIsReportedAndTheBundleLoads(t *testing.T) {
	s := storeHolding(t, "muddle", `(
  _bundle: (
    key: "muddle", version: "1.0.0",
    tree: ( parent: "up", location: "where", delimiter: "/" )
  ),
  ("a record")
)`)
	loaded, err := s.LoadBundle("muddle", "", nil)
	if err != nil {
		t.Fatalf("it was refused: %v", err)
	}
	if loaded.Source == nil {
		t.Fatal("it loaded no source")
	}
	if _, said := serval.TreeHintOf(loaded.Source); said {
		t.Error("a hint that cannot mean what it says was carried anyway")
	}
	if len(loaded.Trouble) == 0 {
		t.Fatal("nothing was said about it")
	}
	if !strings.Contains(loaded.Trouble[0].Reason, "two ways down") {
		t.Errorf("it says %q, want what is wrong with the hint", loaded.Trouble[0].Reason)
	}
}

// **The whole path: a document says its records are a hierarchy, and a tree draws
// one.** Nobody configured a tree anywhere -- no criterion, no node type, no
// standing. The bundle said what its records ARE and the view grew the rest.
//
// The caption comes from the hint's `label`, which is the one word it says about
// what a record is called, and no mapping is declared here either.
func TestATreeGrowsItselfFromABundlesHint(t *testing.T) {
	s := storeHolding(t, "files", filesBundle)
	src, err := trinkets.LookupSourceOn(onStore(t, s), "bundle:files")
	if err != nil {
		t.Fatalf("loading it: %v", err)
	}

	tv := trinkets.NewTreeView()
	tv.SetSource(src)

	// `rank` orders the top level, so etc stands before usr.
	if got, want := strings.Join(captionsOfTree(tv), " "), "etc usr"; got != want {
		t.Fatalf("the tree reads\n  %s\nwant\n  %s", got, want)
	}
	// And usr is not a leaf: the bundle said it has one child.
	items := tv.RootItems()
	if len(items) != 2 {
		t.Fatalf("the tree has %d top rows", len(items))
	}
	usr := items[1]
	if usr.Text != "usr" {
		t.Fatalf("row 1 is %q", usr.Text)
	}
	if usr.IsLeaf() {
		t.Error("usr says it is a leaf; the bundle said it has a child")
	}
	if items[0].IsLeaf() != true {
		t.Error("etc says it has children; the bundle said it has none")
	}

	// Opening it reaches the child, which is what makes this a tree rather than a
	// flat list with a twisty drawn on it.
	tv.ExpandItem(usr)
	if got, want := strings.Join(captionsOfTree(tv), " "), "etc usr local"; got != want {
		t.Errorf("with usr open the tree reads\n  %s\nwant\n  %s", got, want)
	}
}

// And `Source` still answers what the caller handed in, rather than the tree the
// view grew out of it -- a caller asking what it gave is asking about its own
// source.
func TestAGrownTreeStillReportsTheSourceItWasGiven(t *testing.T) {
	s := storeHolding(t, "files", filesBundle)
	src, err := trinkets.LookupSourceOn(onStore(t, s), "bundle:files")
	if err != nil {
		t.Fatalf("loading it: %v", err)
	}
	tv := trinkets.NewTreeView()
	tv.SetSource(src)
	if tv.Source() != src {
		t.Error("it reports a source the caller never handed it")
	}
}

// captionsOfTree is what a tree draws, in order.
func captionsOfTree(tv *trinkets.TreeView) []string {
	var out []string
	var walk func(items []*trinkets.TreeItem)
	walk = func(items []*trinkets.TreeItem) {
		for _, it := range items {
			out = append(out, it.Text)
			if it.Expanded {
				walk(it.Children)
			}
		}
	}
	walk(tv.RootItems())
	return out
}

// **An empty block says nothing**, rather than an empty hint that reads as one and
// then gets reported as contradictory. A member written and left blank is a member
// somebody is still thinking about.
func TestAnEmptyTreeBlockSaysNothing(t *testing.T) {
	s := storeHolding(t, "blank", `(
  _bundle: ( key: "blank", version: "1.0.0", tree: () ),
  ("a record")
)`)
	loaded, err := s.LoadBundle("blank", "", nil)
	if err != nil {
		t.Fatalf("it was refused: %v", err)
	}
	if _, said := serval.TreeHintOf(loaded.Source); said {
		t.Error("an empty block was heard as a hint")
	}
	if len(loaded.Trouble) != 0 {
		t.Errorf("it complained: %v", loaded.Trouble)
	}
}

// A bundle that INCLUDES something says its shape too. The hint belongs to the
// assembled layer, which is what a reader is handed -- and a leaf bundle and a
// composed one become different Go types, so one saying so proves nothing about
// the other.
func TestAComposedBundleSaysItsShapeToo(t *testing.T) {
	s := shelves(t)
	stock(t, s, "leaf-1.0.0", `(_bundle: (key: "leaf", version: "1.0.0"), ("a leaf record"))`)
	stock(t, s, "over-1.0.0", `(
  _bundle: (
    key: "over", version: "1.0.0",
    includes: ( leaf: ">= 1.0.0" ),
    tree: ( parent: ".up", label: ".name" )
  ),
  ( name: "its own", up: 0 )
)`)
	loaded, err := s.LoadBundle("over", "", nil)
	if err != nil {
		t.Fatalf("loading it: %v", err)
	}
	if len(loaded.Trouble) != 0 {
		t.Fatalf("it complained: %v", loaded.Trouble)
	}
	hint, said := serval.TreeHintOf(loaded.Source)
	if !said {
		t.Fatal("a bundle with includes said what its records are, and the layer it " +
			"became did not carry it")
	}
	if hint.Parent != ".up" || hint.Label != ".name" {
		t.Errorf("it says %+v", hint)
	}
}

// **A load's complaints reach the display's log.**
//
// They are reports and not refusals -- the bundle loaded -- so nothing on the wire
// carries them and nothing in the window shows them. Until this they went nowhere
// at all, which is indistinguishable from nothing having gone wrong.
//
// What is checked here is the mapping and the floor under it: the Event Viewer's
// Key is the name that was being loaded and its Detail is what the loader said. The
// line that calls this sits in findSource, one statement after the load.
func TestABundlesComplaintsReachTheDisplaysLog(t *testing.T) {
	s := storeHolding(t, "muddle", `(
  _bundle: (
    key: "muddle", version: "1.0.0",
    tree: ( parent: "up", location: "where", delimiter: "/" )
  ),
  ("a record")
)`)
	loaded, err := s.LoadBundle("muddle", "", nil)
	if err != nil {
		t.Fatalf("it was refused: %v", err)
	}
	if len(loaded.Trouble) == 0 {
		t.Fatal("this bundle was meant to load with something to say about it")
	}

	desktop := trinkets.NewDesktop()
	c := &conn{server: &Server{desktop: desktop}}
	c.report("bundle:muddle", loaded.Trouble)

	got := desktop.Reported()
	if len(got) != len(loaded.Trouble) {
		t.Fatalf("the desktop holds %d of %d complaints: %v",
			len(got), len(loaded.Trouble), got)
	}
	if !strings.HasPrefix(got[0], "bundle:muddle: ") {
		t.Errorf("it is filed as %q, want the name it was loaded under", got[0])
	}
	if !strings.Contains(got[0], "two ways down") {
		t.Errorf("it reads %q, want what the loader said", got[0])
	}

	// A connection with no desktop behind it -- which is every test that builds one
	// by hand -- reports to nobody rather than falling over.
	(&conn{server: &Server{}}).report("bundle:muddle", loaded.Trouble)
}

// And the same thing through the door a statement actually comes in at: a name on
// a connection, resolved against that connection's own store.
//
// This is the line itself rather than the mapping -- findSource loads the bundle
// and reports what the load had to say -- so a complaint dropped there is caught
// here rather than in a review.
func TestFindingABundleReportsWhatTheLoadSaid(t *testing.T) {
	cfg := tempConfig(t) // XDG_CONFIG_HOME, so the folders below are this test's
	known := newKnownStore(filepath.Join(cfg, "known"))
	if err := known.admitted("sha256:aaa", "Papers App", "Test Host", noon); err != nil {
		t.Fatal(err)
	}
	host, item := known.folders("sha256:aaa", "Papers App")
	if host == "" || item == "" {
		t.Fatalf("the client was admitted and keeps nothing: host=%q item=%q", host, item)
	}
	stock(t, newAppStore(host, item), "muddle-1.0.0", `(
  _bundle: (
    key: "muddle", version: "1.0.0",
    tree: ( parent: "up", location: "where", delimiter: "/" )
  ),
  ("a record")
)`)

	desktop := trinkets.NewDesktop()
	c := &conn{
		server:   &Server{desktop: desktop, known: known},
		identity: "sha256:aaa",
		appName:  "Papers App",
	}

	// The bundle loads: a hint that cannot mean what it says is a report, not a
	// refusal, and the records are all still there.
	src, err := c.findSource("bundle:muddle")
	if err != nil {
		t.Fatalf("the bundle was refused: %v", err)
	}
	if src == nil {
		t.Fatal("it loaded no source")
	}

	// And what it had to say is in the display's log, against the name that was
	// asked for.
	got := desktop.Reported()
	if len(got) == 0 {
		t.Fatal("the load had something to say and the display holds nothing")
	}
	if !strings.HasPrefix(got[0], "bundle:muddle: ") {
		t.Errorf("it is filed as %q, want the name the statement used", got[0])
	}
	if !strings.Contains(got[0], "two ways down") {
		t.Errorf("it reads %q, want what the loader said", got[0])
	}
}
