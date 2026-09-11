package wire

// Ordering two values the same way at both ends of a connection.
//
// A view's records come from more than one place at once -- some held by the
// display, some known only to an application across the wire -- and each end
// orders what it holds before the results are folded together. That only works
// if both compute the SAME order from the same spec without conferring, so the
// rules are exact, small enough to implement twice, and free of anything whose
// answer depends on the machine or the locale.
//
// docs/sort-and-filter.md is the spec this implements, and testdata/compare.wire
// is the corpus every implementation of it answers.

import (
	"math"
	"strings"
)

// The four words that are values rather than symbols. Everything else written
// as a bare word is an identifier.
const (
	WordUndefined = "undefined"
	WordNil       = "nil"
	WordTrue      = "true"
	WordFalse     = "false"
)

// The collations a string may be compared under. A symbol and a blob take
// none: a symbol is an identifier, and a blob is not text.
const (
	CollateExact   = "exact"   // codepoint order, rune by rune
	CollateFold    = "fold"    // ASCII case folded, then exact
	CollateNatural = "natural" // digit runs as numbers, then fold
)

// A value's rank, which its type decides before anything in it is read.
const (
	RankUndefined = iota
	RankNil
	RankFalse
	RankTrue
	RankNumber // int and float share one rank and interleave by value
	RankSymbol
	RankString
	RankBytes
	RankUnordered // no order of its own; the sort's last level settles it
)

// Rank is where a value sits before its contents matter. A value that is not
// there at all is undefined, which is the bottom of the order.
func Rank(v *Value) int {
	if v == nil {
		return RankUndefined
	}
	switch v.Kind {
	case WordValue:
		switch v.Word {
		case WordUndefined:
			return RankUndefined
		case WordNil:
			return RankNil
		case WordFalse:
			return RankFalse
		case WordTrue:
			return RankTrue
		}
		return RankSymbol
	case NumberValue:
		return RankNumber
	case StringValue:
		if v.Blob {
			return RankBytes
		}
		return RankString
	}
	return RankUnordered
}

// Compare orders two values: -1, 0 or 1. The collation applies to the string
// rank alone and is CollateExact when empty.
//
// Two values of different ranks are decided by the ranks. Two of the same rank
// are decided by what is in them, except the ranks that hold one value each
// (undefined, nil, false, true) and the unordered rank, which compare equal and
// leave the answer to the sort's last level.
func Compare(a, b *Value, collation string) int {
	ra, rb := Rank(a), Rank(b)
	if ra != rb {
		return sign(ra - rb)
	}
	switch ra {
	case RankNumber:
		return compareNumbers(a, b)
	case RankSymbol:
		return compareRunes(a.Word, b.Word)
	case RankString:
		return collate(a.Str, b.Str, collation)
	case RankBytes:
		return strings.Compare(a.Str, b.Str) // the bytes as they were sent
	}
	return 0
}

// Level is one level of a sort, as it applies to a pair of values already
// taken from the two records being ordered.
type Level struct {
	Descending bool
	Collation  string
}

// CompareLevels walks the levels in order and answers on the first that
// separates the two runs, so each level settles only what the ones above it
// left equal. A run shorter than the levels stops where it ends.
//
// The caller appends the record's key as a final level: two records equal on
// every level a sort names must still have an order, or "the record after this
// point" names more than one place.
func CompareLevels(a, b []*Value, levels []Level) int {
	for i, level := range levels {
		if i >= len(a) || i >= len(b) {
			break
		}
		c := Compare(a[i], b[i], level.Collation)
		if c == 0 {
			continue
		}
		if level.Descending {
			return -c
		}
		return c
	}
	return 0
}

// collate compares two strings under one collation.
func collate(a, b, collation string) int {
	switch collation {
	case CollateFold:
		return compareFolded(a, b)
	case CollateNatural:
		return compareNatural(a, b)
	}
	return compareRunes(a, b)
}

