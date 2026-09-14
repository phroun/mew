package wire

import (
	"math"
	"testing"
)

// awkward is every value worth telling apart, including the pairs that a wire
// spelling gets wrong or gets right by accident.
func awkward() []*Value {
	return []*Value{
		nil,
		NewWord("a"),
		NewWord("ab"),
		NewWord(""),
		NewWord("a)"),   // no wire spelling at all: EncodeValue brackets it
		NewWord("(a)"),  // and this is what the bracketing would make of "a"
		NewWord("a\nb"), // nor this one
		NewString("a"),
		NewString("ab"),
		NewString(""),
		NewString("a\"b"),
		NewString("\\"),
		NewInt(0),
		NewInt(3),
		NewInt(-3),
		NewInt(math.MaxInt64),
		NewFloat(3),                    // 3.0, which is not the integer 3
		NewFloat(0),                    // 0.0
		NewFloat(math.Copysign(0, -1)), // and -0.0, which == calls the same number
		NewFloat(math.NaN()),
		NewFloat(math.Inf(1)),
		NewFloat(math.Inf(-1)),
		NewFloat(1e300),
		{Kind: BlockValue},
		{Kind: BlockValue, Block: &Script{Statements: []*Statement{
			{Verb: "eq", Args: []*Arg{Named("field", "name")}},
		}}},
		{Kind: BlockValue, Block: &Script{Statements: []*Statement{
			{Verb: "eq", Args: []*Arg{Named("field", "size")}},
		}}},
		// The same value under a different name, and the same name under a
		// different verb: a block is its whole shape, not its leaves.
		{Kind: BlockValue, Block: &Script{Statements: []*Statement{
			{Verb: "eq", Args: []*Arg{Named("other", "name")}},
		}}},
		{Kind: BlockValue, Block: &Script{Statements: []*Statement{
			{Verb: "ne", Args: []*Arg{Named("field", "name")}},
		}}},
		{Kind: BlockValue, Block: &Script{Statements: []*Statement{
			{Verb: "eq", Args: []*Arg{Named("field", "name")}},
			{Verb: "eq", Args: []*Arg{Named("field", "size")}},
		}}},
	}
}

// Key and Equal say the same thing. Two ways of asking one question, and a
// table keyed one way and searched the other is a table that loses records.
func TestKeyAndEqualAgree(t *testing.T) {
	vs := awkward()
	for i, a := range vs {
		for j, b := range vs {
			same := Equal(a, b)
			keyed := Key(a) == Key(b)
			if same != keyed {
				t.Errorf("%d/%d: Equal says %v and the keys %q / %q say %v",
					i, j, same, Key(a), Key(b), keyed)
			}
			if (i == j) != same {
				t.Errorf("%d/%d: Equal says %v", i, j, same)
			}
		}
	}
}

// A value equals itself, whatever it is. Without that a record is filed under a
// key it will never be looked up by.
func TestEveryValueEqualsItself(t *testing.T) {
	for _, v := range awkward() {
		if !Equal(v, v) {
			t.Errorf("%s does not equal itself", EncodeValue(v))
		}
		// And a separately built copy of it, since nothing may turn on the
		// pointer.
		if again := rebuild(v); !Equal(v, again) || Key(v) != Key(again) {
			t.Errorf("%s does not equal another of itself", EncodeValue(v))
		}
	}
}

