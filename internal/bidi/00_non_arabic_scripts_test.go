package bidi

import "testing"

// perm returns the visual permutation of a line, or nil.
func perm(runes []rune, rtl bool) []int {
	if l := Compute(runes, rtl); l != nil {
		return l.Perm
	}
	return nil
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Non-Arabic RTL scripts are reordered by bidi (they read right-to-left) but
// are NOT cursively shaped: only Arabic has Unicode presentation-form code
// points to substitute to. Syriac, Mandaic and Adlam are cursive-joining yet
// have no presentation forms (their joined shapes are font/OpenType glyphs we
// can't produce), so we deliver correct ordering with nominal (isolated)
// letter forms. Samaritan is non-cursive, so ordering IS complete support.
func TestNonArabicRTLScriptsReorder(t *testing.T) {
	cases := []struct {
		name  string
		runes []rune
		rtl   bool
		want  []int
	}{
		{"syriac", []rune{0x072B, 0x0720, 0x0721, 0x0710}, false, []int{3, 2, 1, 0}},
		{"mandaic", []rune{0x0840, 0x0841, 0x0842}, false, []int{2, 1, 0}},
		{"samaritan", []rune{0x0800, 0x0801, 0x0802}, false, []int{2, 1, 0}},
		{"adlam", []rune{0x1E900, 0x1E901, 0x1E902}, false, []int{2, 1, 0}},
		{"adlam-rtl-base", []rune{0x1E900, 0x1E901, 0x1E902}, true, []int{2, 1, 0}},
	}
	for _, c := range cases {
		if got := perm(c.runes, c.rtl); !equalInts(got, c.want) {
			t.Errorf("%s: perm %v, want %v", c.name, got, c.want)
		}
	}
}

// A combining mark on a non-Arabic RTL letter stays attached to its base
// through the reversal (Syriac has many vowel/diacritic points).
func TestSyriacCombiningMarkStaysAttached(t *testing.T) {
	// beth, alaph, pthaha-above (a combining point on the alaph).
	got := perm([]rune{0x0712, 0x0710, 0x0730}, false)
	want := []int{1, 2, 0} // alaph, its mark, then beth
	if !equalInts(got, want) {
		t.Fatalf("perm %v, want %v (mark must follow its base)", got, want)
	}
}

// These scripts are not shaped: Shape leaves a pure non-Arabic RTL line
// untouched (nil), so the renderer paints the base code points reversed.
func TestNonArabicScriptsNotShaped(t *testing.T) {
	for _, name := range []string{"syriac", "mandaic", "samaritan", "adlam"} {
		var rs []rune
		switch name {
		case "syriac":
			rs = []rune{0x072B, 0x0720, 0x0721, 0x0710}
		case "mandaic":
			rs = []rune{0x0840, 0x0841, 0x0842}
		case "samaritan":
			rs = []rune{0x0800, 0x0801, 0x0802}
		case "adlam":
			rs = []rune{0x1E900, 0x1E901, 0x1E902}
		}
		if Shape(rs) != nil {
			t.Errorf("%s should not be shaped (no presentation forms)", name)
		}
	}
}
