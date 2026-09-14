package wire

// Deciding whether a record is in a filter.
//
// The other half of what two ends have to agree on. compare.go settles the
// order; this settles the membership, and both are the same comparison rules
// used two ways -- a predicate is a comparison with its answer thrown away
// except for the sign.
//
// docs/sort-and-filter.md is the spec this implements.

import "strings"

// A Record is anything a filter can take a field out of.
//
// A field a record has not got reads as nil, which is `undefined`: a value with
// a rank of its own rather than an error. That is what makes `eq thumbnail
// undefined` a question about presence and leaves the grammar with no presence
// operator to specify.
type Record interface {
	Field(name string) *Value
}

// Field makes a field bag a Record, so a record or a boundary that arrived on
// the wire can be put to a filter without being copied into something else.
func (f Fields) Field(name string) *Value { return f.Get(name) }

// Match reports whether a record passes a filter. A nil filter passes
// everything, which is what an unfiltered sequence is.
//
// The identity comes in beside the record rather than as a field of it,
// because that is what it is: `id` asks about the identity, and every other
// operator asks about a field. A record whose fields happen to include one
// called `key` is answering an ordinary question about an ordinary field.
// Pass nil for id where there is none, and `id` then matches nothing.
func Match(id *Value, rec Record, f *Filter) bool {
	if f == nil {
		return true
	}
	switch f.Op {
	case OpAnd:
		return all(id, rec, f.Children)
	case OpOr:
		for _, c := range f.Children {
			if Match(id, rec, c) {
				return true
			}
		}
		// A conjunction of nothing holds and a disjunction of nothing does not,
		// which is each operator's own identity and needs no special case
		// beyond saying so.
		return false
	case OpNot:
		// A block is an AND wherever one appears, this one included, so `not {
		// a; b }` is the negation of `a and b`. With one predicate inside --
		// which is how a negation is nearly always written -- the two readings
		// agree anyway.
		return !all(id, rec, f.Children)
	case OpID:
		for _, v := range f.Values {
			if id != nil && Compare(id, v, f.Collate) == 0 {
				return true
			}
		}
		return false
	case OpHas:
		return rec.Field(f.Field) != nil
	case OpLacks:
		return rec.Field(f.Field) == nil
	case OpContains, OpStarts, OpEnds:
		return matchText(f.Op, rec.Field(f.Field), f.Value(), f.Collate)
	}

	have := rec.Field(f.Field)

	// A value with no order of its own -- a nested list -- cannot be compared
	// against anything, so every comparison naming one is false.
	//
	// The comparison core says such values are all equal, which is what a sort
	// needs: they tie, and the record key settles them. A filter asking `eq
	// .tags { ... }` would inherit that and answer yes for any record with any
	// tags at all, having looked inside nothing. It cannot answer, so it does
	// not admit -- a filter narrows, and that is the direction to fail in.
	// Whether the field is there at all is what `has` and `lacks` are for.
	if Rank(have) == RankUnordered {
		return false
	}

	if f.Op == OpIn {
		for _, v := range f.Values {
			if Compare(have, v, f.Collate) == 0 {
				return true
			}
		}
		return false
	}

	c := Compare(have, f.Value(), f.Collate)
	switch f.Op {
	case OpEq:
		return c == 0
	case OpNe:
		return c != 0
	case OpLt:
		return c < 0
	case OpLe:
		return c <= 0
	case OpGt:
		return c > 0
	case OpGe:
		return c >= 0
	}
	return false
}

func all(id *Value, rec Record, children []*Filter) bool {
	for _, c := range children {
		if !Match(id, rec, c) {
			return false
		}
	}
	return true
}

// matchText answers the three text predicates, which are text's alone: a
// number, a symbol and a word have no inside for one string to sit in, so a
// field that is not the same flavour of string as the value fails rather than
// being rendered into one to compare.
//
// Bytes take no collation, being not text. Text takes one, and `natural` reads
// as `fold` here: a digit run's numeric value says whether one string sorts
// before another, and nothing at all about whether it sits inside it.
func matchText(op string, have, want *Value, collation string) bool {
	if have == nil || want == nil ||
		have.Kind != StringValue || want.Kind != StringValue ||
		have.Blob != want.Blob {
		return false
	}
	a, b := have.Str, want.Str
	if !have.Blob && collation != CollateExact && collation != "" {
		a, b = foldASCII(a), foldASCII(b)
	}
	switch op {
	case OpStarts:
		return strings.HasPrefix(a, b)
	case OpEnds:
		return strings.HasSuffix(a, b)
	}
	return strings.Contains(a, b)
}

// foldASCII maps A-Z to a-z and leaves every other byte alone, which is the
// whole of the `fold` collation. It works a byte at a time because every byte
// of a multi-byte rune is above 0x7f, so no ASCII letter can appear inside one.
func foldASCII(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r >= 'A' && r <= 'Z' }) {
		return s
	}
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
