package trinkets

// Arabic cursive joining (Unicode Joining_Type), used by the gfx renderer to
// decide, per cell, whether to draw a kashida on each side. A kashida is only
// added where the letter AND its neighbour can actually connect, so final and
// isolated forms never sprout a stray tatweel.
//
// Most Arabic letters are Dual_Joining (join on both sides). A small,
// well-known set is Right_Joining (join only toward the PRECEDING letter, never
// the following one) — the alef/dal/ra/waw families and their Persian/Urdu
// variants. Hamza is Non_Joining. Anything outside the Arabic letter ranges
// joins nothing.

// arabicRightJoining is the Unicode Joining_Type=R set (Arabic + common
// Persian/Urdu): these letters connect to the previous letter but not the next.
var arabicRightJoining = map[rune]bool{
	0x0622: true, 0x0623: true, 0x0624: true, 0x0625: true, 0x0627: true, // alef family, waw-hamza
	0x0629: true,               // teh marbuta
	0x062F: true, 0x0630: true, // dal, thal
	0x0631: true, 0x0632: true, // ra, zay
	0x0648: true,                             // waw
	0x0671: true,                             // alef wasla
	0x0688: true, 0x0691: true, 0x0698: true, // ddal, rreh, jeh
	0x06C0: true, 0x06C3: true, // heh/teh-marbuta variants
	0x06C6: true, 0x06C7: true, 0x06C8: true, 0x06CB: true, // waw variants
	0x06D2: true, 0x06D3: true, // yeh barree
	0x06D5: true, // ae
}

// arabicNonJoining: letters that join on neither side.
var arabicNonJoining = map[rune]bool{0x0621: true} // hamza

// isArabicLetter reports whether r is an Arabic-script letter (the ranges that
// carry joining behavior — not marks, digits, or punctuation).
func isArabicLetter(r rune) bool {
	switch {
	case r >= 0x0620 && r <= 0x064A: // Arabic letters
		return true
	case r >= 0x066E && r <= 0x06D3: // extended letters (Persian/Urdu, etc.)
		return true
	case r >= 0x06D5 && r <= 0x06DC:
		return true
	case r >= 0x06FA && r <= 0x06FF:
		return true
	case r >= 0x0750 && r <= 0x077F: // Arabic Supplement
		return true
	}
	return false
}

// arabicJoinsNext reports whether r can join to the FOLLOWING letter (only
// dual-joining letters do).
func arabicJoinsNext(r rune) bool {
	return isArabicLetter(r) && !arabicRightJoining[r] && !arabicNonJoining[r]
}

// arabicJoinsPrev reports whether r can join to the PRECEDING letter (dual- and
// right-joining letters — everything but hamza and non-letters).
func arabicJoinsPrev(r rune) bool {
	return isArabicLetter(r) && !arabicNonJoining[r]
}

// arabicCellShape describes what one Arabic cell shapes and which slice of the
// shaped run belongs on screen. The renderer shapes S — a five-piece window of
// LOGICAL text, prev + tatweel + letter + tatweel + next (pieces present only
// on sides that join) — as ONE run, so the shaper produces the true joined
// forms with real connecting strokes, then cuts the neighbour letters off the
// ends by cluster position and keeps the letter plus its tatweel connectors.
// Rune ranges are half-open indices into S's runes; -1 marks an absent piece.
type arabicCellShape struct {
	s          string // logical window text (bidi/shaping handled by the engine)
	seg0, seg1 int    // the cell's own letter (or lam-alef ligature pair)
	rt0, rt1   int    // tatweel toward the logically-PREV letter (visual right)
	lt0, lt1   int    // tatweel toward the logically-NEXT letter (visual left)
}

// arabicRenderContext builds the five-piece shaping window for one cell.
// leftBase/rightBase are the VISUAL neighbours (cells are visual order, RTL
// reversed), so the logical previous letter is rightBase and the logical next
// is leftBase. kashL/kashR come from arabicKashida and are true only when both
// letters actually join across that edge, so a non-joining side contributes
// neither a neighbour nor a tatweel and the letter takes its correct
// final/initial/isolated form. form is ShapeArabicCellVisual's result, used
// only to detect lam-alef ligatures (rebuilt as the base pair so the font's
// GSUB forms the mandatory ligature; the alef half's cell is suppressed
// upstream). Legacy presentation-form codepoints are never emitted — many
// faces (notably the macOS system Arabic fonts) do not carry that block.
func arabicRenderContext(base, form rune, leftBase, rightBase rune, kashL, kashR bool) *arabicCellShape {
	var seq []rune
	switch form {
	case 0xFEF5, 0xFEF6:
		seq = []rune{'ل', 'آ'} // lam + alef with madda
	case 0xFEF7, 0xFEF8:
		seq = []rune{'ل', 'أ'} // lam + alef with hamza above
	case 0xFEF9, 0xFEFA:
		seq = []rune{'ل', 'إ'} // lam + alef with hamza below
	case 0xFEFB, 0xFEFC:
		seq = []rune{'ل', 'ا'} // lam + alef
	default:
		seq = []rune{base}
	}
	// Two tatweels per joining side: enough connecting stroke that a cell-width
	// window centred on the letter always cuts mid-tatweel at the cell edges,
	// never running out of stroke before the boundary.
	ctx := &arabicCellShape{rt0: -1, rt1: -1, lt0: -1, lt1: -1}
	var rs []rune
	if kashR { // logical prev = visual right neighbour
		rs = append(rs, rightBase, 'ـ', 'ـ')
		ctx.rt0, ctx.rt1 = 1, 3
	}
	ctx.seg0 = len(rs)
	rs = append(rs, seq...)
	ctx.seg1 = len(rs)
	if kashL { // logical next = visual left neighbour
		ctx.lt0, ctx.lt1 = len(rs), len(rs)+2
		rs = append(rs, 'ـ', 'ـ', leftBase)
	}
	ctx.s = string(rs)
	return ctx
}

