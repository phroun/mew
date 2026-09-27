package display

// The example in docs/bundle-format.md, loaded.
//
// A format example that is only prose goes stale the first time the loader
// moves and nobody notices. This one is READ FROM THE DOCUMENT, so the document
// is a fixture: change what a bundle means without changing what the doc says
// and this fails.

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/phroun/serval"
)

const formatDoc = "../docs/bundle-format.md"

// documented is the first psl block in the format document, which is the
// example. The placeholder hash in it stands for "some bundle's content", and
// only a test knows what that hashes to, so the caller says.
func documented(t *testing.T, iconsHash string) string {
	t.Helper()
	text, err := os.ReadFile(formatDoc)
	if err != nil {
		t.Fatalf("the format document is what this tests: %v", err)
	}
	_, after, found := strings.Cut(string(text), "```psl\n")
	if !found {
		t.Fatalf("%s holds no psl block", formatDoc)
	}
	block, _, found := strings.Cut(after, "\n```")
	if !found {
		t.Fatalf("%s has a psl block that never ends", formatDoc)
	}
	return strings.Replace(block, strings.Repeat("0", 64), iconsHash, 1)
}

// The whole example, with everything it names present except the one include
// it says is optional.
func TestTheDocumentedBundleLoads(t *testing.T) {
	s := shelves(t)

	icons := doc("icons", "0.4.0", "", `, ("an icon")`)
	stock(t, s, "icons-0.4.0", icons)
	stock(t, s, "figaro-0.1.3", doc("figaro", "0.1.3", "",
		`, ("figaro first"), ("figaro second"), legacy: "to be deleted"`))
	stock(t, s, "grammar-2.4.0", doc("grammar", "2.4.0", "", `, ("a rule")`))
	stock(t, s, "drop-folder-0.2.0", doc("drop-folder", "0.2.0", "", `, ("a dropped thing")`))
	// `palette` is deliberately absent: the example says its absence is
	// survivable, and this is what proves the document means it.

	stock(t, s, "objectLibrary-2.1.0", documented(t, hashOf([]byte(icons))))

	live := Sources{"mail.incoming": serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(0), serval.Record{serval.Named("subject", "hello")}),
	})}

	got, err := s.LoadBundle("objectLibrary", "2.1.0", live)
	if err != nil {
		t.Fatalf("the documented bundle would not load: %v", err)
	}

	// One report, and it is the optional include that was never there.
	if len(got.Trouble) != 1 || got.Trouble[0].Alias != "palette" {
		t.Fatalf("it reported %v", got.Trouble)
	}

	keys := keysOf(t, got.Source)
	for _, want := range []string{
		"figaro/0",  // an included record, untouched
		"figaro/1",  // the one an amendment stood in for
		"grammar/0", // a bounded range
		"icons/0",   // pinned by hash
		"drops/0",   // an aliased bundle, optional and present
		"inbox/0",   // a registered source
		"0", "1",    // the bundle's own ordered records
		`"notes"`, // and its own keyed one
	} {
		if !has(keys, want) {
			t.Errorf("%s is missing from %v", want, keys)
		}
	}
	// The deleted one is gone, and nothing about the document is a record.
	for _, gone := range []string{"figaro/legacy", `"_bundle"`, `"_amendments"`} {
		if has(keys, gone) {
			t.Errorf("%s is still there: %v", gone, keys)
		}
	}
}

// Every rule the document states about where a bundle is filed, checked against
// the table it states them in.
func TestTheDocumentedFilingRuleHolds(t *testing.T) {
	text, err := os.ReadFile(formatDoc)
	if err != nil {
		t.Fatal(err)
	}
	body := string(text)

	for _, at := range []string{"objectLibrary", "objectLibrary-2.1.0",
		"objectLibrary-draft", "#objectLibrary"} {
		if !strings.Contains(body, "`"+at+"`") {
			t.Errorf("%s no longer shows %q as a filing that works", formatDoc, at)
		}
		if !filedAsItsOwnKey(at, "objectLibrary") {
			t.Errorf("the document says %q is indexed, and it is not", at)
		}
	}
	if filedAsItsOwnKey("scratch", "objectLibrary") {
		t.Error("the document says scratch is not indexed, and it is")
	}
}

// And the expressions the document tabulates read as it says they do.
func TestTheDocumentedExpressionsReadAsWritten(t *testing.T) {
	for _, c := range []struct {
		text  string
		check func(want) error
	}{
		{"1.4.2", func(w want) error {
			if w.pin != "1.4.2" {
				return fmt.Errorf("a bare version is not a pin")
			}
			return nil
		}},
		{">= 0.1.0", func(w want) error {
			if w.floor != "0.1.0" || w.upper != "" {
				return fmt.Errorf("a floor bounded something")
			}
			return nil
		}},
		{"< 3.0", func(w want) error {
			if w.upper != "3.0" {
				return fmt.Errorf("an upper bound was not read")
			}
			return nil
		}},
		{"1.4.2, optional", func(w want) error {
			if w.pin != "1.4.2" || !w.optional {
				return fmt.Errorf("optional did not ride a pin")
			}
			return nil
		}},
		{">= 0.1.0, < 3.0", func(w want) error {
			if w.floor != "0.1.0" || w.upper != "3.0" {
				return fmt.Errorf("a floor and a bound did not both land")
			}
			return nil
		}},
	} {
		w, err := parseWant("x", c.text)
		if err != nil {
			t.Errorf("%q: %v", c.text, err)
			continue
		}
		if err := c.check(w); err != nil {
			t.Errorf("%q: %v", c.text, err)
		}
	}

	// And the two the document says are refused.
	for _, text := range []string{"1.4.2, >= 2.0", "1.0, 2.0"} {
		if _, err := parseWant("x", text); err == nil {
			t.Errorf("%q was taken, and the document says it is refused", text)
		}
	}
}
