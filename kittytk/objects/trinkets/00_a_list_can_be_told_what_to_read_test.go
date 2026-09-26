package trinkets

// Naming a source, and saying which field a row shows against which it means.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/phroun/serval"
)

func namedRows(n int) *serval.ListSource {
	rows := make([]serval.Row, n)
	for i := range rows {
		rows[i] = serval.NewRow(serval.NewText(fmt.Sprintf("k%02d", i)), serval.Record{
			serval.Named("subject", fmt.Sprintf("message %d", i)),
			serval.Named("id", int64(1000+i)),
			serval.Named(rowDisplay, fmt.Sprintf("row %d", i)),
		})
	}
	return serval.NewListSource(rows)
}

// A registered name is what a list asks for, so whatever made the source and
// whatever reads it need not know about each other.
func TestAListReadsASourceByName(t *testing.T) {
	RegisterSource("test.inbox", namedRows(8))
	defer UnregisterSource("test.inbox")

	l := NewListView()
	if err := l.SetSourceByName("source:test.inbox"); err != nil {
		t.Fatalf("it would not read the source: %v", err)
	}
	if l.Count() != 8 {
		t.Errorf("it counts %d rows, want 8", l.Count())
	}
	if got := l.Item(2); got == nil || got.Text != "row 2" {
		t.Errorf("row 2 is %v", got)
	}
}

// A name nothing is registered under is refused, and says so rather than
// leaving a list quietly empty.
func TestAnUnregisteredNameIsRefused(t *testing.T) {
	l := NewListView()
	err := l.SetSourceByName("source:nobody.registered.this")
	if err == nil {
		t.Fatal("it took a name nothing stands for")
	}
	if !strings.Contains(err.Error(), "nobody.registered.this") {
		t.Errorf("the refusal does not name what was asked for: %v", err)
	}
	// And the list is untouched, still reading its own items.
	if l.Source() != nil {
		t.Error("a refused name left a source behind")
	}
}

// Unregistering a name does not take the source away from a list already
// reading it: a trinket holds the source, not the name it was found under.
func TestUnregisteringDoesNotTakeASourceAway(t *testing.T) {
	RegisterSource("test.going", namedRows(5))
	l := NewListView()
	if err := l.SetSourceByName("source:test.going"); err != nil {
		t.Fatal(err)
	}
	UnregisterSource("test.going")

	if l.Count() != 5 {
		t.Errorf("after the name went the list counts %d rows, want 5", l.Count())
	}
	// But the name no longer stands for anything.
	if _, err := LookupSource("source:test.going"); err == nil {
		t.Error("the name still stands for something")
	}
}

// A bundle is found by a loader the display installs, because assembling one
// means reaching a store and a store is not this package's.
func TestABundleIsFoundThroughTheInstalledLoader(t *testing.T) {
	var askedKey, askedVersion string
	SetBundleLoader(func(key, version string) (serval.Source, error) {
		askedKey, askedVersion = key, version
		return namedRows(4), nil
	})
	defer SetBundleLoader(nil)

	l := NewListView()
	if err := l.SetSourceByName("bundle:objectLibrary@2.1.0"); err != nil {
		t.Fatalf("it would not read the bundle: %v", err)
	}
	if askedKey != "objectLibrary" || askedVersion != "2.1.0" {
		t.Errorf("it asked for %q at %q", askedKey, askedVersion)
	}
	if l.Count() != 4 {
		t.Errorf("it counts %d rows", l.Count())
	}

	// The bare form means a bundle too, and no version means the newest there is.
	if err := l.SetSourceByName("objectLibrary"); err != nil {
		t.Fatal(err)
	}
	if askedKey != "objectLibrary" || askedVersion != "" {
		t.Errorf("the bare form asked for %q at %q", askedKey, askedVersion)
	}
}

