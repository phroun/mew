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
	0x0629: true,                             // teh marbuta
	0x062F: true, 0x0630: true,               // dal, thal
	0x0631: true, 0x0632: true,               // ra, zay
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
