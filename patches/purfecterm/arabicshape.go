package purfecterm

// Arabic contextual shaping for per-cell renderers.
//
// A cell-grid renderer draws one cell at a time, so a shaping engine (Pango,
// Qt, go-text) is never given the neighboring letters and always produces the
// ISOLATED form — Arabic comes out broken apart. Rather than restructure the
// renderers around run shaping, each cell's glyph is resolved to its Unicode
// Arabic Presentation Forms variant (isolated/final/initial/medial) from the
// neighbor cells' base characters, and the renderer draws that variant — the
// classic cell-terminal approach, matching what a shaping terminal displays.
//
// Cells are assumed to hold VISUAL order (an RTL run's letters reversed into
// left-to-right cells), which is what bidi-implementing applications — mew —
// emit: the visually-RIGHT neighbor is the logically PREVIOUS letter and the
// visually-LEFT neighbor the logically NEXT.
//
// This is the shared shaper for every per-cell renderer (gtk, qt, and the
// KittyTK gfx trinket, which carries a private copy until this lands).

// arabForm holds a letter's presentation forms. fin != 0 means the letter can
// join to the logically-previous letter; dual means it also reaches forward to
// the logically-next one.
type arabForm struct {
	iso, fin, ini, med rune
	dual               bool
}

var arabForms = map[rune]arabForm{
	0x0621: {iso: 0xFE80},                          // hamza (non-joining)
	0x0622: {iso: 0xFE81, fin: 0xFE82},             // alef madda
	0x0623: {iso: 0xFE83, fin: 0xFE84},             // alef hamza above
	0x0624: {iso: 0xFE85, fin: 0xFE86},             // waw hamza
	0x0625: {iso: 0xFE87, fin: 0xFE88},             // alef hamza below
	0x0626: {0xFE89, 0xFE8A, 0xFE8B, 0xFE8C, true}, // yeh hamza
	0x0627: {iso: 0xFE8D, fin: 0xFE8E},             // alef
	0x0628: {0xFE8F, 0xFE90, 0xFE91, 0xFE92, true}, // beh
	0x0629: {iso: 0xFE93, fin: 0xFE94},             // teh marbuta
	0x062A: {0xFE95, 0xFE96, 0xFE97, 0xFE98, true}, // teh
	0x062B: {0xFE99, 0xFE9A, 0xFE9B, 0xFE9C, true}, // theh
	0x062C: {0xFE9D, 0xFE9E, 0xFE9F, 0xFEA0, true}, // jeem
	0x062D: {0xFEA1, 0xFEA2, 0xFEA3, 0xFEA4, true}, // hah
	0x062E: {0xFEA5, 0xFEA6, 0xFEA7, 0xFEA8, true}, // khah
	0x062F: {iso: 0xFEA9, fin: 0xFEAA},             // dal
	0x0630: {iso: 0xFEAB, fin: 0xFEAC},             // thal
	0x0631: {iso: 0xFEAD, fin: 0xFEAE},             // reh
	0x0632: {iso: 0xFEAF, fin: 0xFEB0},             // zain
	0x0633: {0xFEB1, 0xFEB2, 0xFEB3, 0xFEB4, true}, // seen
	0x0634: {0xFEB5, 0xFEB6, 0xFEB7, 0xFEB8, true}, // sheen
	0x0635: {0xFEB9, 0xFEBA, 0xFEBB, 0xFEBC, true}, // sad
	0x0636: {0xFEBD, 0xFEBE, 0xFEBF, 0xFEC0, true}, // dad
	0x0637: {0xFEC1, 0xFEC2, 0xFEC3, 0xFEC4, true}, // tah
	0x0638: {0xFEC5, 0xFEC6, 0xFEC7, 0xFEC8, true}, // zah
	0x0639: {0xFEC9, 0xFECA, 0xFECB, 0xFECC, true}, // ain
	0x063A: {0xFECD, 0xFECE, 0xFECF, 0xFED0, true}, // ghain
	0x0640: {0x0640, 0x0640, 0x0640, 0x0640, true}, // tatweel (is the join stroke)
	0x0641: {0xFED1, 0xFED2, 0xFED3, 0xFED4, true}, // feh
	0x0642: {0xFED5, 0xFED6, 0xFED7, 0xFED8, true}, // qaf
	0x0643: {0xFED9, 0xFEDA, 0xFEDB, 0xFEDC, true}, // kaf
	0x0644: {0xFEDD, 0xFEDE, 0xFEDF, 0xFEE0, true}, // lam
	0x0645: {0xFEE1, 0xFEE2, 0xFEE3, 0xFEE4, true}, // meem
	0x0646: {0xFEE5, 0xFEE6, 0xFEE7, 0xFEE8, true}, // noon
	0x0647: {0xFEE9, 0xFEEA, 0xFEEB, 0xFEEC, true}, // heh
	0x0648: {iso: 0xFEED, fin: 0xFEEE},             // waw
	0x0649: {iso: 0xFEEF, fin: 0xFEF0},             // alef maksura
	0x064A: {0xFEF1, 0xFEF2, 0xFEF3, 0xFEF4, true}, // yeh
	0x067E: {0xFB56, 0xFB57, 0xFB58, 0xFB59, true}, // peh (Persian)
	0x0686: {0xFB7A, 0xFB7B, 0xFB7C, 0xFB7D, true}, // tcheh (Persian)
	0x0698: {iso: 0xFB8A, fin: 0xFB8B},             // jeh (Persian)
	0x06A9: {0xFB8E, 0xFB8F, 0xFB90, 0xFB91, true}, // keheh (Persian)
	0x06AF: {0xFB92, 0xFB93, 0xFB94, 0xFB95, true}, // gaf (Persian)
	0x06CC: {0xFBFC, 0xFBFD, 0xFBFE, 0xFBFF, true}, // farsi yeh
}

