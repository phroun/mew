package wire

// The statement the display says a complaint with.
//
// A refusal is what a batch is answered WITH, in place of the reply that would
// have named what it made. This is not that: the statements ran, the objects
// exist, and something along the way is worth the author knowing. It travels just
// before the reply, so it is part of the answer to the batch that caused it.
//
// testdata/trouble.wire is the text three client libraries read; `c/interop` and
// `python/tests` answer the same file.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// troubleFrom takes one line apart and writes it back from the structure, which
// is what makes a part dropped on the way in a part missing on the way out.
func troubleFrom(text string) (string, error) {
	script, err := Parse(text)
	if err != nil {
		return "", err
	}
	if len(script.Statements) != 1 {
		return "", errNotOne
	}
	t, err := DecodeTrouble(script.Statements[0])
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(EncodeTrouble(t), TroubleVerb+" "), nil
}

var errNotOne = errTrouble("not one statement")

type errTrouble string

func (e errTrouble) Error() string { return string(e) }

func TestTheTroubleCorpusIsAnswered(t *testing.T) {
	path := filepath.Join("..", "testdata", "trouble.wire")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var in string
	var line, cases int
	for i, raw := range strings.Split(string(data), "\n") {
		text := strings.TrimSpace(raw)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		verb, rest, _ := strings.Cut(text, " ")
		switch verb {
		case TroubleVerb:
			if in != "" {
				t.Fatalf("%s:%d: a case with no answer", path, line)
			}
			in, line = text, i+1
		case "want", "bad":
			if in == "" {
				t.Fatalf("%s:%d: an answer with no case", path, i+1)
			}
			got, err := troubleFrom(in)
			cases++
			if verb == "bad" {
				if err == nil {
					t.Errorf("%s:%d: %s was read as a complaint", path, line, in)
				}
				in = ""
				continue
			}
			if err != nil {
				t.Errorf("%s:%d: %s was refused: %v", path, line, in, err)
			} else if got != rest {
				t.Errorf("%s:%d: %s came back as\n  %s\nwant\n  %s", path, line, in, got, rest)
			}
			in = ""
		default:
			t.Fatalf("%s:%d: %q is not a corpus line", path, i+1, verb)
		}
	}
	if in != "" {
		t.Fatalf("%s: a case with no answer", path)
	}
	if cases < 12 {
		t.Errorf("the corpus holds %d cases, which is fewer than it did", cases)
	}
}

// A complaint rides the reply it belongs to, so what a reader ends up holding is
// the reply -- with the complaints on it.
func TestAReplyCarriesWhatWentWrong(t *testing.T) {
	r := &Reply{
		IDs: map[string]uint64{"lv": 17},
		Trouble: []Trouble{
			{About: "bundle:papers", Text: "papers/extra: no such include"},
		},
	}
	if got, want := EncodeReply(r), "reply lv=17"; got != want {
		t.Errorf("the reply line reads %q, want %q -- the complaints are their own"+
			" statements and not arguments on this one", got, want)
	}
}