// compareRunes is codepoint order, one rune against one. For well-formed UTF-8
// this is the byte order too; it is written as runes because that is what the
// rule says and what a malformed encoding would otherwise get wrong.
func compareRunes(a, b string) int {
	ar, br := []rune(a), []rune(b)
	for i := 0; i < len(ar) && i < len(br); i++ {
		if ar[i] != br[i] {
			return sign(int(ar[i]) - int(br[i]))
		}
	}
	return sign(len(ar) - len(br))
}

// compareFolded is compareRunes with ASCII case folded away. Nothing else is
// touched: a fold that reached further would be one two implementations could
// disagree about.
func compareFolded(a, b string) int {
	ar, br := []rune(a), []rune(b)
	for i := 0; i < len(ar) && i < len(br); i++ {
		x, y := foldRune(ar[i]), foldRune(br[i])
		if x != y {
			return sign(int(x) - int(y))
		}
	}
	return sign(len(ar) - len(br))
}

// compareNatural reads a run of ASCII digits as a number and everything else
// as folded runes, so file9 comes before file10.
func compareNatural(a, b string) int {
	ar, br := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ar) && j < len(br) {
		if isDigit(ar[i]) && isDigit(br[j]) {
			startA, startB := i, j
			for i < len(ar) && isDigit(ar[i]) {
				i++
			}
			for j < len(br) && isDigit(br[j]) {
				j++
			}
			if c := compareDigitRuns(ar[startA:i], br[startB:j]); c != 0 {
				return c
			}
			continue
		}
		x, y := foldRune(ar[i]), foldRune(br[j])
		if x != y {
			return sign(int(x) - int(y))
		}
		i++
		j++
	}
	return sign((len(ar) - i) - (len(br) - j))
}

// compareDigitRuns compares two runs of digits as numbers, however long they
// are: with the leading zeros gone, the longer run is the larger number and
// equal lengths compare digit by digit. Two runs of the same value are then
// separated by the zeros themselves, so 01 and 1 are not the same string.
func compareDigitRuns(a, b []rune) int {
	as, bs := withoutLeadingZeros(a), withoutLeadingZeros(b)
	if len(as) != len(bs) {
		return sign(len(as) - len(bs))
	}
	for i := range as {
		if as[i] != bs[i] {
			return sign(int(as[i]) - int(bs[i]))
		}
	}
	return sign(len(a) - len(b))
}

func withoutLeadingZeros(r []rune) []rune {
	i := 0
	for i < len(r)-1 && r[i] == '0' {
		i++
	}
	return r[i:]
}

// compareNumbers orders two numbers exactly. Two integers compare as integers
// however large they are, and an integer against a float compares without
// either being converted: casting the integer loses digits, and casting the
// float loses the fraction that decides it.
func compareNumbers(a, b *Value) int {
	switch {
	case a.IsInt && b.IsInt:
		return signOf(a.Int, b.Int)
	case a.IsInt:
		return -compareFloatToInt(b.Number, a.Int)
	case b.IsInt:
		return compareFloatToInt(a.Number, b.Int)
	}
	switch {
	case a.Number < b.Number:
		return -1
	case a.Number > b.Number:
		return 1
	}
	return 0
}

// compareFloatToInt orders a float against an integer: -1 if the float is the
// smaller. A float too large to be an int64 is decided by its sign alone;
// otherwise the whole part decides and the fraction breaks the tie.
func compareFloatToInt(f float64, i int64) int {
	if math.IsNaN(f) {
		return 0
	}
	const (
		tooLarge = 9223372036854775808.0  // 2^63, one past the largest int64
		tooSmall = -9223372036854775808.0 // -2^63, the smallest int64 exactly
	)
	if f >= tooLarge {
		return 1
	}
	if f < tooSmall {
		return -1
	}
	whole := int64(math.Trunc(f))
	if whole != i {
		return signOf(whole, i)
	}
	switch frac := f - math.Trunc(f); {
	case frac > 0:
		return 1
	case frac < 0:
		return -1
	}
	return 0
}

func foldRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// signOf orders two integers without subtracting them, a difference of two
// int64s being able to overflow one.
func signOf(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