// lamAlefForms maps an alef variant to its {isolated, final} lam-alef ligature
// (the mandatory Arabic ligature).
var lamAlefForms = map[rune][2]rune{
	0x0622: {0xFEF5, 0xFEF6},
	0x0623: {0xFEF7, 0xFEF8},
	0x0625: {0xFEF9, 0xFEFA},
	0x0627: {0xFEFB, 0xFEFC},
}

// ShapeArabicCellVisual resolves the glyph a cell should DRAW for base character c
// given its visually-left and visually-right neighbor base characters (visual
// order: right = logically previous, left = logically next). Non-Arabic (or
// unknown) characters return unchanged. suppress reports the cell is the alef
// half of a lam-alef pair, whose ligature is drawn in the lam's cell — the
// caller paints the background but no glyph.
func ShapeArabicCellVisual(left, c, right rune) (glyph rune, suppress bool) {
	f, ok := arabForms[c]
	if !ok {
		return c, false
	}
	logPrev, logNext := right, left

	// The mandatory lam-alef ligature: the lam draws it; the alef vanishes.
	if c == 0x0644 {
		if la, isAlef := lamAlefForms[logNext]; isAlef {
			if pf, ok2 := arabForms[logPrev]; ok2 && pf.dual {
				return la[1], false // joined to the previous letter: final
			}
			return la[0], false
		}
	}
	if _, isAlef := lamAlefForms[c]; isAlef && logPrev == 0x0644 {
		return 0, true
	}

	// joinPrev: the previous letter reaches forward (dual-joining).
	// joinNext: this letter reaches forward AND the next can join backward.
	joinPrev := false
	if pf, ok2 := arabForms[logPrev]; ok2 && pf.dual {
		joinPrev = true
	}
	joinNext := false
	if f.dual {
		if nf, ok2 := arabForms[logNext]; ok2 && nf.fin != 0 {
			joinNext = true
		}
	}

	switch {
	case joinPrev && joinNext && f.med != 0:
		return f.med, false
	case joinNext && !joinPrev && f.ini != 0:
		return f.ini, false
	case joinPrev && f.fin != 0:
		return f.fin, false
	}
	if f.iso != 0 {
		return f.iso, false
	}
	return c, false
}
