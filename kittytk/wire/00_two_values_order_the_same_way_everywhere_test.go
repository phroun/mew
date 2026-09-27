package wire

// The comparison core, against the corpus every implementation of it answers.
//
// The comparison itself is serval's -- ordering records is a data question, not
// a protocol one -- but the corpus lives here, because what it is FOR is the
// Go, C and Python implementations all answering the same questions from the
// same file. So the cases are written the way a value is written anywhere else
// on the wire, read with the wire parser, and handed across as data.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/phroun/serval"
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

		if got := serval.Compare(a, b, collation); got != want {
			t.Errorf("case %d: compare(%s, %s, %q) = %d, want %d",
				i+1, show(a), show(b), collation, got, want)
		}
		// Every case is its own mirror: swapping the two swaps the answer.
		if got := serval.Compare(b, a, collation); got != -want {
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
	for _, collation := range []string{serval.CollateExact, serval.CollateFold, serval.CollateNatural, ""} {
		for i, stmt := range script.Statements {
			v := corpusValue(t, stmt, "a")
			if got := serval.Compare(v, v, collation); got != 0 {
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
	text := serval.NewText("ab")
	blob := serval.NewBytes([]byte("ab"))
	if got := serval.Compare(text, blob, serval.CollateExact); got != -1 {
		t.Errorf("a string against the same bytes = %d, want -1", got)
	}
	if got := serval.Compare(blob, blob, serval.CollateExact); got != 0 {
		t.Errorf("bytes against themselves = %d, want 0", got)
	}
	// Bytes compare unsigned, so a high byte is the larger.
	high := serval.NewBytes([]byte("\xff"))
	low := serval.NewBytes([]byte("\x01"))
	if got := serval.Compare(low, high, serval.CollateExact); got != -1 {
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
	if serval.Rank(a) != serval.RankUnordered {
		t.Fatalf("a block ranks %d, want the unordered rank", serval.Rank(a))
	}
	if got := serval.Compare(a, b, serval.CollateExact); got != 0 {
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
	first := []*serval.Value{
		corpusValue(t, script.Statements[0], "a"),
		corpusValue(t, script.Statements[0], "b"),
	}
	second := []*serval.Value{
		corpusValue(t, script.Statements[1], "a"),
		corpusValue(t, script.Statements[1], "b"),
	}

	ascending := []serval.Level{{}, {}}
	if got := serval.CompareLevels(first, second, ascending); got != -1 {
		t.Errorf("equal on the first level, smaller on the second = %d, want -1", got)
	}

	descendingSecond := []serval.Level{{}, {Descending: true}}
	if got := serval.CompareLevels(first, second, descendingSecond); got != 1 {
		t.Errorf("the second level turned over = %d, want 1", got)
	}

	// Different on the first level: the second never runs.
	other := []*serval.Value{
		serval.NewSymbol("engineering"),
		corpusValue(t, script.Statements[1], "b"),
	}
	if got := serval.CompareLevels(other, first, descendingSecond); got != -1 {
		t.Errorf("decided on the first level = %d, want -1", got)
	}
}

// corpusValue reads one side of a case as the data value it stands for. The
// corpus is written in the wire's own grammar; what is compared is what the
// grammar meant.
func corpusValue(t *testing.T, stmt *Statement, name string) *serval.Value {
	t.Helper()
	for _, a := range stmt.Args {
		if a.Name == name {
			return AsData(a.Value)
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
func show(v *serval.Value) string { return v.String() }
