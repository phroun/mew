package display

// A bundle is a psl that says so. The store notices on the way past and can
// say afterwards which item holds which bundle -- and nothing more than that.

import (
	"os"
	"path/filepath"
	"testing"
)

// figaro is a bundle as the format writes one: metadata in the keyed space,
// records in the ordered one.
const figaro = `(
  _bundle: ( key: "figaro", author: "Jeffrey R. Day", version: "0.1.0" ),
  _hash: "deadbeef",
  ("ordered_record1", a: "one"),
  ("ordered_record2", a: "two")
)`

// shelves is a whole app store -- both halves -- in a place of this test's own.
func shelves(t *testing.T) *appStore {
	t.Helper()
	dir := t.TempDir()
	return &appStore{
		kept:   newStoreDir(filepath.Join(dir, "data")),
		cached: newStoreDir(filepath.Join(dir, "cache")),
	}
}

// only is the one entry a lookup was expected to find.
func only(t *testing.T, got []bundleEntry) bundleEntry {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("expected one bundle, got %d: %+v", len(got), got)
	}
	return got[0]
}

// The whole of it: written under a key of the app's choosing, found afterwards
// by the name and version it states inside itself.
func TestABundleIsFoundByWhatItCallsItself(t *testing.T) {
	s := shelves(t)
	if _, err := s.put("libraryV1", "psl", []byte(figaro)); err != nil {
		t.Fatal(err)
	}

	e := only(t, s.bundlesNamed("figaro", "0.1.0"))
	if e.storeKey != "libraryV1" {
		t.Errorf("figaro 0.1.0 is held by %q", e.storeKey)
	}
	if e.hash != "deadbeef" {
		t.Errorf("its hash read as %q", e.hash)
	}

	// And by the hash, which names the content rather than the name.
	if h := only(t, s.bundlesHashed("deadbeef")); h.storeKey != "libraryV1" {
		t.Errorf("the hash found %q", h.storeKey)
	}

	// The store key is the app's own word and says nothing about the bundle:
	// asking for it by the name it is FILED under finds nothing.
	if got := s.bundlesNamed("libraryV1", "0.1.0"); len(got) != 0 {
		t.Errorf("the store key answered as a bundle key: %+v", got)
	}
}

// A psl is a document like any other until it says `_bundle`.
func TestAPlainPslIsNotABundle(t *testing.T) {
	s := shelves(t)
	for _, c := range []struct{ key, text string }{
		{"plain", `( ("a record", a: 1), other: "thing" )`},
		{"broken", `( _bundle: (key: "x"` + " unclosed"},
		{"nameless", `( _bundle: ( author: "nobody" ), ("r") )`},
	} {
		if _, err := s.put(c.key, "psl", []byte(c.text)); err != nil {
			t.Fatalf("%s: %v", c.key, err)
		}
	}
	if got := s.kept.bundles(); len(got) != 0 {
		t.Errorf("something that is not a bundle was indexed: %+v", got)
	}
}

// The index answers a question asked by name, so a bundle no include could
// name is turned away at the reading rather than written down and skipped on
// the way back: an index line nothing will ever read is still a line.
func TestABundleWithNoNameIsNotOne(t *testing.T) {
	nameless := `( _bundle: ( author: "nobody", version: "0.1.0" ), ("r") )`
	if e, ok := bundleOf("somewhere", []byte(nameless)); ok {
		t.Errorf("a bundle with no key read as %+v", e)
	}
	// And the same document with one is.
	named := `( _bundle: ( key: "named", version: "0.1.0" ), ("r") )`
	if _, ok := bundleOf("somewhere", []byte(named)); !ok {
		t.Error("a bundle with a key did not read as one")
	}
}

// The TYPE is what says a document is a psl. Bytes that would parse as a
// bundle are not one where the app called them something else, because what an
// item is is what the app said it is and not what it happens to look like.
func TestBundleBytesUnderAnotherTypeAreNotABundle(t *testing.T) {
	s := shelves(t)
	if _, err := s.put("notes", "txt", []byte(figaro)); err != nil {
		t.Fatal(err)
	}
	if got := s.bundlesNamed("figaro", "0.1.0"); len(got) != 0 {
		t.Errorf("a txt item was indexed as a bundle: %+v", got)
	}
}

// Written in pieces, it is not a bundle until the piece that completes it --
// and then it is, with nobody having had to say so.
func TestABundleAppendedInPiecesIsFoundWhenItCloses(t *testing.T) {
	s := shelves(t)
	head, tail := figaro[:40], figaro[40:]

	if _, err := s.appendTo("growing", "psl", []byte(head)); err != nil {
		t.Fatal(err)
	}
	if got := s.bundlesNamed("figaro", "0.1.0"); len(got) != 0 {
		t.Fatalf("a half-written bundle was indexed: %+v", got)
	}

	if _, err := s.appendTo("growing", "psl", []byte(tail)); err != nil {
		t.Fatal(err)
	}
	if e := only(t, s.bundlesNamed("figaro", "0.1.0")); e.storeKey != "growing" {
		t.Errorf("the finished bundle is held by %q", e.storeKey)
	}
}

