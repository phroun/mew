// Package hebrew folds Hebrew combining points into the single
// Alphabetic-Presentation-Form glyph that carries them, so a terminal that
// mispositions a free-standing point (drawing a dagesh, dot or rafe a cell off
// its base) renders the letter correctly. It is standard-library-only and
// mew-free, so it is shared by every surface that emits Hebrew to such a terminal
// and can be contributed upstream.
package hebrew

// dottedCircle is U+25CC, the base an isolated combining mark is anchored on.
const dottedCircle = 0x25CC

// dageshForm maps a Hebrew base letter + dagesh/mapiq to its presentation form.
// Letters with no such form (het, ayin, the final mem/nun/tsadi) are absent;
// their dagesh is dropped by PrecomposeCluster.
var dageshForm = map[rune]rune{
	0x05D0: 0xFB30, // alef + mapiq
	0x05D1: 0xFB31, // bet
	0x05D2: 0xFB32, // gimel
	0x05D3: 0xFB33, // dalet
	0x05D4: 0xFB34, // he + mapiq
	0x05D5: 0xFB35, // vav
	0x05D6: 0xFB36, // zayin
	0x05D8: 0xFB38, // tet
	0x05D9: 0xFB39, // yod
	0x05DA: 0xFB3A, // final kaf
	0x05DB: 0xFB3B, // kaf
	0x05DC: 0xFB3C, // lamed
	0x05DE: 0xFB3E, // mem
	0x05E0: 0xFB40, // nun
	0x05E1: 0xFB41, // samekh
	0x05E3: 0xFB43, // final pe
	0x05E4: 0xFB44, // pe
	0x05E6: 0xFB46, // tsadi
	0x05E7: 0xFB47, // qof
	0x05E8: 0xFB48, // resh
	0x05E9: 0xFB49, // shin (bare dagesh, no dot)
	0x05EA: 0xFB4A, // tav
}

// rafeForm maps a Hebrew base letter + rafe to its presentation form (only bet,
// kaf and pe have one).
var rafeForm = map[rune]rune{
	0x05D1: 0xFB4C, // bet
	0x05DB: 0xFB4D, // kaf
	0x05E4: 0xFB4E, // pe
}

// Folds reports whether r is a Hebrew point that folds into its base's
// presentation form: the dagesh/mapiq, shin dot, sin dot, rafe, or the
// holam-haser-for-vav. Vowels and accents do not fold.
func Folds(r rune) bool {
	switch r {
	case 0x05BC, 0x05C1, 0x05C2, 0x05BF, 0x05BA:
		return true
	}
	return false
}

// PrecomposeCluster folds a Hebrew cluster — a base rune followed by its
// combining marks — for a terminal that mishandles free-standing points. It
// returns the runes to emit: the base with its folding points folded into one
// presentation-form glyph, followed by the vowels/accents that ride normally;
// and ok=true when a fold happened.
//
// An isolated point anchored on a dotted circle (◌ + shin/sin dot, or ◌ +
// holam-haser) is shown on its faux base — the shin-with-dot or vav-with-holam
// glyph — since those points have no meaningful isolated rendering. A dagesh
// whose letter has no presentation form is dropped. ok=false when nothing folds
// (no such point, or a non-Hebrew base), and the caller emits the cluster as-is.
func PrecomposeCluster(runes []rune) ([]rune, bool) {
	if len(runes) < 2 {
		return nil, false
	}
	base := runes[0]

	if base == dottedCircle {
		switch runes[1] {
		case 0x05C1:
			return []rune{0xFB2A}, true // shin with shin dot
		case 0x05C2:
			return []rune{0xFB2B}, true // shin with sin dot
		case 0x05BA:
			return []rune{0xFB4B}, true // vav with holam
		}
		return nil, false
	}
	if base < 0x05D0 || base > 0x05EA {
		return nil, false // not a Hebrew base letter
	}

	var hasDagesh, hasShinDot, hasSinDot, hasRafe, hasHolamHaser bool
	vowels := make([]rune, 0, len(runes)-1)
	for _, m := range runes[1:] {
		switch {
		case m == 0x05BC:
			hasDagesh = true
		case m == 0x05C1:
			hasShinDot = true
		case m == 0x05C2:
			hasSinDot = true
		case m == 0x05BF:
			hasRafe = true
		case m == 0x05BA && base == 0x05D5:
			hasHolamHaser = true
		default:
			vowels = append(vowels, m)
		}
	}
	if !hasDagesh && !hasShinDot && !hasSinDot && !hasRafe && !hasHolamHaser {
		return nil, false
	}

	pre := base // fall back to the bare letter, dropping an un-formable point
	switch {
	case base == 0x05E9 && hasDagesh && hasShinDot:
		pre = 0xFB2C // shin with dagesh and shin dot
	case base == 0x05E9 && hasDagesh && hasSinDot:
		pre = 0xFB2D // shin with dagesh and sin dot
	case base == 0x05E9 && hasShinDot:
		pre = 0xFB2A
	case base == 0x05E9 && hasSinDot:
		pre = 0xFB2B
	case hasHolamHaser:
		pre = 0xFB4B
	case hasDagesh:
		if f, ok := dageshForm[base]; ok {
			pre = f
		}
	case hasRafe:
		if f, ok := rafeForm[base]; ok {
			pre = f
		}
	}
	return append([]rune{pre}, vowels...), true
}

// ComposedBase folds base + its folding points into the single presentation-form
// glyph, ignoring any vowels. It is PrecomposeCluster's base rune alone — for
// callers that fold the base but handle the vowels themselves (e.g. drift, which
// moves the vowels to another cell). Returns the base unchanged, ok=false, when
// nothing folds.
func ComposedBase(runes []rune) (rune, bool) {
	folded, ok := PrecomposeCluster(runes)
	if !ok {
		if len(runes) > 0 {
			return runes[0], false
		}
		return 0, false
	}
	return folded[0], true
}
