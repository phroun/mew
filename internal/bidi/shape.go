package bidi

import xbidi "golang.org/x/text/unicode/bidi"

// Arabic cursive shaping (a minimal UAX-independent implementation of the
// joining algorithm). Terminals render whatever code points we hand them left
// to right, and — since we reverse RTL runs ourselves — a terminal shaper can
// never see an Arabic run in logical order. So we substitute each Arabic
// letter with its contextual presentation form (Arabic Presentation Forms-B,
// U+FE70..U+FEFF) in LOGICAL order here; those forms are standalone glyphs
// with no joining behaviour of their own, so the renderer can reverse and
// colour them cell by cell without breaking the joins.
//
// Scope: the Arabic script (standard Arabic plus the Persian/Urdu/Kurdish/
// Uyghur extensions), initial/medial/final/isolated selection, transparent
// combining marks skipped in the join context, tatweel/ZWJ as joining-causing,
// and the mandatory lam-alef ligature (which uses LigatureAbsorbed).
//
// Only Arabic is shaped here, because only the Arabic script has Unicode
// presentation-form code points to substitute to. The other cursive RTL
// scripts — Syriac, Mandaic, Adlam, N'Ko — have no presentation forms; their
// joined shapes exist only as font/OpenType glyphs, which cannot be produced
// by code-point substitution (and would be defeated by our RTL reversal in any
// case). Those scripts still get correct bidi ordering (they read right to
// left, with combining marks kept on their base), just in nominal/isolated
// letter forms. Samaritan is non-cursive, so ordering is complete support for
// it. See scripts_test.go.