func rebuild(v *Value) *Value {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

// The three decisions, written down so that changing one is a deliberate act.
func TestWhatCountsAsTheSameValue(t *testing.T) {
	blob := &Value{Kind: StringValue, Str: "ab", Blob: true}
	text := NewString("ab")
	if !Equal(blob, text) {
		t.Error("a blob and a string of the same bytes are different values")
	}

	if Equal(NewInt(3), NewFloat(3)) {
		t.Error("the integer 3 and the float 3.0 are the same value")
	}

	nan := NewFloat(math.NaN())
	if !Equal(nan, NewFloat(math.NaN())) {
		t.Error("NaN does not equal NaN, so a record keyed by one is lost")
	}
	if Equal(NewFloat(0), NewFloat(math.Copysign(0, -1))) {
		t.Error("0.0 and -0.0 are the same value")
	}

	if Equal(NewWord("a"), NewString("a")) {
		t.Error("the symbol a and the string \"a\" are the same value")
	}
}

// Keys can be concatenated, which is what a table keyed by two things does, and
// what a block of them does inside one key.
//
// The length in front of each piece is what makes that safe. A tag alone is
// not enough, because the text can hold the tag: `awb` then `c` and `a` then
// `bwc` both run to wawbwc without one.
func TestKeysDoNotRunTogether(t *testing.T) {
	for _, c := range []struct{ a, b, x, y *Value }{
		{NewWord("awb"), NewWord("c"), NewWord("a"), NewWord("bwc")},
		{NewString("asb"), NewString("c"), NewString("a"), NewString("bsc")},
		{NewWord("ab"), NewWord("c"), NewWord("a"), NewWord("bc")},
		{NewInt(1), NewInt(23), NewInt(12), NewInt(3)},
	} {
		if Key(c.a)+Key(c.b) == Key(c.x)+Key(c.y) {
			t.Errorf("%s then %s keys the same as %s then %s",
				Key(c.a), Key(c.b), Key(c.x), Key(c.y))
		}
	}

	// The same inside a sort, whose fields are run together the same way.
	if SortKey([]SortLevel{{Field: "afb"}, {Field: "c"}}) ==
		SortKey([]SortLevel{{Field: "a"}, {Field: "bfc"}}) {
		t.Error("two sort levels ran together into a third")
	}
}

// The wire spelling collides and Key does not, which is the reason for it.
//
// EncodeValue answers `undefined` for a nil value, for a kind it does not know,
// and for the symbol actually called `undefined`. A table keyed by the spelling
// files all three in one place -- so a record named `undefined` answers a
// question about nothing at all, and one about nothing answers for it.
func TestKeyTellsApartWhatTheSpellingCannot(t *testing.T) {
	var missing *Value
	named := NewWord("undefined")
	unknown := &Value{Kind: ValueKind(-1)}

	if EncodeValue(missing) != EncodeValue(named) || EncodeValue(named) != EncodeValue(unknown) {
		t.Fatalf("the spelling no longer collides: %q / %q / %q -- this test is "+
			"the reason Key exists and wants rewriting, not deleting",
			EncodeValue(missing), EncodeValue(named), EncodeValue(unknown))
	}
	for _, c := range []struct{ a, b *Value }{
		{missing, named}, {named, unknown}, {missing, unknown},
	} {
		if Equal(c.a, c.b) || Key(c.a) == Key(c.b) {
			t.Errorf("%q and %q are one value", Key(c.a), Key(c.b))
		}
	}
}

// A sort and a filter name a prepared sequence the same way, and two that are
// not the same sequence do not key the same.
func TestSortsAndFiltersAreNamedApart(t *testing.T) {
	sorts := [][]SortLevel{
		nil,
		{{Field: "name"}},
		{{Field: "name", Level: Level{Descending: true}}},
		{{Field: "name", Level: Level{Collation: CollateFold}}},
		{{Field: "name"}, {Field: "size"}},
		{{Field: "size"}, {Field: "name"}},
		// A field whose name holds the separator the spelling uses.
		{{Field: "name desc"}},
	}
	seen := map[string][]SortLevel{}
	for _, s := range sorts {
		k := SortKey(s)
		if had, clash := seen[k]; clash {
			t.Errorf("%v and %v are named the same", had, s)
		}
		seen[k] = s
	}

	filters := []*Filter{
		nil,
		{Op: OpEq, Field: "name", Values: []*Value{NewString("a")}},
		{Op: OpEq, Field: "name", Values: []*Value{NewString("b")}},
		{Op: OpEq, Field: "size", Values: []*Value{NewString("a")}},
		{Op: OpEq, Field: "name", Values: []*Value{NewString("a")}, Collate: CollateFold},
		{Op: OpNot, Children: []*Filter{
			{Op: OpEq, Field: "name", Values: []*Value{NewString("a")}},
		}},
		{Op: OpAnd, Children: []*Filter{
			{Op: OpEq, Field: "name", Values: []*Value{NewString("a")}},
		}},
		// Two filters holding the same parts in a different shape. Without a
		// count in front of the children, `not(and(X))` and `not(and(), X)`
		// come out as one filter: the same headers in the same order, and
		// nothing saying where one operator's children stop.
		{Op: OpNot, Children: []*Filter{
			{Op: OpAnd, Children: []*Filter{
				{Op: OpEq, Field: "name", Values: []*Value{NewString("a")}},
			}},
		}},
		{Op: OpNot, Children: []*Filter{
			{Op: OpAnd},
			{Op: OpEq, Field: "name", Values: []*Value{NewString("a")}},
		}},
		// And the same for values, which run together the same way.
		{Op: OpIn, Field: "name", Values: []*Value{NewString("a"), NewString("b")}},
		{Op: OpIn, Field: "name", Values: []*Value{NewString("a")}},
	}
	names := map[string]*Filter{}
	for _, f := range filters {
		k := FilterKey(f)
		if had, clash := names[k]; clash {
			t.Errorf("%s and %s are named the same", had.Encode(), f.Encode())
		}
		names[k] = f
	}
}
