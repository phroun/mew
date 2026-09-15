package wire

// Between what the grammar parsed and what the records are made of.
//
// The two are different things and this is where they meet. A wire Value is a
// PARSE NODE: its kinds are lexical -- a bare token, a quoted string, a braced
// block of statements -- and they say how something was written. A serval Value
// is a DATA value: its kinds are what a thing is, and nothing in it knows that
// a grammar exists.
//
// Four words are values rather than names, and this is the only place that is
// true. `undefined`, `nil`, `true` and `false` are how the wire writes the
// absence of a value, nothing, and the two booleans -- so they are read as
// those here, and past this point a bare token is only ever a name. That is
// what keeps a field whose value is the WORD "true" apart from one whose value
// IS true: on the wire they are the same six characters, and only the sender
// knew which it meant.

import "github.com/phroun/serval"

// The four words that carry a value rather than naming something.
const (
	WordUndefined = "undefined"
	WordNil       = "nil"
	WordTrue      = "true"
	WordFalse     = "false"
)

// asData reads a parsed value as the value it stands for.
func asData(v *Value) *serval.Value {
	if v == nil {
		return nil
	}
	switch v.Kind {
	case WordValue:
		switch v.Word {
		case WordUndefined:
			return nil
		case WordNil:
			return serval.NewNil()
		case WordTrue:
			return serval.NewBool(true)
		case WordFalse:
			return serval.NewBool(false)
		}
		return serval.NewSymbol(v.Word)
	case NumberValue:
		if v.IsInt {
			return serval.NewInt(v.Int)
		}
		return serval.NewFloat(v.Number)
	case StringValue:
		if v.Blob {
			return serval.NewBytes([]byte(v.Str))
		}
		return serval.NewText(v.Str)
	case BlockValue:
		return serval.NewList(asRecord(v.Block))
	}
	return nil
}

// asWire writes a value as the parse node that spells it.
func asWire(v *serval.Value) *Value {
	if v == nil {
		return NewWord(WordUndefined)
	}
	switch v.Kind {
	case serval.NilValue:
		return NewWord(WordNil)
	case serval.BoolValue:
		if v.Bool {
			return NewWord(WordTrue)
		}
		return NewWord(WordFalse)
	case serval.NumberValue:
		if v.IsInt {
			return NewInt(v.Int)
		}
		return NewFloat(v.Num)
	case serval.SymbolValue:
		return NewWord(v.Str)
	case serval.TextValue:
		return NewString(v.Str)
	case serval.BytesValue:
		return NewBlob([]byte(v.Str))
	case serval.ListValue:
		return &Value{Kind: BlockValue, Block: asScript(v.List)}
	}
	return NewWord(WordUndefined)
}

// asRecord reads a block as a record: one statement per field, the field's name
// first and its value, if it has one, after.
//
// A statement carrying no value at all is a name with nothing under it, which
// is a field a query ASKED for rather than one a record has. A statement whose
// verb is empty is a member standing by its position.
func asRecord(s *Script) serval.Record {
	if s == nil {
		return nil
	}
	out := make(serval.Record, 0, len(s.Statements))
	for _, st := range s.Statements {
		f := &serval.Field{Name: st.Verb}
		for _, a := range st.Args {
			if a.Name == "" && a.Value != nil {
				f.Value = asData(a.Value)
				break
			}
		}
		out = append(out, f)
	}
	return out
}

// asScript writes a record as the block that spells it.
func asScript(r serval.Record) *Script {
	s := &Script{}
	for _, f := range r {
		st := &Statement{Verb: f.Name}
		if f.Value != nil {
			st.Args = []*Arg{{Value: asWire(f.Value)}}
		}
		s.Statements = append(s.Statements, st)
	}
	return s
}

// AsData and AsWire are the same two readings, for the layers above that hold
// one side and need the other: a source relaying a record it was handed, a
// client naming an identity it was given, a tool writing a value back out.
//
// The block readings stay unexported. A caller that has a whole record has a
// statement to put it in, and ParseFields and EncodeRecord are that statement's
// two ends.
func AsData(v *Value) *serval.Value { return asData(v) }
func AsWire(v *serval.Value) *Value { return asWire(v) }
