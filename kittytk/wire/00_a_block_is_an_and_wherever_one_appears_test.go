package wire

// A filter's outermost operator, written out and read back.
//
// **A block IS an and**, wherever one appears. That is the grammar, and it is why
// the top of a filter can be written as a bare block at all: `filter={ eq a 1; eq
// b 2 }` is a conjunction because a block is one.
//
// Which makes the encoder's job at the top of a filter a decision rather than a
// formality. An `and` may have its children written straight into the outer block,
// because that block already means and. An `or` may not, and a `not` may not: their
// children written there would be read as a conjunction, which for an `or` asks for
// rows satisfying every branch at once and for a `not` asks for the opposite of
// what was meant. Neither fails; both answer wrongly and quietly.
//
// This is not hypothetical. A tree hint with no root spelled out takes the rows
// whose container is absent OR empty -- "an OR and not a choice, because guessing
// which of the two a document meant would put half a tree's top level out of
// sight". Flattened to an and it asks for the rows whose container is both absent
// and empty, which is none of them, and a hundred thousand rows arrived as an empty
// tree.

import (
	"testing"

	"github.com/phroun/serval"
)

// oneRow is a record the filters below are asked about.
type oneRow serval.Record

func (r oneRow) Field(name string) *serval.Value { return serval.Record(r).Get(name) }

// The top of a filter survives being written out, whichever operator it is -- and
// what "survives" means is that it goes on matching the same records. Comparing the
// structure would let an operator that reads back as something equivalent look like
// a failure, and would let one that reads back wrongly look like a pass if the
// shape happened to line up.
func TestTheTopOfAFilterMeansTheSameWrittenOut(t *testing.T) {
	// Absent, empty, and neither: the three ways a container field can read, which
	// is exactly what a tree hint's top level turns on.
	rows := []struct {
		what string
		rec  oneRow
	}{
		{"no dir at all", oneRow{serval.Named("name", "x")}},
		{"an empty dir", oneRow{serval.Named("dir", "")}},
		{"a dir", oneRow{serval.Named("dir", "somewhere")}},
	}

	absent := &serval.Filter{Op: serval.OpEq, Field: "dir", Values: []*serval.Value{nil}}
	empty := &serval.Filter{Op: serval.OpEq, Field: "dir", Values: []*serval.Value{serval.NewText("")}}

	for _, f := range []*serval.Filter{
		{Op: serval.OpOr, Children: []*serval.Filter{absent, empty}},
		{Op: serval.OpAnd, Children: []*serval.Filter{absent, empty}},
		{Op: serval.OpNot, Children: []*serval.Filter{absent}},
		// One branch, which is where a flattened or is hardest to see: an and of one
		// and an or of one mean the same thing, so this passes either way and is
		// here to say so rather than to catch anything.
		{Op: serval.OpOr, Children: []*serval.Filter{absent}},
		// Nested, because the inner operators were never flattened -- only the top
		// was -- and a fix at the top must not disturb them.
		{Op: serval.OpAnd, Children: []*serval.Filter{
			{Op: serval.OpOr, Children: []*serval.Filter{absent, empty}},
			{Op: serval.OpNot, Children: []*serval.Filter{
				{Op: serval.OpEq, Field: "name", Values: []*serval.Value{serval.NewText("x")}},
			}},
		}},
	} {
		text := EncodeFilter(f)
		script, err := Parse("descriptor filter=" + text)
		if err != nil {
			t.Errorf("%s wrote as %s, which does not parse: %v", f, text, err)
			continue
		}
		got, err := ParseFilter(script.Statements[0].Args[0].Value)
		if err != nil {
			t.Errorf("%s wrote as %s, which is not a filter: %v", f, text, err)
			continue
		}
		for _, row := range rows {
			want := serval.Match(serval.NewInt(1), row.rec, f)
			if have := serval.Match(serval.NewInt(1), row.rec, got); have != want {
				t.Errorf("%s wrote as %s; against %s it now answers %v, want %v",
					f, text, row.what, have, want)
			}
		}
	}
}

// An operator with nothing under it is no filter, rather than an operator with an
// empty block after it -- which is what `not {}` would be, and what parsing back
// refuses.
func TestAnOperatorWithNoChildrenWritesAsNoFilter(t *testing.T) {
	for _, op := range []string{serval.OpAnd, serval.OpOr, serval.OpNot} {
		if got := EncodeFilter(&serval.Filter{Op: op}); got != "{}" {
			t.Errorf("an empty %s wrote as %s, want {}", op, got)
		}
	}
}