// arabicForms maps a base letter to its [isolated, final, initial, medial]
// presentation forms; 0 marks a form the letter does not have (right-joining
// and non-joining letters have no initial/medial).
var arabicForms = map[rune][4]rune{
	0x0621: {0xFE80, 0, 0, 0},                // hamza (non-joining)
	0x0622: {0xFE81, 0xFE82, 0, 0},           // alef with madda above
	0x0623: {0xFE83, 0xFE84, 0, 0},           // alef with hamza above
	0x0624: {0xFE85, 0xFE86, 0, 0},           // waw with hamza above
	0x0625: {0xFE87, 0xFE88, 0, 0},           // alef with hamza below
	0x0626: {0xFE89, 0xFE8A, 0xFE8B, 0xFE8C}, // yeh with hamza above
	0x0627: {0xFE8D, 0xFE8E, 0, 0},           // alef
	0x0628: {0xFE8F, 0xFE90, 0xFE91, 0xFE92}, // beh
	0x0629: {0xFE93, 0xFE94, 0, 0},           // teh marbuta
	0x062A: {0xFE95, 0xFE96, 0xFE97, 0xFE98}, // teh
	0x062B: {0xFE99, 0xFE9A, 0xFE9B, 0xFE9C}, // theh
	0x062C: {0xFE9D, 0xFE9E, 0xFE9F, 0xFEA0}, // jeem
	0x062D: {0xFEA1, 0xFEA2, 0xFEA3, 0xFEA4}, // hah
	0x062E: {0xFEA5, 0xFEA6, 0xFEA7, 0xFEA8}, // khah
	0x062F: {0xFEA9, 0xFEAA, 0, 0},           // dal
	0x0630: {0xFEAB, 0xFEAC, 0, 0},           // thal
	0x0631: {0xFEAD, 0xFEAE, 0, 0},           // reh
	0x0632: {0xFEAF, 0xFEB0, 0, 0},           // zain
	0x0633: {0xFEB1, 0xFEB2, 0xFEB3, 0xFEB4}, // seen
	0x0634: {0xFEB5, 0xFEB6, 0xFEB7, 0xFEB8}, // sheen
	0x0635: {0xFEB9, 0xFEBA, 0xFEBB, 0xFEBC}, // sad
	0x0636: {0xFEBD, 0xFEBE, 0xFEBF, 0xFEC0}, // dad
	0x0637: {0xFEC1, 0xFEC2, 0xFEC3, 0xFEC4}, // tah
	0x0638: {0xFEC5, 0xFEC6, 0xFEC7, 0xFEC8}, // zah
	0x0639: {0xFEC9, 0xFECA, 0xFECB, 0xFECC}, // ain
	0x063A: {0xFECD, 0xFECE, 0xFECF, 0xFED0}, // ghain
	0x0641: {0xFED1, 0xFED2, 0xFED3, 0xFED4}, // feh
	0x0642: {0xFED5, 0xFED6, 0xFED7, 0xFED8}, // qaf
	0x0643: {0xFED9, 0xFEDA, 0xFEDB, 0xFEDC}, // kaf
	0x0644: {0xFEDD, 0xFEDE, 0xFEDF, 0xFEE0}, // lam
	0x0645: {0xFEE1, 0xFEE2, 0xFEE3, 0xFEE4}, // meem
	0x0646: {0xFEE5, 0xFEE6, 0xFEE7, 0xFEE8}, // noon
	0x0647: {0xFEE9, 0xFEEA, 0xFEEB, 0xFEEC}, // heh
	0x0648: {0xFEED, 0xFEEE, 0, 0},           // waw
	0x0649: {0xFEEF, 0xFEF0, 0, 0},           // alef maksura
	0x064A: {0xFEF1, 0xFEF2, 0xFEF3, 0xFEF4}, // yeh

	// Persian / Urdu / other extended Arabic-script letters (Presentation
	// Forms-A, U+FB50..U+FBFF).
	0x067E: {0xFB56, 0xFB57, 0xFB58, 0xFB59}, // peh (پ)
	0x0679: {0xFB66, 0xFB67, 0xFB68, 0xFB69}, // tteh (ٹ, Urdu)
	0x0686: {0xFB7A, 0xFB7B, 0xFB7C, 0xFB7D}, // tcheh (چ)
	0x06A4: {0xFB6A, 0xFB6B, 0xFB6C, 0xFB6D}, // veh (ڤ)
	0x0698: {0xFB8A, 0xFB8B, 0, 0},           // jeh (ژ, right-joining)
	0x0688: {0xFB88, 0xFB89, 0, 0},           // ddal (ڈ, Urdu, right-joining)
	0x0691: {0xFB8C, 0xFB8D, 0, 0},           // rreh (ڑ, Urdu, right-joining)
	0x06A9: {0xFB8E, 0xFB8F, 0xFB90, 0xFB91}, // keheh (ک)
	0x06AF: {0xFB92, 0xFB93, 0xFB94, 0xFB95}, // gaf (گ)
	0x06AD: {0xFBD3, 0xFBD4, 0xFBD5, 0xFBD6}, // ng (ڭ)
	0x06BA: {0xFB9E, 0xFB9F, 0, 0},           // noon ghunna (ں, right-joining)
	0x06BE: {0xFBAA, 0xFBAB, 0xFBAC, 0xFBAD}, // heh doachashmee (ھ)
	0x06C0: {0xFBA4, 0xFBA5, 0, 0},           // heh with yeh above (ۀ)
	0x06C1: {0xFBA6, 0xFBA7, 0xFBA8, 0xFBA9}, // heh goal (ہ)
	0x06CC: {0xFBFC, 0xFBFD, 0xFBFE, 0xFBFF}, // farsi yeh (ی)
	0x06D2: {0xFBAE, 0xFBAF, 0, 0},           // yeh barree (ے, right-joining)
	0x06C6: {0xFBD9, 0xFBDA, 0, 0},           // oe (ۆ, Kurdish/Uyghur)
	0x06C7: {0xFBD7, 0xFBD8, 0, 0},           // u (ۇ)
	0x06C8: {0xFBDB, 0xFBDC, 0, 0},           // yu (ۈ)
	0x06CB: {0xFBDE, 0xFBDF, 0, 0},           // ve (ۋ)
}

// LigatureAbsorbed marks the second logical position of a mandatory ligature
// (the alef of a lam-alef pair): the glyph lives on the first position and
// this one takes no cell of its own. Selecting either position highlights the
// shared cell; deleting either code point breaks the ligature apart, since the
// remaining letter re-shapes on its own.
const LigatureAbsorbed rune = -1

// isAlefVariant reports whether r is an alef that forms a lam-alef ligature.
func isAlefVariant(r rune) bool {
	switch r {
	case 0x0622, 0x0623, 0x0625, 0x0627:
		return true
	}
	return false
}

