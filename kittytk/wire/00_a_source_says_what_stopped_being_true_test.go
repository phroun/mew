package wire

// A source speaking first.
//
// Everything else an application says answers something the display asked. This
// does not: only the source knows its records moved, and nothing on the other end
// can find out. Invalidation is told, never decided -- so the assertions here are
// about what a notice must be able to say, and about the two ways of saying less
// than happened, which is the one mistake that is silent and permanent.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phroun/serval"
)

// staleOf writes a notice, parses the statement back, and hands over what came
// out -- so nothing here can pass by agreeing with itself about a struct.
func staleOf(t *testing.T, s *Stale) *Stale {
	t.Helper()
	text := s.Encode()
	got, err := staleFrom(text)
	if err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	return got
}

// staleFrom parses one statement's worth of text.
func staleFrom(text string) (*Stale, error) {
	script, err := Parse(text)
	if err != nil {
		return nil, err
	}
	if len(script.Statements) != 1 {
		return nil, errCount(len(script.Statements))
	}
	if got := script.Statements[0].Verb; got != StaleVerb {
		return nil, errVerb(got)
	}
	return ParseStale(script.Statements[0].Args)
}

type errCount int

func (e errCount) Error() string { return "not one statement" }

type errVerb string

func (e errVerb) Error() string { return "not a stale: " + string(e) }

// The four reasons survive the trip, because what each one COSTS is different
// and a notice that lost the word would cost the most every time.
func TestTheReasonSurvivesTheTrip(t *testing.T) {
	for _, how := range []serval.Change{serval.Added, serval.Removed, serval.Replaced, serval.Altered} {
		got := staleOf(t, &Stale{Source: "papers", ID: NewInt(42), How: how})
		if got.How != how {
			t.Errorf("%s came back as %s", how, got.How)
		}
		if got.Source != "papers" {
			t.Errorf("it is about %q", got.Source)
		}
		if got.ID == nil || got.ID.Int != 42 {
			t.Errorf("it names %v", got.ID)
		}
	}
}

// **A removal is cheaper than a move, so the two must not collapse.** A record
// that has LEFT the sequence leaves everything between a run's ends still there,
// and the completeness claim survives; one that may have MOVED takes the run with
// it. Same operation on the links, told apart by this word alone.
func TestARemovalIsNotAReplacement(t *testing.T) {
	gone := staleOf(t, &Stale{Source: "papers", ID: NewInt(1), How: serval.Removed})
	moved := staleOf(t, &Stale{Source: "papers", ID: NewInt(1), How: serval.Replaced})
	if gone.How == moved.How {
		t.Fatal("a removal and a replacement read the same")
	}
	if gone.Notice().Change != serval.Removed || moved.Notice().Change != serval.Replaced {
		t.Errorf("as notices they are %s and %s", gone.Notice().Change, moved.Notice().Change)
	}
}

// Saying less than happened is the one mistake that cannot be recovered from, so
// every default leans wide: no id= is every record, and an unsaid reason is the
// widest thing sayable about one.
func TestWhatIsLeftOutReadsAsTheWidestThing(t *testing.T) {
	whole, err := staleFrom(`stale source="papers"`)
	if err != nil {
		t.Fatal(err)
	}
	if whole.ID != nil {
		t.Errorf("a notice naming no record named %v", whole.ID)
	}
	if whole.How != serval.Replaced {
		t.Errorf("an unsaid reason reads as %s, want the widest claim about a record", whole.How)
	}
	// And the notice it becomes has no extent, which is every record it is
	// answered against.
	if n := whole.Notice(); n.First != nil || n.Last != nil {
		t.Errorf("it came out with an extent: %v..%v", n.First, n.Last)
	}

	// An alteration naming no field is every field, which is the same leaning.
	any, err := staleFrom(`stale source="papers" id=42 how=altered`)
	if err != nil {
		t.Fatal(err)
	}
	if len(any.Fields) != 0 {
		t.Errorf("it named %v", any.Fields)
	}
}