// With no loader installed a bundle name says plainly that nothing here can find
// one, rather than failing as though the bundle were missing.
func TestWithNoLoaderABundleNameSaysSo(t *testing.T) {
	SetBundleLoader(nil)
	l := NewListView()
	err := l.SetSourceByName("bundle:objectLibrary")
	if err == nil {
		t.Fatal("it found a bundle with nothing installed to find one")
	}
	if !strings.Contains(err.Error(), "knows how to find") {
		t.Errorf("the refusal reads as a missing bundle: %v", err)
	}
}

// A name that names nothing at all, and one with no key in it.
//
// With a loader INSTALLED, so that a bundle name failing proves the name was
// refused here rather than merely reaching a loader that was not there. Without
// that these pass whichever guard fires, and would say nothing about either.
func TestANameHasToNameSomething(t *testing.T) {
	asked := 0
	SetBundleLoader(func(key, version string) (serval.Source, error) {
		asked++
		return namedRows(1), nil
	})
	defer SetBundleLoader(nil)

	for _, name := range []string{"", "   ", "bundle:", "source:"} {
		if _, err := LookupSource(name); err == nil {
			t.Errorf("%q was taken as a name", name)
		}
	}
	if asked != 0 {
		t.Errorf("a name with no key reached the loader %d times", asked)
	}
}

// Room around a name is room, not part of it. A statement written with a space
// after the quote names the same thing as one without.
func TestRoomAroundANameIsNotPartOfIt(t *testing.T) {
	RegisterSource("test.spaced", namedRows(3))
	defer UnregisterSource("test.spaced")

	if _, err := LookupSource("  source:test.spaced  "); err != nil {
		t.Errorf("a name with room around it was refused: %v", err)
	}

	var asked string
	SetBundleLoader(func(key, version string) (serval.Source, error) {
		asked = key
		return namedRows(1), nil
	})
	defer SetBundleLoader(nil)
	if _, err := LookupSource("  objectLibrary  "); err != nil {
		t.Fatalf("a bundle name with room around it was refused: %v", err)
	}
	if asked != "objectLibrary" {
		t.Errorf("it asked for %q, want the name without the room", asked)
	}
}

// What a row SHOWS and what it MEANS can be two different fields, which is what
// a list over a delimited file needs: the column a reader sees and the column
// the program hands back are not always the same one.
func TestARowCanShowOneFieldAndMeanAnother(t *testing.T) {
	RegisterSource("test.mail", namedRows(6))
	defer UnregisterSource("test.mail")

	l := NewListView()
	if err := l.SetSourceByName("source:test.mail"); err != nil {
		t.Fatal(err)
	}
	l.SetFields("subject", "id")

	if got := l.Item(3); got == nil || got.Text != "message 3" {
		t.Errorf("row 3 shows %v, want the subject", got)
	}
	v := l.ValueAt(3)
	if v == nil || !v.IsInt || v.Int != 1003 {
		t.Errorf("row 3 means %v, want 1003", v)
	}
}

// They are ONE field until somebody says otherwise, so a plain list means what
// it shows and a caller need not know which kind of list it is asking.
func TestAPlainListMeansWhatItShows(t *testing.T) {
	l := filled(4)
	l.extent(0, 4)

	if got := l.Item(2); got == nil || got.Text != "item 2" {
		t.Fatalf("row 2 shows %v", got)
	}
	v := l.ValueAt(2)
	if v == nil || v.Str != "item 2" {
		t.Errorf("row 2 means %v, want what it shows", v)
	}
}

// Saying which field to show drops what was read under the old one, or the list
// would go on showing the field it was told to stop showing.
func TestChangingTheFieldRereadsTheRows(t *testing.T) {
	RegisterSource("test.swap", namedRows(6))
	defer UnregisterSource("test.swap")

	l := NewListView()
	if err := l.SetSourceByName("source:test.swap"); err != nil {
		t.Fatal(err)
	}
	if got := l.Item(1); got == nil || got.Text != "row 1" {
		t.Fatalf("row 1 shows %v to begin with", got)
	}

	l.SetFields("subject", "")
	if got := l.Item(1); got == nil || got.Text != "message 1" {
		t.Errorf("after naming another field row 1 shows %v", got)
	}
}