// lamAlefLigature returns the [isolated, final] ligature glyphs for lam + the
// given alef variant.
func lamAlefLigature(alef rune) (isolated, final rune) {
	switch alef {
	case 0x0622: // alef with madda
		return 0xFEF5, 0xFEF6
	case 0x0623: // alef with hamza above
		return 0xFEF7, 0xFEF8
	case 0x0625: // alef with hamza below
		return 0xFEF9, 0xFEFA
	default: // plain alef (0x0627)
		return 0xFEFB, 0xFEFC
	}
}

type joinType uint8

const (
	joinNone        joinType = iota // U: no joining
	joinRight                       // R: joins only to the previous letter
	joinDual                        // D: joins both sides
	joinCausing                     // C: tatweel, ZWJ
	joinTransparent                 // T: combining marks (skipped in context)
)

// joiningTypeOf classifies a rune for the shaping join context.
func joiningTypeOf(r rune) joinType {
	if classOf(r) == xbidi.NSM {
		return joinTransparent // harakat and other combining marks
	}
	if r == 0x0640 || r == 0x200D { // tatweel, ZERO WIDTH JOINER
		return joinCausing
	}
	if f, ok := arabicForms[r]; ok {
		switch {
		case f[2] != 0:
			return joinDual
		case f[1] != 0:
			return joinRight
		}
	}
	return joinNone
}

// prevMeaningful / nextMeaningful return the joining type of the nearest
// non-transparent neighbour (joinNone when there is none).
func prevMeaningful(runes []rune, i int) joinType {
	for j := i - 1; j >= 0; j-- {
		if t := joiningTypeOf(runes[j]); t != joinTransparent {
			return t
		}
	}
	return joinNone
}

func nextMeaningful(runes []rune, i int) joinType {
	for j := i + 1; j < len(runes); j++ {
		if t := joiningTypeOf(runes[j]); t != joinTransparent {
			return t
		}
	}
	return joinNone
}

// Shape returns a copy of runes with each Arabic letter replaced by its
// contextual presentation form, or nil if the line contains no Arabic letter
// (the common fast path). Combining marks and every non-Arabic rune are copied
// through unchanged, so the result stays one rune per input rune.
func Shape(runes []rune) []rune {
	hasArabic := false
	for _, r := range runes {
		if _, ok := arabicForms[r]; ok {
			hasArabic = true
			break
		}
	}
	if !hasArabic {
		return nil
	}

	out := make([]rune, len(runes))
	copy(out, runes)
	for i, r := range runes {
		if out[i] == LigatureAbsorbed {
			continue // the alef of a lam-alef pair, already handled
		}

		// Mandatory lam-alef ligature: a lam immediately followed by an alef
		// variant becomes a single ligature glyph on the lam, with the alef
		// absorbed into the same cell. Its form is final when the lam joins a
		// preceding letter, isolated otherwise (the alef terminates joining).
		if r == 0x0644 && i+1 < len(runes) && isAlefVariant(runes[i+1]) {
			iso, fin := lamAlefLigature(runes[i+1])
			if prevMeaningful(runes, i) == joinDual || prevMeaningful(runes, i) == joinCausing {
				out[i] = fin
			} else {
				out[i] = iso
			}
			out[i+1] = LigatureAbsorbed
			continue
		}

		f, ok := arabicForms[r]
		if !ok {
			continue
		}
		t := joiningTypeOf(r)
		if t != joinRight && t != joinDual {
			continue // hamza and the like keep their isolated form
		}
		// A letter joins its previous neighbour when that neighbour links
		// forward (dual or causing), and joins its next neighbour when that
		// neighbour links backward (dual, right-joining, or causing) — the
		// latter only possible for a dual-joining letter.
		p := prevMeaningful(runes, i)
		joinsPrev := p == joinDual || p == joinCausing
		n := nextMeaningful(runes, i)
		joinsNext := t == joinDual && (n == joinDual || n == joinRight || n == joinCausing)

		var form int
		switch {
		case joinsPrev && joinsNext:
			form = 3 // medial
		case joinsPrev:
			form = 1 // final
		case joinsNext:
			form = 2 // initial
		default:
			form = 0 // isolated
		}
		if g := f[form]; g != 0 {
			out[i] = g
		} else {
			out[i] = f[0]
		}
	}
	return out
}
