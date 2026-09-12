package wire

// The Go side of the shared query corpus.
//
// testdata/query.wire holds the text a display sends and the structure it has
// to come out as. Go, C and Python each answer the same file with their own
// parser, so a client library that quietly drops a sort level or reads a
// filter differently fails a build here rather than producing a list that is
// subtly in the wrong order.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// queryCase is one corpus case: what arrived, and what it has to become.
type queryCase struct {
	line int
	kind string // "spec" or "fill"
	in   string
	want string
	bad  bool
}

// readQueryCorpus reads the shared file into cases.
func readQueryCorpus(t *testing.T) []queryCase {
	t.Helper()
	path := filepath.Join("..", "testdata", "query.wire")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []queryCase
	var open *queryCase
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		verb, rest, _ := strings.Cut(line, " ")
		switch verb {
		case "spec", "fill":
			if open != nil {
				t.Fatalf("%s:%d: a case with no answer", path, open.line)
			}
			cases = append(cases, queryCase{line: i + 1, kind: verb, in: rest})
			open = &cases[len(cases)-1]
		case "want":
			if open == nil {
				t.Fatalf("%s:%d: an answer with no case", path, i+1)
			}
			open.want = rest
			open = nil
		case "bad":
			if open == nil {
				t.Fatalf("%s:%d: an answer with no case", path, i+1)
			}
			open.bad = true
			open = nil
		default:
			t.Fatalf("%s:%d: %q is not a corpus line", path, i+1, verb)
		}
	}
	if open != nil {
		t.Fatalf("%s:%d: a case with no answer", path, open.line)
	}
	return cases
}

// answerQueryCase parses one case and renders the structure back out.
func answerQueryCase(c queryCase) (string, error) {
	script, err := Parse(c.kind + " " + c.in)
	if err != nil {
		return "", err
	}
	args := script.Statements[0].Args
	if c.kind == "spec" {
		spec, err := ParseSpec(args)
		if err != nil {
			return "", err
		}
		return spec.Encode(), nil
	}
	fill, err := ParseFill(args)
	if err != nil {
		return "", err
	}
	return fill.Encode(), nil
}

func TestTheQueryCorpusIsAnswered(t *testing.T) {
	cases := readQueryCorpus(t)
	if len(cases) < 40 {
		t.Fatalf("the corpus holds %d cases, which is fewer than it did", len(cases))
	}
	for _, c := range cases {
		got, err := answerQueryCase(c)
		switch {
		case c.bad && err == nil:
			t.Errorf("line %d: %s %s was taken as %q, and should have been refused",
				c.line, c.kind, c.in, got)
		case c.bad:
			// Refused, as the corpus says it must be.
		case err != nil:
			t.Errorf("line %d: %s %s was refused: %v", c.line, c.kind, c.in, err)
		case got != c.want:
			t.Errorf("line %d: %s %s\n  came out as %s\n  want        %s",
				c.line, c.kind, c.in, got, c.want)
		}
	}
}

// What comes out goes back in unchanged: the canonical spelling is one the
// parser reads to the same structure it was rendered from.
func TestTheCanonicalSpellingIsStable(t *testing.T) {
	for _, c := range readQueryCorpus(t) {
		if c.bad {
			continue
		}
		again, err := answerQueryCase(queryCase{kind: c.kind, in: c.want})
		if err != nil {
			t.Errorf("line %d: the canonical form %q would not parse: %v", c.line, c.want, err)
			continue
		}
		if again != c.want {
			t.Errorf("line %d: %q renders as %q the second time", c.line, c.want, again)
		}
	}
}
