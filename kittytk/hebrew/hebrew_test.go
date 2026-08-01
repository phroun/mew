package hebrew

import "testing"

func str(rs ...rune) string { return string(rs) }

func TestPrecomposeCluster(t *testing.T) {
	const (
		bet, shin, alef, he, vav, het = 0x05D1, 0x05E9, 0x05D0, 0x05D4, 0x05D5, 0x05D7
		dagesh, shinDot, sinDot, rafe = 0x05BC, 0x05C1, 0x05C2, 0x05BF
		holamHaser                    = 0x05BA
		sheva, qamats, qubuts         = 0x05B0, 0x05B8, 0x05BB
	)
	cases := []struct {
		name  string
		runes []rune
		want  string
		ok    bool
	}{
		{"bet+dagesh", []rune{bet, dagesh}, str(0xFB31), true},
		{"shin+shindot", []rune{shin, shinDot}, str(0xFB2A), true},
		{"shin+sindot", []rune{shin, sinDot}, str(0xFB2B), true},
		{"shin+dagesh+shindot", []rune{shin, dagesh, shinDot}, str(0xFB2C), true},
		{"shin+dagesh+sindot", []rune{shin, dagesh, sinDot}, str(0xFB2D), true},
		{"alef+mapiq", []rune{alef, dagesh}, str(0xFB30), true},
		{"he+mapiq", []rune{he, dagesh}, str(0xFB34), true},
		{"vav+dagesh(shuruk)", []rune{vav, dagesh}, str(0xFB35), true},
		{"bet+rafe", []rune{bet, rafe}, str(0xFB4C), true},
		{"vav+holam-haser", []rune{vav, holamHaser}, str(0xFB4B), true},

		// The point composes wherever it falls among the marks, and vowels ride
		// the composed glyph — the reported regression.
		{"shin+shindot+vowel", []rune{shin, shinDot, sheva}, str(0xFB2A, sheva), true},
		{"shin+vowel+shindot", []rune{shin, sheva, shinDot}, str(0xFB2A, sheva), true},
		{"shin+vowel+shindot+vowel", []rune{shin, qubuts, shinDot, qamats}, str(0xFB2A, qubuts, qamats), true},
		{"bet+vowel+dagesh", []rune{bet, qamats, dagesh}, str(0xFB31, qamats), true},

		// Anchored (isolated) point on a faux base.
		{"bare shin dot", []rune{dottedCircle, shinDot}, str(0xFB2A), true},
		{"bare sin dot", []rune{dottedCircle, sinDot}, str(0xFB2B), true},
		{"bare holam-haser", []rune{dottedCircle, holamHaser}, str(0xFB4B), true},
		{"bare vowel stays on its circle", []rune{dottedCircle, sheva}, "", false},

		// No presentation form -> the dagesh is dropped; vowels kept.
		{"het+dagesh omits", []rune{het, dagesh}, str(het), true},
		{"het+dagesh+qamats keeps vowel", []rune{het, dagesh, qamats}, str(het, qamats), true},

		// Nothing folds.
		{"bet+vowel only", []rune{bet, qamats}, "", false},
		{"bare letter", []rune{bet}, "", false},
		{"latin base", []rune{'a', dagesh}, "", false},
	}
	for _, c := range cases {
		out, ok := PrecomposeCluster(c.runes)
		if ok != c.ok || (ok && string(out) != c.want) {
			t.Errorf("%s: got (%q,%v), want (%q,%v)", c.name, string(out), ok, c.want, c.ok)
		}
	}
}

func TestComposedBase(t *testing.T) {
	const shin, shinDot, sheva = 0x05E9, 0x05C1, 0x05B0
	// The base folds regardless of vowels riding it; the vowels are the caller's.
	if b, ok := ComposedBase([]rune{shin, sheva, shinDot}); !ok || b != 0xFB2A {
		t.Errorf("ComposedBase(shin+vowel+shindot) = %04X,%v want FB2A,true", b, ok)
	}
	if b, ok := ComposedBase([]rune{shin, sheva}); ok || b != shin {
		t.Errorf("ComposedBase(shin+vowel) = %04X,%v want shin,false", b, ok)
	}
}

func TestFolds(t *testing.T) {
	for _, r := range []rune{0x05BC, 0x05C1, 0x05C2, 0x05BF, 0x05BA} {
		if !Folds(r) {
			t.Errorf("U+%04X should fold", r)
		}
	}
	for _, r := range []rune{0x05B0, 0x05B8, 0x05BB, 'a'} { // vowels + non-Hebrew
		if Folds(r) {
			t.Errorf("U+%04X should not fold", r)
		}
	}
}
