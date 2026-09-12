package wire

// Writing the wire language back out.
//
// The parser turns text into statements; this turns statements back into text.
// Both directions of the connection need it now: an event carrying a block --
// a record's fields, a boundary -- has to render that block, and an app
// announcing what it hosts has to render a whole filter and sort.

import (
	"fmt"
	"strconv"
	"strings"
)

// Value constructors, for building what is about to be sent.

// NewString is a quoted string value.
func NewString(s string) *Value { return &Value{Kind: StringValue, Str: s} }

// NewBlob is a string value carrying bytes rather than text, so every one of
// them is escaped on the way out.
func NewBlob(b []byte) *Value { return &Value{Kind: StringValue, Str: string(b), Blob: true} }

// NewWord is a bare word: an identifier, an enum, or one of the four words
// that are values (undefined, nil, true, false).
func NewWord(w string) *Value { return &Value{Kind: WordValue, Word: w} }

// NewInt is an exact integer value.
func NewInt(i int64) *Value {
	return &Value{Kind: NumberValue, Number: float64(i), Int: i, IsInt: true}
}

// NewFloat is a floating-point value. A whole float written through here is
// still a float: NewInt is what says an integer was meant.
func NewFloat(f float64) *Value { return &Value{Kind: NumberValue, Number: f} }

// NewBlock is a block value holding the given statements.
func NewBlock(stmts ...*Statement) *Value {
	return &Value{Kind: BlockValue, Block: &Script{Statements: stmts}}
}

// Val is the convenience form: a Go value as a wire value. A *Value passes
// through, nil becomes the word `nil`, and anything with no spelling of its
// own is rendered with %v as a string.
func Val(v any) *Value {
	switch x := v.(type) {
	case nil:
		return NewWord(WordNil)
	case *Value:
		return x
	case string:
		return NewString(x)
	case []byte:
		return NewBlob(x)
	case bool:
		if x {
			return NewWord(WordTrue)
		}
		return NewWord(WordFalse)
	case int:
		return NewInt(int64(x))
	case int64:
		return NewInt(x)
	case uint64:
		return NewInt(int64(x))
	case float64:
		return NewFloat(x)
	case float32:
		return NewFloat(float64(x))
	}
	return NewString(fmt.Sprintf("%v", v))
}

// Named is one named value, for building an event's fields or a record's.
func Named(name string, v any) *Arg { return &Arg{Name: name, Value: Val(v)} }

// EncodeValue renders one value as wire text.
func EncodeValue(v *Value) string {
	if v == nil {
		return WordUndefined
	}
	switch v.Kind {
	case WordValue:
		return v.Word
	case NumberValue:
		if v.IsInt {
			return strconv.FormatInt(v.Int, 10)
		}
		// In as few digits as read it back exactly, and never in a spelling
		// that would come back an integer: `3` is an int and `3.0` is a float,
		// and a boundary that changed type between the two ends would be
		// comparing different things.
		s := strconv.FormatFloat(v.Number, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eEnN") {
			s += ".0"
		}
		return s
	case StringValue:
		if v.Blob {
			return QuoteBlob([]byte(v.Str))
		}
		return quoteString(v.Str)
	case BlockValue:
		body := EncodeScript(v.Block)
		if body == "" {
			return "{}"
		}
		return "{ " + body + " }"
	}
	return WordUndefined
}

// EncodeScript renders a run of statements, separated the way a block
// separates them.
func EncodeScript(s *Script) string {
	if s == nil || len(s.Statements) == 0 {
		return ""
	}
	parts := make([]string, 0, len(s.Statements))
	for _, st := range s.Statements {
		parts = append(parts, EncodeStatement(st))
	}
	return strings.Join(parts, "; ")
}

// EncodeStatement renders one statement: its key if it has one, its verb, and
// its arguments in the order they stand.
func EncodeStatement(st *Statement) string {
	var sb strings.Builder
	if st.Key != "" {
		sb.WriteString(st.Key)
		sb.WriteByte('=')
	}
	switch {
	case st.Verb != "":
		sb.WriteString(st.Verb)
	case st.Ref != "":
		sb.WriteString(st.Ref)
	default:
		sb.WriteString(strconv.FormatUint(st.RefID, 10))
	}
	for _, a := range st.Args {
		sb.WriteByte(' ')
		sb.WriteString(EncodeArg(a))
	}
	return sb.String()
}

// EncodeArg renders one argument: a flag, an operand, or a named value.
func EncodeArg(a *Arg) string {
	if a.Value == nil {
		switch a.Flag {
		case FlagFalse:
			return "!" + a.Name
		case FlagIndeterminate:
			return "?" + a.Name
		}
		return a.Name
	}
	if a.Name == "" {
		return EncodeValue(a.Value)
	}
	return a.Name + "=" + EncodeValue(a.Value)
}
