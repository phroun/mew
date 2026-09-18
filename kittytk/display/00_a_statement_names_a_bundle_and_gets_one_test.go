package display

// The whole path, end to end: a bundle on disk, a statement naming it, and a
// trinket reading its records.
//
// Every piece of this existed before and none of them were joined. The store
// held bundles, the loader assembled them, the trinket took a name -- and
// nothing turned the name into the store. This is the join, and it is checked by
// walking all of it rather than by any part of it saying it would work.

import (
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
