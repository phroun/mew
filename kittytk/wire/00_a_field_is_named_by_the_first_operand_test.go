package wire

// A predicate's field, and the names a corpus line cannot hold.
//
// testdata/query.wire carries the agreement between the three implementations,
// and it is a line-based file: a name with a newline in it has no case there,
// and nor does a name built by a program rather than written down. Both reach
// the encoder, so both are here.
//
// What the corpus DOES carry -- an index, a symbol, a quoted name, and the
// forms that name nothing -- is not repeated here.

import (
	"testing"

	"github.com/phroun/serval"
)

// Whatever a field is called, writing it out and reading it back names the
// same field. A source made of structured records gets its field names from
// the data, which is where a name the grammar has no bare spelling for comes
// from in the first place.
func TestAFieldNameSurvivesBeingWrittenOut(t *testing.T) {
	for _, name := range []string{
		"name",
		".size",
		"0",
		"007",
		"-1",
		"two words",
		"a)b",
		"line\nbreak",
		"()",
		"collate",
		"true",
		"1.5",
		"1e+21",
	} {
		want := &serval.Filter{Op: serval.OpEq, Field: name, Values: []*serval.Value{serval.NewText("x")}}
		text := EncodeFilter(&serval.Filter{Op: serval.OpAnd, Children: []*serval.Filter{want}})

		script, err := Parse("spec filter=" + text)
		if err != nil {
			t.Errorf("%q wrote as %s, which does not parse: %v", name, text, err)
			continue
		}
		got, err := ParseFilter(script.Statements[0].Args[0].Value)
		if err != nil {
			t.Errorf("%q wrote as %s, which is not a filter: %v", name, text, err)
			continue
		}
		if len(got.Children) != 1 || got.Children[0].Field != name {
			t.Errorf("%q wrote as %s and came back as %s", name, text, got)
		}
	}
}

// Bytes are not a name. No parse produces a blob -- a quoted run of characters
// is text until somebody says otherwise -- so this is the one form only a
// caller building a statement for itself can put in a field's place, and it is
// refused there like a float or a block.
func TestBytesDoNotNameAField(t *testing.T) {
	st := &Statement{Verb: serval.OpEq, Args: []*Arg{
		{Value: NewBlob([]byte("name"))},
		{Value: NewString("x")},
	}}
	if f, err := parsePredicate(st); err == nil {
		t.Errorf("a blob was taken as the field name %q: %s", f.Field, f)
	}
}

// A field with no name at all is not a field. Nothing builds one, and the
// spelling it would take -- an empty string -- is refused on the way back in
// rather than read as a filter over nothing.
func TestAFilterWithNoFieldIsRefused(t *testing.T) {
	text := EncodeFilter(&serval.Filter{Op: serval.OpAnd, Children: []*serval.Filter{
		{Op: serval.OpEq, Values: []*serval.Value{serval.NewText("x")}},
	}})
	script, err := Parse("spec filter=" + text)
	if err != nil {
		t.Fatalf("%s does not parse: %v", text, err)
	}
	if f, err := ParseFilter(script.Statements[0].Args[0].Value); err == nil {
		t.Errorf("%s was taken as %s", text, f)
	}
}
