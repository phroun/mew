package wire

// Values written with no name, in the order they were written.
//
// A property always travels under its name -- that is what makes the alias
// dictionaries worth having, and a property statement refuses an operand. But
// a VERB may take operands, and several already did through special cases: a
// target reference is a bare number, and the type word after `new` is a bare
// word read as a position. A filter is the case that needed the rest: an
// operator, a field, and what it is matched against, with no room for invented
// argument names between them.

import "testing"

// An operand of any kind stands in the order it was written, beside whatever
// is named.
func TestOperandsStandInTheOrderTheyWereWritten(t *testing.T) {
	script, err := Parse(`eq name "." collate=fold`)
	if err != nil {
		t.Fatal(err)
	}
	stmt := script.Statements[0]
	if stmt.Verb != "eq" {
		t.Fatalf("verb is %q", stmt.Verb)
	}
	if len(stmt.Args) != 3 {
		t.Fatalf("the statement holds %d args, want 3: %#v", len(stmt.Args), stmt.Args)
	}
	// The field is a bare word, which is a flag carrying its own name.
	if stmt.Args[0].Name != "name" || stmt.Args[0].Flag != FlagTrue {
		t.Errorf("the field came through as %#v", stmt.Args[0])
	}
	// The value is an operand: no name, and its own kind.
	if stmt.Args[1].Name != "" || stmt.Args[1].Value == nil ||
		stmt.Args[1].Value.Kind != StringValue || stmt.Args[1].Value.Str != "." {
		t.Errorf("the value came through as %#v", stmt.Args[1])
	}
	// Anything named still arrives named, after it.
	if stmt.Args[2].Name != "collate" || stmt.Args[2].Value.Word != "fold" {
		t.Errorf("the collation came through as %#v", stmt.Args[2])
	}
}

// A block is an operand like any other, so nesting needs no argument name
// invented to carry it.
func TestABlockCanBeAnOperand(t *testing.T) {
	script, err := Parse(`or { eq kind folder; eq kind disk }`)
	if err != nil {
		t.Fatal(err)
	}
	stmt := script.Statements[0]
	if stmt.Verb != "or" || len(stmt.Args) != 1 {
		t.Fatalf("or parsed as %q with %d args", stmt.Verb, len(stmt.Args))
	}
	block := stmt.Args[0].Value
	if stmt.Args[0].Name != "" || block == nil || block.Kind != BlockValue {
		t.Fatalf("the block came through as %#v", stmt.Args[0])
	}
	if n := len(block.Block.Statements); n != 2 {
		t.Fatalf("the block holds %d statements, want 2", n)
	}
	for i, inner := range block.Block.Statements {
		if inner.Verb != "eq" || len(inner.Args) != 2 {
			t.Errorf("alternative %d is %q with %d args", i, inner.Verb, len(inner.Args))
		}
	}
}

// Every kind of value can stand as an operand, and each keeps what it is --
// which is how a filter says that a symbol and a string are different
// questions without any type annotation.
func TestAnOperandKeepsWhatItIs(t *testing.T) {
	script, err := Parse(`is folder "folder" 1024 -2.5 { inner }`)
	if err != nil {
		t.Fatal(err)
	}
	args := script.Statements[0].Args
	if len(args) != 5 {
		t.Fatalf("the statement holds %d args, want 5: %#v", len(args), args)
	}
	// The first is a bare word, so it is a flag carrying its name.
	if args[0].Flag != FlagTrue || args[0].Name != "folder" {
		t.Errorf("the word came through as %#v", args[0])
	}
	for i, want := range []ValueKind{StringValue, NumberValue, NumberValue, BlockValue} {
		a := args[i+1]
		if a.Name != "" {
			t.Errorf("operand %d arrived named %q", i, a.Name)
		}
		if a.Value == nil || a.Value.Kind != want {
			t.Errorf("operand %d is %#v, want kind %d", i, a.Value, want)
		}
	}
	if args[1].Value.Str != "folder" {
		t.Errorf("the string operand is %q", args[1].Value.Str)
	}
	if !args[2].Value.IsInt || args[2].Value.Int != 1024 {
		t.Errorf("the integer operand is %#v", args[2].Value)
	}
	if args[3].Value.IsInt || args[3].Value.Number != -2.5 {
		t.Errorf("the float operand is %#v", args[3].Value)
	}
}

// A target reference is an operand like any other now, rather than the one
// special case the grammar allowed.
func TestATargetReferenceIsJustAnOperand(t *testing.T) {
	script, err := Parse(`set 1042 caption="hello"`)
	if err != nil {
		t.Fatal(err)
	}
	args := script.Statements[0].Args
	if len(args) != 2 {
		t.Fatalf("the statement holds %d args, want 2", len(args))
	}
	if args[0].Name != "" || !args[0].Value.IsInt || args[0].Value.Int != 1042 {
		t.Errorf("the target came through as %#v", args[0])
	}
	if args[1].Name != "caption" || args[1].Value.Str != "hello" {
		t.Errorf("the property came through as %#v", args[1])
	}
}