// An item that stops being a bundle loses its entry, however it stopped.
func TestWhatIsNoLongerABundleIsNoLongerIndexed(t *testing.T) {
	for _, c := range []struct {
		name string
		stop func(*appStore) error
	}{
		{"dropped", func(s *appStore) error { return s.drop("held") }},
		{"replaced with a plain psl", func(s *appStore) error {
			_, err := s.put("held", "psl", []byte(`( ("just a record") )`))
			return err
		}},
		{"replaced with another type", func(s *appStore) error {
			_, err := s.put("held", "txt", []byte("words"))
			return err
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := shelves(t)
			if _, err := s.put("held", "psl", []byte(figaro)); err != nil {
				t.Fatal(err)
			}
			if len(s.bundlesNamed("figaro", "0.1.0")) != 1 {
				t.Fatal("it was not indexed to begin with")
			}
			if err := c.stop(s); err != nil {
				t.Fatal(err)
			}
			if got := s.bundlesNamed("figaro", "0.1.0"); len(got) != 0 {
				t.Errorf("it is still indexed as %+v", got)
			}
		})
	}
}

// Two items claiming one bundle at one version is the author's mistake, and
// the store hands back both rather than choosing.
func TestTwoItemsClaimingOneBundleAreBothReported(t *testing.T) {
	s := shelves(t)
	for _, key := range []string{"first", "second"} {
		if _, err := s.put(key, "psl", []byte(figaro)); err != nil {
			t.Fatal(err)
		}
	}
	got := s.bundlesNamed("figaro", "0.1.0")
	if len(got) != 2 {
		t.Fatalf("expected both, got %+v", got)
	}
	if got[0].storeKey == got[1].storeKey {
		t.Errorf("one item was reported twice: %+v", got)
	}
}

// The cache is where the fresher copy lands, so at the moment of asking it is
// the answer -- and the kept one is not also returned, because that would read
// as the collision it is not.
func TestACachedBundleShadowsTheKeptOne(t *testing.T) {
	s := shelves(t)
	if _, err := s.put("library", "psl", []byte(figaro)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.put("#library", "psl", []byte(figaro)); err != nil {
		t.Fatal(err)
	}
	if e := only(t, s.bundlesNamed("figaro", "0.1.0")); e.storeKey != "#library" {
		t.Errorf("the answer was %q, not the cached copy", e.storeKey)
	}

	// The two halves are indexed apart, so a cache sweep cannot take the kept
	// one with it -- and what it leaves is what answers next time.
	if err := s.drop("#library"); err != nil {
		t.Fatal(err)
	}
	if e := only(t, s.bundlesNamed("figaro", "0.1.0")); e.storeKey != "library" {
		t.Errorf("after the cache went, the answer was %q", e.storeKey)
	}
}

// A version is part of what an include names, so two of them are two bundles.
func TestTwoVersionsOfOneBundleAreTwoBundles(t *testing.T) {
	s := shelves(t)
	older := `( _bundle: ( key: "figaro", version: "0.0.9" ), ("r") )`
	if _, err := s.put("new", "psl", []byte(figaro)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.put("old", "psl", []byte(older)); err != nil {
		t.Fatal(err)
	}
	if e := only(t, s.bundlesNamed("figaro", "0.1.0")); e.storeKey != "new" {
		t.Errorf("0.1.0 is held by %q", e.storeKey)
	}
	if e := only(t, s.bundlesNamed("figaro", "0.0.9")); e.storeKey != "old" {
		t.Errorf("0.0.9 is held by %q", e.storeKey)
	}
	// A version it does not state is not a version it answers to.
	if got := s.bundlesNamed("figaro", "9.9.9"); len(got) != 0 {
		t.Errorf("a version nothing states was found: %+v", got)
	}
}

// The index is derived, so a store that has never had one -- which is every
// store that existed before this was written -- still answers.
func TestTheIndexIsBuiltAgainWhenItIsGone(t *testing.T) {
	s := shelves(t)
	if _, err := s.put("library", "psl", []byte(figaro)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.kept.bundleIndexPath()); err != nil {
		t.Fatal(err)
	}
	if e := only(t, s.bundlesNamed("figaro", "0.1.0")); e.storeKey != "library" {
		t.Errorf("after the index went, the answer was %q", e.storeKey)
	}
}

// A name or a version is written down as it stands, and read back whole.
func TestAKeyWithASpaceInItSurvivesTheIndex(t *testing.T) {
	s := shelves(t)
	spaced := `( _bundle: ( key: "the figaro library", version: "0.1.0 beta" ) )`
	if _, err := s.put("spaced", "psl", []byte(spaced)); err != nil {
		t.Fatal(err)
	}
	if e := only(t, s.bundlesNamed("the figaro library", "0.1.0 beta")); e.storeKey != "spaced" {
		t.Errorf("it came back as %q", e.storeKey)
	}
}

// A bare word and a quoted string name one bundle: the distinction PSL draws
// between a symbol and a string is not one a NAME makes.
func TestABareNameAndAQuotedOneAreTheSameBundle(t *testing.T) {
	s := shelves(t)
	bare := `( _bundle: ( key: figaro, version: "0.1.0" ) )`
	if _, err := s.put("bare", "psl", []byte(bare)); err != nil {
		t.Fatal(err)
	}
	if e := only(t, s.bundlesNamed("figaro", "0.1.0")); e.storeKey != "bare" {
		t.Errorf("a bare key came back as %q", e.storeKey)
	}
}
