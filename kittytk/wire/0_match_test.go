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
		if got := Match(nil, file, filter(t, c.text)); got != c.want {
			t.Errorf("%s matched %v", c.text, got)
		}
	}
}

// A nil filter is an unfiltered sequence.
func TestNoFilterHoldsEverything(t *testing.T) {
	if !Match(nil, rec("name", "anything"), nil) {
		t.Error("a record fell out of a filter that was not there")
	}
}

// Each operator's identity: a conjunction of nothing holds, a disjunction of
// nothing does not. `not` of nothing cannot be written -- the parser refuses
// it -- so there is no third case.
func TestAnEmptyOperatorIsItsOwnIdentity(t *testing.T) {
	if !Match(nil, rec(), filter(t, "{ and {} }")) {
		t.Error("an empty AND excluded a record")
	}
	if Match(nil, rec(), filter(t, "{ or {} }")) {
		t.Error("an empty OR admitted a record")
	}
}

// A block is an AND wherever one appears, so `not { a; b }` negates `a and b`
// rather than negating each of them. With one predicate inside, which is how a
// negation is nearly always written, the two readings agree.
func TestNotNegatesTheBlockAsAWhole(t *testing.T) {
	f := filter(t, `{ not { starts name "src/"; gt size 5000 } }`)
	if !Match(nil, rec("name", "src/parser.go", "size", 1024), f) {
		t.Error("a record matching one half of the negated block was excluded")
	}
	if Match(nil, rec("name", "src/parser.go", "size", 9000), f) {
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
		if got := Match(nil, r, filter(t, c.text)); got != c.want {
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
		{"kebab-case", true},
		{"2026-09-13", true},
		{"1x", true},
		{"1e5x", true},
		{"e5", true},

		{"", false},
		// What spells a number comes back as one.
		{"3", false},
		{"3.5", false},
		{"1e+21", false},
		// A leading sign says the token is a number and nothing else.
		{"-3", false},
		{"+x", false},
		{"-", false},
		// And what a bare token cannot hold.
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

// A bare token is read whole and then measured, so what decides between a
// number and a symbol is what the token says rather than what it starts with.
func TestABareTokenIsANumberOrASymbolByWhatItSays(t *testing.T) {
	for _, c := range []struct {
		text string
		kind ValueKind
		num  float64
	}{
		{"3", NumberValue, 3},
		{"-3", NumberValue, -3},
		{"+3", NumberValue, 3},
		{"3.5", NumberValue, 3.5},
		{"1e5", NumberValue, 100000},
		{"1E5", NumberValue, 100000},
		{"1e+21", NumberValue, 1e21},
		{"6.02e+23", NumberValue, 6.02e23},
		{"1e-10", NumberValue, 1e-10},

		{"1x", WordValue, 0},
		{"1e5x", WordValue, 0},
		{"kebab-case", WordValue, 0},
		{"2026-09-13", WordValue, 0},
		{".0", WordValue, 0},
		{"e5", WordValue, 0},
		{"3.5.7", WordValue, 0},

		// The numeric form is written out rather than handed to the language's
		// own number parser, because each accepts a different set of extras.
		// These are the ones Go's would take and C's and Python's would not.
		{"0x1p4", WordValue, 0},
		{"inf", WordValue, 0},
		{"nan", WordValue, 0},
		{"infinity", WordValue, 0},
		{"1_000", WordValue, 0},
	} {
		script, err := Parse("f v=" + c.text)
		if err != nil {
			t.Errorf("%s: %v", c.text, err)
			continue
		}
		v := script.Statements[0].Args[0].Value
		if v == nil || v.Kind != c.kind {
			t.Errorf("%s came out as %#v", c.text, v)
			continue
		}
		if c.kind == NumberValue && v.Number != c.num {
			t.Errorf("%s came out as %v", c.text, v.Number)
		}
		if c.kind == WordValue && v.Word != c.text {
			t.Errorf("%s came out as the word %q", c.text, v.Word)
		}
	}
}

// A token written with a leading sign is a number and nothing else, so one that
// does not measure up is refused rather than quietly becoming a symbol.
func TestALeadingSignSaysNumberAndNothingElse(t *testing.T) {
	for _, text := range []string{"-x", "+x", "-", "+", "+1e", "-1.", "-."} {
		if _, err := Parse("f v=" + text); err == nil {
			t.Errorf("%q was accepted", text)
		}
	}
}

// A symbol the grammar has no bare spelling for is bracketed rather than
// turned into something else, so every symbol crosses as a symbol.
func TestASymbolWithNoBareSpellingIsBracketed(t *testing.T) {
	for _, c := range []struct{ word, text string }{
		{"plain", "plain"},
		{"kebab-case", "kebab-case"},
		{"2026-09-13", "2026-09-13"},
		{"objectLibrary/figaro/3", "(objectLibrary/figaro/3)"},
		{"*star", "(*star)"},
		{"two words", "(two words)"},
		{"quote\"inside", `(quote"inside)`},
		{"semi;colon", "(semi;colon)"},
		{"{braced}", "({braced})"},
		// What spells a number is bracketed too, or it would come back as one.
		{"3", "(3)"},
		{"1e+21", "(1e+21)"},
		{"-3", "(-3)"},
	} {
		got := EncodeValue(NewWord(c.word))
		if got != c.text {
			t.Errorf("the symbol %q was written %s", c.word, got)
		}
		script, err := Parse("f v=" + got)
		if err != nil {
			t.Errorf("%s did not read back: %v", got, err)
			continue
		}
		v := script.Statements[0].Args[0].Value
		if v == nil || v.Kind != WordValue || v.Word != c.word {
			t.Errorf("%s read back as %#v", got, v)
		}
	}
}

// An empty bracket names nothing, and one that does not close runs past the
// end of the statement, so both are refused rather than half-read.
func TestABracketedSymbolIsAName(t *testing.T) {
	for _, text := range []string{"f v=()", "f v=(unterminated", "f v=(two\nlines)"} {
		if _, err := Parse(text); err == nil {
			t.Errorf("%q was accepted", text)
		}
	}
}

// A field holding something with no order of its own -- a nested list -- cannot
// be compared against anything, so every comparison naming one is false.
//
// The comparison core calls all such values equal, which is what a sort needs:
// they tie, and the record key settles them. A filter inheriting that would
// answer yes to `eq tags { ... }` for any record with any tags at all, having
// looked inside nothing.
func TestAListCannotBeCompared(t *testing.T) {
	tagged := rec("name", "a", "tags", NewBlock(&Statement{Verb: "red"}))
	plain := rec("name", "b")

	for _, text := range []string{
		"{ eq tags undefined }",
		"{ ne tags undefined }",
		`{ eq tags "red" }`,
		"{ lt tags 3 }",
		"{ ge tags 3 }",
		"{ in tags red blue }",
		`{ contains tags "red" }`,
	} {
		if Match(nil, tagged, filter(t, text)) {
			t.Errorf("%s matched a record whose field is a list", text)
		}
	}

	// And the field being absent is a different thing from being a list, so
	// comparisons against a record that has not got it are untouched.
	if !Match(nil, plain, filter(t, "{ eq tags undefined }")) {
		t.Error("a record without the field stopped comparing as undefined")
	}
}

// Which is why presence has operators of its own: they ask whether the record
// carries the field, whatever it holds, and take no value.
func TestHasAndLacksAskWhetherTheFieldIsThere(t *testing.T) {
	tagged := rec("name", "a", "tags", NewBlock(&Statement{Verb: "red"}))
	plain := rec("name", "b")

	for _, c := range []struct {
		text string
		on   Record
		want bool
	}{
		{"{ has tags }", tagged, true},
		{"{ has tags }", plain, false},
		{"{ lacks tags }", tagged, false},
		{"{ lacks tags }", plain, true},

		// Whatever the field holds, including a plain value.
		{"{ has name }", tagged, true},
		{"{ lacks name }", tagged, false},

		// And they compose like any other predicate.
		{"{ has tags; eq name \"a\" }", tagged, true},
		{"{ not { lacks tags } }", tagged, true},
		{"{ not { lacks tags } }", plain, false},
	} {
		if got := Match(nil, c.on, filter(t, c.text)); got != c.want {
			t.Errorf("%s matched %v", c.text, got)
		}
	}
}

// `id` asks about a record's identity, which is not a field.
//
// It travels beside the record rather than among its fields, so it comes into
// Match as its own argument -- and a record that happens to carry a field
// called `key` is answering an ordinary question about an ordinary field.
func TestIDAsksAboutTheIdentityAndNotAField(t *testing.T) {
	id := NewWord("left/1")
	// The record's own `key` field says something else entirely.
	r := rec("key", "not-really-a-key", "name", "gamma")

	for _, c := range []struct {
		text string
		want bool
	}{
		{`{ id (left/1) }`, true},
		{`{ id (left/0) }`, false},
		{`{ id (left/0) (left/1) (right/9) }`, true},
		{`{ not { id (left/1) } }`, false},

		// A field called `key` is a field. These ask about the record's
		// contents, and nothing about its identity.
		{`{ eq key "not-really-a-key" }`, true},
		{`{ eq key (left/1) }`, false},
	} {
		if got := Match(id, r, filter(t, c.text)); got != c.want {
			t.Errorf("%s matched %v", c.text, got)
		}
	}

	// A record with no identity matches no set of them.
	if Match(nil, r, filter(t, `{ id (left/1) }`)) {
		t.Error("a record with no identity matched one")
	}
}

// It takes identities, written as values, and no field.
func TestIDIsWrittenWithoutAField(t *testing.T) {
	for _, text := range []string{
		`{ id }`,
		`{ id key (left/1) }`,
		`{ id at=(left/1) }`,
		`{ id { left/1 } }`,
	} {
		script, err := Parse("f filter=" + text)
		if err != nil {
			continue // refused at the grammar, which is refused
		}
		if _, err := ParseFilter(script.Statements[0].Args[0].Value); err == nil {
			t.Errorf("%s was accepted", text)
		}
	}
	f := filter(t, `{ id (left/1) 7 "notes" }`)
	if got := f.Encode(); got != `{ id (left/1) 7 "notes" }` {
		t.Errorf("it reads back as %s", got)
	}
}

// A record carries an identity, and a result claiming to carry a record
// without one is refused rather than read as a record of nobody.
func TestARecordWithoutAnIdentityIsRefused(t *testing.T) {
	script, err := Parse(`result 9 record={ name "src/parser.go" }`)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := ParseResult(script.Statements[0].Args[1:]); err == nil {
		t.Errorf("a record with no identity was read as %#v", r)
	}

	// And with one, it reads back as what was written.
	script, err = Parse(`result 9 id=17 record={ name "src/parser.go" }`)
	if err != nil {
		t.Fatal(err)
	}
	r, err := ParseResult(script.Statements[0].Args[1:])
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == nil || r.ID.Int != 17 || !r.Whole {
		t.Errorf("it came back as %#v", r)
	}
}
