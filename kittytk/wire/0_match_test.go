package wire

// What a filter lets through.
//
// The cases are the ones two implementations could disagree about: an empty
// operator, a field a record has not got, a value of the wrong kind for the
// predicate asking about it, and a collation on an op that has no order to
// apply one to.

import "testing"

// rec builds a record out of pairs, so a case reads as the record it is about.
func rec(pairs ...any) Fields {
	out := make(Fields, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, Named(pairs[i].(string), pairs[i+1]))
	}
	return out
}

// filter reads one from its wire text, which is how one arrives.
func filter(t *testing.T, text string) *Filter {
	t.Helper()
	script, err := Parse("f filter=" + text)
	if err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	f, err := ParseFilter(script.Statements[0].Args[0].Value)
	if err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	return f
}

func TestAFilterDecidesWhatIsIn(t *testing.T) {
	file := rec("name", "src/parser.go", "size", 1024, "kind", NewWord("source"))

	for _, c := range []struct {
		text string
		want bool
	}{
		// A block is an AND, and an empty one holds everything.
		{"{}", true},
		{`{ eq name "src/parser.go" }`, true},
		{`{ eq name "other" }`, false},
		{`{ eq name "src/parser.go"; ge size 1024 }`, true},
		{`{ eq name "src/parser.go"; gt size 1024 }`, false},

		// A symbol and a string are different values, which is what lets a
		// filter carry no type annotations.
		{"{ eq kind source }", true},
		{`{ eq kind "source" }`, false},

		// undefined is a value with a rank, so presence needs no operator.
		{"{ eq thumbnail undefined }", true},
		{"{ ne name undefined }", true},
		{"{ lt thumbnail 0 }", true},

		{"{ or { eq size 1; eq size 1024 } }", true},
		{"{ or { eq size 1; eq size 2 } }", false},
		{`{ not { starts name "." } }`, true},
		{`{ not { starts name "src/" } }`, false},
		{"{ in size 1 1024 4096 }", true},
		{"{ in size 1 2 3 }", false},
		{"{ in kind { source; folder } }", true},
		{"{ in kind { folder; disk } }", false},

		{`{ contains name "parser" }`, true},
		{`{ contains name "PARSER" }`, false},
		{`{ contains name "PARSER" collate=fold }`, true},
		{`{ starts name "src/" }`, true},
		{`{ ends name ".go" }`, true},
		{`{ ends name ".GO" collate=fold }`, true},

		// Text is text. A number has no inside for a string to sit in, and is
		// not rendered into one to pretend it has.
		{`{ contains size "102" }`, false},
		{`{ starts kind "sou" }`, false},
	} {
		if got := Match(file, filter(t, c.text)); got != c.want {
			t.Errorf("%s matched %v", c.text, got)
		}
	}
}

// A nil filter is an unfiltered sequence.
func TestNoFilterHoldsEverything(t *testing.T) {
	if !Match(rec("name", "anything"), nil) {
		t.Error("a record fell out of a filter that was not there")
	}
}

// Each operator's identity: a conjunction of nothing holds, a disjunction of
// nothing does not. `not` of nothing cannot be written -- the parser refuses
// it -- so there is no third case.
func TestAnEmptyOperatorIsItsOwnIdentity(t *testing.T) {
	if !Match(rec(), filter(t, "{ and {} }")) {
		t.Error("an empty AND excluded a record")
	}
	if Match(rec(), filter(t, "{ or {} }")) {
		t.Error("an empty OR admitted a record")
	}
}

// A block is an AND wherever one appears, so `not { a; b }` negates `a and b`
// rather than negating each of them. With one predicate inside, which is how a
// negation is nearly always written, the two readings agree.
func TestNotNegatesTheBlockAsAWhole(t *testing.T) {
	f := filter(t, `{ not { starts name "src/"; gt size 5000 } }`)
	if !Match(rec("name", "src/parser.go", "size", 1024), f) {
		t.Error("a record matching one half of the negated block was excluded")
	}
	if Match(rec("name", "src/parser.go", "size", 9000), f) {
		t.Error("a record matching the whole negated block was admitted")
	}
}

// The text predicates are text's alone, and the empty needle is what shows it:
// every string contains it, and a value that is not a string contains nothing,
// because it has no inside for a string to sit in.
func TestATextPredicateNeedsText(t *testing.T) {
	r := rec("name", "parser.go", "size", 1024, "kind", NewWord("source"), "data", []byte("hello"))

	for _, c := range []struct {
		text string
		want bool
	}{
		{`{ contains name "" }`, true},
		{`{ contains size "" }`, false},
		{`{ contains kind "" }`, false},
		{`{ contains missing "" }`, false},

		// Bytes are not text, and take no collation. A text needle is not
		// looked for in them, whatever the bytes happen to spell.
		{`{ contains data "ell" }`, false},
		{`{ starts data "hel" }`, false},
	} {
		if got := Match(r, filter(t, c.text)); got != c.want {
			t.Errorf("%s matched %v", c.text, got)
		}
	}
}

// A word is written as itself with nothing around it, so a caller building one
// out of text from somewhere else has to ask whether it can be spelled at all.
func TestWhatCanBeWrittenAsABareWord(t *testing.T) {
	for _, c := range []struct {
		text string
		want bool
	}{
		{"plain", true},
		{"with_under", true},
		{"SHOUT", true},
		{"has9digits", true},
		{".member", true},
		{"_leading", true},
		{"", false},
		{"1x", false},         // a word does not start with a digit
		{"kebab-case", false}, // nor hold a hyphen
		{"*star", false},
		{"two words", false},
		{"quote\"inside", false},
		{"semi;colon", false},
	} {
		if got := IsWord(c.text); got != c.want {
			t.Errorf("IsWord(%q) is %v", c.text, got)
		}
		// And what it says can be written, reads back as the word it was.
		if !c.want {
			continue
		}
		script, err := Parse("f v=" + c.text)
		if err != nil {
			t.Errorf("%q did not read back: %v", c.text, err)
			continue
		}
		if v := script.Statements[0].Args[0].Value; v == nil || v.Kind != WordValue || v.Word != c.text {
			t.Errorf("%q read back as %#v", c.text, v)
		}
	}
}