// arabicKashida returns whether a cell holding base (with visual neighbours
// leftBase to its left and rightBase to its right — cells are in visual order,
// so the left neighbour is the logically-NEXT letter and the right neighbour
// the logically-PREVIOUS) should draw a kashida on its left and/or right edge.
// A kashida fills toward a neighbour only when both letters can join there.
func arabicKashida(base, leftBase, rightBase rune) (left, right bool) {
	if !isArabicLetter(base) {
		return false, false
	}
	// Right edge faces the logically-previous letter (rightBase).
	right = arabicJoinsPrev(base) && arabicJoinsNext(rightBase)
	// Left edge faces the logically-next letter (leftBase).
	left = arabicJoinsNext(base) && arabicJoinsPrev(leftBase)
	return left, right
}

// arabicPresentationBase maps an Arabic presentation-form code point
// (Forms-A/B) back to its base letter. Bidi-aware applications that emit
// visual-order text (terminal editors such as mew) pre-shape Arabic, so
// terminal cells carry PRESENTATION forms — the joining classifier and the
// shaping window must be computed from the BASE letters or no cell ever
// joins. Data is the standard Unicode Arabic Presentation Forms-A/B
// decomposition; the lam-alef ligature forms map to alef (the ligature joins
// toward the previous letter only, exactly like an alef).
var arabicPresentationBase = map[rune]rune{
	0xFB56: 0x067E,
	0xFB57: 0x067E,
	0xFB58: 0x067E,
	0xFB59: 0x067E,
	0xFB66: 0x0679,
	0xFB67: 0x0679,
	0xFB68: 0x0679,
	0xFB69: 0x0679,
	0xFB6A: 0x06A4,
	0xFB6B: 0x06A4,
	0xFB6C: 0x06A4,
	0xFB6D: 0x06A4,
	0xFB7A: 0x0686,
	0xFB7B: 0x0686,
	0xFB7C: 0x0686,
	0xFB7D: 0x0686,
	0xFB88: 0x0688,
	0xFB89: 0x0688,
	0xFB8A: 0x0698,
	0xFB8B: 0x0698,
	0xFB8C: 0x0691,
	0xFB8D: 0x0691,
	0xFB8E: 0x06A9,
	0xFB8F: 0x06A9,
	0xFB90: 0x06A9,
	0xFB91: 0x06A9,
	0xFB92: 0x06AF,
	0xFB93: 0x06AF,
	0xFB94: 0x06AF,
	0xFB95: 0x06AF,
	0xFB9E: 0x06BA,
	0xFB9F: 0x06BA,
	0xFBA4: 0x06C0,
	0xFBA5: 0x06C0,
	0xFBA6: 0x06C1,
	0xFBA7: 0x06C1,
	0xFBA8: 0x06C1,
	0xFBA9: 0x06C1,
	0xFBAA: 0x06BE,
	0xFBAB: 0x06BE,
	0xFBAC: 0x06BE,
	0xFBAD: 0x06BE,
	0xFBAE: 0x06D2,
	0xFBAF: 0x06D2,
	0xFBD3: 0x06AD,
	0xFBD4: 0x06AD,
	0xFBD5: 0x06AD,
	0xFBD6: 0x06AD,
	0xFBD7: 0x06C7,
	0xFBD8: 0x06C7,
	0xFBD9: 0x06C6,
	0xFBDA: 0x06C6,
	0xFBDB: 0x06C8,
	0xFBDC: 0x06C8,
	0xFBDE: 0x06CB,
	0xFBDF: 0x06CB,
	0xFBFC: 0x06CC,
	0xFBFD: 0x06CC,
	0xFBFE: 0x06CC,
	0xFBFF: 0x06CC,
	0xFE80: 0x0621,
	0xFE81: 0x0622,
	0xFE82: 0x0622,
	0xFE83: 0x0623,
	0xFE84: 0x0623,
	0xFE85: 0x0624,
	0xFE86: 0x0624,
	0xFE87: 0x0625,
	0xFE88: 0x0625,
	0xFE89: 0x0626,
	0xFE8A: 0x0626,
	0xFE8B: 0x0626,
	0xFE8C: 0x0626,
	0xFE8D: 0x0627,
	0xFE8E: 0x0627,
	0xFE8F: 0x0628,
	0xFE90: 0x0628,
	0xFE91: 0x0628,
	0xFE92: 0x0628,
	0xFE93: 0x0629,
	0xFE94: 0x0629,
	0xFE95: 0x062A,
	0xFE96: 0x062A,
	0xFE97: 0x062A,
	0xFE98: 0x062A,
	0xFE99: 0x062B,
	0xFE9A: 0x062B,
	0xFE9B: 0x062B,
	0xFE9C: 0x062B,
	0xFE9D: 0x062C,
	0xFE9E: 0x062C,
	0xFE9F: 0x062C,
	0xFEA0: 0x062C,
	0xFEA1: 0x062D,
	0xFEA2: 0x062D,
	0xFEA3: 0x062D,
	0xFEA4: 0x062D,
	0xFEA5: 0x062E,
	0xFEA6: 0x062E,
	0xFEA7: 0x062E,
	0xFEA8: 0x062E,
	0xFEA9: 0x062F,
	0xFEAA: 0x062F,
	0xFEAB: 0x0630,
	0xFEAC: 0x0630,
	0xFEAD: 0x0631,
	0xFEAE: 0x0631,
	0xFEAF: 0x0632,
	0xFEB0: 0x0632,
	0xFEB1: 0x0633,
	0xFEB2: 0x0633,
	0xFEB3: 0x0633,
	0xFEB4: 0x0633,
	0xFEB5: 0x0634,
	0xFEB6: 0x0634,
	0xFEB7: 0x0634,
	0xFEB8: 0x0634,
	0xFEB9: 0x0635,
	0xFEBA: 0x0635,
	0xFEBB: 0x0635,
	0xFEBC: 0x0635,
	0xFEBD: 0x0636,
	0xFEBE: 0x0636,
	0xFEBF: 0x0636,
	0xFEC0: 0x0636,
	0xFEC1: 0x0637,
	0xFEC2: 0x0637,
	0xFEC3: 0x0637,
	0xFEC4: 0x0637,
	0xFEC5: 0x0638,
	0xFEC6: 0x0638,
	0xFEC7: 0x0638,
	0xFEC8: 0x0638,
	0xFEC9: 0x0639,
	0xFECA: 0x0639,
	0xFECB: 0x0639,
	0xFECC: 0x0639,
	0xFECD: 0x063A,
	0xFECE: 0x063A,
	0xFECF: 0x063A,
	0xFED0: 0x063A,
	0xFED1: 0x0641,
	0xFED2: 0x0641,
	0xFED3: 0x0641,
	0xFED4: 0x0641,
	0xFED5: 0x0642,
	0xFED6: 0x0642,
	0xFED7: 0x0642,
	0xFED8: 0x0642,
	0xFED9: 0x0643,
	0xFEDA: 0x0643,
	0xFEDB: 0x0643,
	0xFEDC: 0x0643,
	0xFEDD: 0x0644,
	0xFEDE: 0x0644,
	0xFEDF: 0x0644,
	0xFEE0: 0x0644,
	0xFEE1: 0x0645,
	0xFEE2: 0x0645,
	0xFEE3: 0x0645,
	0xFEE4: 0x0645,
	0xFEE5: 0x0646,
	0xFEE6: 0x0646,
	0xFEE7: 0x0646,
	0xFEE8: 0x0646,
	0xFEE9: 0x0647,
	0xFEEA: 0x0647,
	0xFEEB: 0x0647,
	0xFEEC: 0x0647,
	0xFEED: 0x0648,
	0xFEEE: 0x0648,
	0xFEEF: 0x0649,
	0xFEF0: 0x0649,
	0xFEF1: 0x064A,
	0xFEF2: 0x064A,
	0xFEF3: 0x064A,
	0xFEF4: 0x064A,
	0xFEF5: 0x0627, // lam-alef ligature
	0xFEF6: 0x0627, // lam-alef ligature
	0xFEF7: 0x0627, // lam-alef ligature
	0xFEF8: 0x0627, // lam-alef ligature
	0xFEF9: 0x0627, // lam-alef ligature
	0xFEFA: 0x0627, // lam-alef ligature
	0xFEFB: 0x0627, // lam-alef ligature
	0xFEFC: 0x0627, // lam-alef ligature
}

// arabicBaseChar returns the base letter for a presentation-form code point,
// or r unchanged when r is not a presentation form.
func arabicBaseChar(r rune) rune {
	if b, ok := arabicPresentationBase[r]; ok {
		return b
	}
	return r
}
