package wire

// Telling one value from another, for the tables that hold records rather than
// the text that carries them.
//
// A cache, an ordering and a book of amendments all have to answer "is this the
// same record?" over and over, and the obvious way to do it was to write both
// identities out as wire text and compare the strings. That works, and it is
// wrong in three ways worth the sixty lines here:
//
//   - It makes the SPELLING load-bearing. Change how a float is written and
//     every key in every table changes with it, silently and everywhere at
//     once. The clients already disagree about float spelling.
//   - The spelling COLLIDES. EncodeValue answers `undefined` for a nil value,
//     for a value of a kind it does not know, and for the symbol that is
//     actually called `undefined` -- three different things filed under one
//     key, and a record named `undefined` found by asking about nothing at all.
//   - It is expensive. Wire text is escaped, quoted, and for a float written to
//     the shortest spelling that reads back exactly, and none of that is worth
//     paying for something nobody will ever read.
//
// So: Key is a canonical form and Equal is a comparison, and neither is
// anything to do with the wire. Key escapes nothing -- it puts a length in
// front of every piece of text instead, so that no two different values can run
// together into the same string -- and it is never parsed back.
//
// Key and Equal agree. Two values are Equal exactly when their Keys match, and
// 0_identity_test.go holds them to it over everything awkward there is.

import (
	"math"
	"strconv"
)

// Key is a value's canonical form: two values have the same Key exactly when
// Equal says they are the same value.
//
// Self-delimiting, so keys can be concatenated -- which is what a table keyed
// by more than one thing does, and what a block of them does inside one key.
func Key(v *Value) string {
	var buf [40]byte
	return string(AppendKey(buf[:0], v))
}

// AppendKey appends a value's Key to b, for a caller building a key out of more
// than one thing. A table keyed by a sequence and an identity wants one buffer
// and one string, not a key per part and a join afterwards.
func AppendKey(b []byte, v *Value) []byte {
	if v == nil {
		return append(b, '0')
	}
	switch v.Kind {
	case WordValue:
		return appendText(b, 'w', v.Word)
	case StringValue:
		return appendText(b, 's', v.Str)
	case NumberValue:
		if v.IsInt {
			return append(strconv.AppendInt(append(b, 'i'), v.Int, 10), ';')
		}
		return append(strconv.AppendUint(
			append(b, 'f'), math.Float64bits(v.Number), 16), ';')
	case BlockValue:
		b = append(b, '{')
		if v.Block != nil {
			for _, st := range v.Block.Statements {
				b = appendText(b, 'k', st.Key)
				b = appendText(b, 'v', st.Verb)
				b = appendText(b, 'r', st.Ref)
				b = append(strconv.AppendUint(b, st.RefID, 10), ';')
				for _, a := range st.Args {
					b = appendText(b, 'n', a.Name)
					b = append(strconv.AppendInt(b, int64(a.Flag), 10), ';')
					b = AppendKey(b, a.Value)
				}
				b = append(b, ',')
			}
		}
		return append(b, '}')
	}
	return append(b, '?')
}

// Equal reports whether two values are the same value.
//
// Three things it decides, each of which the wire spelling decided by accident
// before it:
//
//   - A blob and a string of the same bytes are the same value. Blob says how
//     the bytes are WRITTEN, which is no part of what they are, and the two
//     already spelled the same.
//   - 3 and 3.0 are not. An integer and a float are different kinds of number
//     here, and a boundary that changed type between the two ends would be
//     comparing different things.
//   - Floats compare by their bits and not by ==, which makes NaN equal to
//     itself and 0.0 different from -0.0. Both match what the spelling did. It
//     is also the only workable answer: a key must be reflexive, or a record
//     whose identity does not equal itself is filed and never found again.
func Equal(a, b *Value) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case WordValue:
		return a.Word == b.Word
	case StringValue:
		return a.Str == b.Str
	case NumberValue:
		if a.IsInt != b.IsInt {
			return false
		}
		if a.IsInt {
			return a.Int == b.Int
		}
		return math.Float64bits(a.Number) == math.Float64bits(b.Number)
	case BlockValue:
		// Delegated rather than walked a second time: one traversal to be
		// wrong in, instead of two to keep in step.
		return Key(a) == Key(b)
	}
	return false
}

// SortKey and FilterKey name the other two halves of a prepared sequence, for
// the same reason and on the same terms. A sequence is a source, a sort and a
// filter, and whatever holds one per sequence has to be able to say when two
// are the same sequence without writing a query out as text.
func SortKey(levels []SortLevel) string {
	b := make([]byte, 0, 32)
	for _, l := range levels {
		b = appendText(b, 'f', l.Field)
		b = appendText(b, 'c', l.Collation)
		if l.Descending {
			b = append(b, '-')
		} else {
			b = append(b, '+')
		}
	}
	return string(b)
}

func FilterKey(f *Filter) string {
	return string(appendFilterKey(make([]byte, 0, 64), f))
}

// The counts in front of the values and the children are belt and braces, and a
// mutation test will tell you so: every key starts with a tag letter and a count
// with a digit, so where the values stop and the children start is already
// unambiguous without them. They are here so that it stays unambiguous if a tag
// is ever added that does not, which is exactly the kind of change nobody would
// think to check. The same goes for the comma after each statement of a block.
func appendFilterKey(b []byte, f *Filter) []byte {
	if f == nil {
		return append(b, '0')
	}
	b = appendText(b, 'o', f.Op)
	b = appendText(b, 'f', f.Field)
	b = appendText(b, 'c', f.Collate)
	b = append(strconv.AppendInt(b, int64(len(f.Values)), 10), ':')
	for _, v := range f.Values {
		b = AppendKey(b, v)
	}
	b = append(strconv.AppendInt(b, int64(len(f.Children)), 10), ':')
	for _, c := range f.Children {
		b = appendFilterKey(b, c)
	}
	return b
}

// appendText appends a tagged, length-prefixed run of text. The length is what
// keeps two pieces from running together into a third: without it the words
// `ab` and `c` would key the same as `a` and `bc`.
func appendText(b []byte, tag byte, s string) []byte {
	b = append(b, tag)
	b = strconv.AppendInt(b, int64(len(s)), 10)
	return append(append(b, ':'), s...)
}
