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
