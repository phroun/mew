package wire

// The comparison core, against the corpus every implementation of it answers.
//
// The corpus is read with the wire parser itself, so a case is written the way
// a value is written anywhere else, and the Go, C and Python implementations
// are all answering the same questions from the same file.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestTheCorpusAnswersTheSameWayHere(t *testing.T) {
	path := filepath.Join("..", "testdata", "compare.wire")
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	script, err := Parse(string(text))
	if err != nil {
		t.Fatalf("reading the corpus: %v", err)
	}

	cases := 0
	for i, stmt := range script.Statements {
		if stmt.Verb == "" && len(stmt.Args) == 0 {
			continue // a blank line or a comment
		}
		if stmt.Verb != "case" {
			t.Errorf("statement %d is %q, not a case", i+1, stmt.Verb)
			continue
		}
		cases++
		a, b := corpusValue(t, stmt, "a"), corpusValue(t, stmt, "b")
		want, ok := corpusInt(stmt, "want")
		if !ok {
			t.Errorf("case %d has no want", i+1)
			continue
		}
		collation := corpusWord(stmt, "collate")

		if got := Compare(a, b, collation); got != want {
			t.Errorf("case %d: compare(%s, %s, %q) = %d, want %d",
				i+1, show(a), show(b), collation, got, want)
		}
		// Every case is its own mirror: swapping the two swaps the answer.
		if got := Compare(b, a, collation); got != -want {
			t.Errorf("case %d reversed: compare(%s, %s, %q) = %d, want %d",
				i+1, show(b), show(a), collation, got, -want)
		}
	}
	if cases < 50 {
		t.Errorf("the corpus answered %d cases, which is fewer than it holds", cases)
	}
}

// A value is equal to itself, whatever it is and whatever the collation.
func TestEveryValueIsEqualToItself(t *testing.T) {
	script, err := Parse(`case a=undefined
case a=nil
case a=true
case a=false
case a=0
case a=-17
case a=9007199254740993
case a=1.5
case a=apple
case a="apple"
case a=""
case a={ inner thing }`)
	if err != nil {
		t.Fatal(err)
	}
	for _, collation := range []string{CollateExact, CollateFold, CollateNatural, ""} {
		for i, stmt := range script.Statements {
			v := corpusValue(t, stmt, "a")
			if got := Compare(v, v, collation); got != 0 {
				t.Errorf("case %d under %q: a value is not equal to itself: %d",
					i+1, collation, got)
			}
		}
	}
}

// A blob is bytes and a quoted string is text, so they are different ranks
// even when they hold the same characters -- which the wire cannot say on
// receipt, so nothing but a locally built value reaches this.
func TestBytesAreNotAString(t *testing.T) {
	text := &Value{Kind: StringValue, Str: "ab"}
	blob := &Value{Kind: StringValue, Str: "ab", Blob: true}
	if got := Compare(text, blob, CollateExact); got != -1 {
		t.Errorf("a string against the same bytes = %d, want -1", got)
	}
	if got := Compare(blob, blob, CollateExact); got != 0 {
		t.Errorf("bytes against themselves = %d, want 0", got)
	}
	// Bytes compare unsigned, so a high byte is the larger.
	high := &Value{Kind: StringValue, Str: "\xff", Blob: true}
	low := &Value{Kind: StringValue, Str: "\x01", Blob: true}
	if got := Compare(low, high, CollateExact); got != -1 {
		t.Errorf("\\x01 against \\xff = %d, want -1", got)
	}
}

// A block has no order of its own, so two of them compare equal and the sort's
// last level decides -- rather than pretending one comes first.
func TestTwoBlocksHaveNoOrderBetweenThem(t *testing.T) {
	script, err := Parse("case a={ one } b={ two }")
	if err != nil {
		t.Fatal(err)
	}
	stmt := script.Statements[0]
	a, b := corpusValue(t, stmt, "a"), corpusValue(t, stmt, "b")
	if Rank(a) != RankUnordered {
		t.Fatalf("a block ranks %d, want the unordered rank", Rank(a))
	}
	if got := Compare(a, b, CollateExact); got != 0 {
		t.Errorf("two blocks compare %d, want 0", got)
	}
}

// A level settles only what the levels above it left equal, and a descending
// level turns over its own answer and nothing else.
func TestALevelSettlesOnlyWhatTheOnesAboveItLeftEqual(t *testing.T) {
	script, err := Parse(`case a=sales b=40000
case a=sales b=120000`)
	if err != nil {
		t.Fatal(err)
	}
	first := []*Value{
		corpusValue(t, script.Statements[0], "a"),
		corpusValue(t, script.Statements[0], "b"),
	}
	second := []*Value{
		corpusValue(t, script.Statements[1], "a"),
		corpusValue(t, script.Statements[1], "b"),
	}

	ascending := []Level{{}, {}}
	if got := CompareLevels(first, second, ascending); got != -1 {
		t.Errorf("equal on the first level, smaller on the second = %d, want -1", got)
	}

	descendingSecond := []Level{{}, {Descending: true}}
	if got := CompareLevels(first, second, descendingSecond); got != 1 {
		t.Errorf("the second level turned over = %d, want 1", got)
	}

	// Different on the first level: the second never runs.
	other := []*Value{
		&Value{Kind: WordValue, Word: "engineering"},
		corpusValue(t, script.Statements[1], "b"),
	}
	if got := CompareLevels(other, first, descendingSecond); got != -1 {
		t.Errorf("decided on the first level = %d, want -1", got)
	}
}

func corpusValue(t *testing.T, stmt *Statement, name string) *Value {
	t.Helper()
	for _, a := range stmt.Args {
		if a.Name == name {
			return a.Value
		}
	}
	return nil
}

func corpusInt(stmt *Statement, name string) (int, bool) {
	for _, a := range stmt.Args {
		if a.Name == name && a.Value != nil && a.Value.IsInt {
			return int(a.Value.Int), true
		}
	}
	return 0, false
}

func corpusWord(stmt *Statement, name string) string {
	for _, a := range stmt.Args {
		if a.Name == name && a.Value != nil {
			return a.Value.Word
		}
	}
	return ""
}

// show renders a value for a failure message.
func show(v *Value) string {
	switch {
	case v == nil:
		return "<missing>"
	case v.Kind == WordValue:
		return v.Word
	case v.Kind == StringValue:
		return `"` + strings.ReplaceAll(v.Str, `"`, `\"`) + `"`
	case v.Kind == NumberValue && v.IsInt:
		return strconv.FormatInt(v.Int, 10)
	case v.Kind == NumberValue:
		return strconv.FormatFloat(v.Number, 'g', -1, 64)
	}
	return "{block}"
}