// A field with no extent is the CHEAP case rather than the weak one: where the
// named field decides nothing it costs the order nothing, and where it does it
// costs one field per record. A source able to say this should.
func TestAFieldWithNoRecordIsSayable(t *testing.T) {
	got, err := staleFrom(`stale source="papers" how=altered fields={ size }`)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != nil {
		t.Errorf("it named a record: %v", got.ID)
	}
	if strings.Join(got.Fields, ",") != "size" {
		t.Errorf("it named %v", got.Fields)
	}
	if got.Notice().Fields[0] != "size" {
		t.Errorf("the notice names %v", got.Notice().Fields)
	}
}

// **`fields=` beside any reason but an alteration is a statement contradicting
// itself**, and it is refused rather than half-read. On every other reason the
// values are gone entire, so a list of them says the opposite of the word next to
// it -- and quietly dropping one half of a contradiction is how a source comes to
// believe it said something it did not.
func TestAContradictionIsRefusedRatherThanHalfRead(t *testing.T) {
	for _, bad := range []string{
		`stale source="papers" id=42 how=removed fields={ size }`,
		`stale source="papers" id=42 how=replaced fields={ size }`,
		`stale source="papers" id=42 how=added fields={ size }`,
		`stale source="papers" id=42 fields={ size }`, // unsaid reads as replaced
	} {
		if _, err := staleFrom(bad); err == nil {
			t.Errorf("%s was read as a notice", bad)
		}
	}
}

// It names fields, not their values. A value here would read as an assertion
// about what the record now holds, and a notice never makes one: it says what has
// stopped being true, and causes forgetting rather than traffic.
func TestANoticeCarriesNoValues(t *testing.T) {
	if _, err := staleFrom(`stale source="papers" id=42 how=altered fields={ size 1024 }`); err == nil {
		t.Error("a notice carried a value")
	}
}

// A notice that does not say which source is a notice about all of them, and an
// application may serve several.
func TestANoticeSaysWhichSource(t *testing.T) {
	for _, bad := range []string{
		`stale id=42 how=removed`,
		`stale source=papers id=42`, // a name, not a word
		`stale source=7 id=42`,
	} {
		if _, err := staleFrom(bad); err == nil {
			t.Errorf("%s was read as a notice", bad)
		}
	}
}

// Arguments it does not know are refused rather than ignored. A source reaching
// for a word this does not have is a source whose meaning is being dropped, and
// it should hear so while it can still be changed.
func TestAnArgumentItDoesNotKnowIsRefused(t *testing.T) {
	for _, bad := range []string{
		`stale source="papers" id=42 how=removed reason="deleted"`,
		`stale source="papers" id=42 how=removed complete`,
		`stale source="papers" first=1 last=9`, // the extent, which is not built
	} {
		if _, err := staleFrom(bad); err == nil {
			t.Errorf("%s was read as a notice", bad)
		}
	}
}

// --- the shared corpus ----------------------------------------------------

// testdata/stale.wire is the text three client libraries read. The Go side
// answers it here; `c/interop` and `python/tests` answer the same file.
func TestTheStaleCorpusIsAnswered(t *testing.T) {
	path := filepath.Join("..", "testdata", "stale.wire")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var in string
	var line int
	cases := 0
	for i, raw := range strings.Split(string(data), "\n") {
		text := strings.TrimSpace(raw)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		verb, rest, _ := strings.Cut(text, " ")
		switch verb {
		case StaleVerb:
			if in != "" {
				t.Fatalf("%s:%d: a case with no answer", path, line)
			}
			in, line = text, i+1
		case "want", "bad":
			if in == "" {
				t.Fatalf("%s:%d: a notice with no case", path, i+1)
			}
			cases++
			got, err := staleFrom(in)
			if verb == "bad" {
				if err == nil {
					t.Errorf("%s:%d: %s was read as a notice", path, line, in)
				}
				in = ""
				continue
			}
			if err != nil {
				t.Errorf("%s:%d: %s: %v", path, line, in, err)
				in = ""
				continue
			}
			// Written back from the structure, so a part dropped on the way in is
			// a part missing on the way out.
			if want := StaleVerb + " " + rest; got.Encode() != want {
				t.Errorf("%s:%d: %s\n  came out as %s\n  want       %s", path, line, in, got.Encode(), want)
			}
			in = ""
		default:
			t.Fatalf("%s:%d: %q is not a corpus line", path, i+1, verb)
		}
	}
	if in != "" {
		t.Fatalf("%s: a case with no answer", path)
	}
	if cases == 0 {
		t.Fatalf("%s: no cases", path)
	}
}
