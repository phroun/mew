package trinkets

import (
	"image"
	"strings"
	"testing"

	"github.com/phroun/purfecterm"
)

// The gfx renderer joins cursive Arabic by mapping each cell (and its
// neighbours) back to base letters, then shaping a window — prev + tatweel +
// letter + tatweel + next — as ONE run so the font's GSUB produces the true
// joined forms, and cutting this cell's piece out by cluster position.
//
// THE REGRESSION THIS FILE GUARDS: bidi-aware applications (e.g. mew) emit
// Arabic as PRESENTATION FORMS, not base letters. For a long time the join
// logic read those presentation forms directly, the base-only classifier saw
// every cell as non-joining, and nothing ever connected — while every test
// that fed BASE letters passed. So these tests feed BOTH encodings and assert
// the renderer treats them identically. A test that only feeds base letters
// would not have caught the bug and must never be the only coverage again.

// renderArabicCell mirrors paintGraphical EXACTLY for one visual cell: it maps
// the raw cell chars (which may be presentation forms, as emitting apps send)
// back to base letters via arabicBaseChar before computing joins and the shaping
// window. Do not "simplify" this to pass the raw runes through — that
// divergence from the paint path is the original bug.
func renderArabicCell(t *testing.T, term *PurfecTerm, visual []rune, i, boxW, boxH int, ppu float64) *image.RGBA {
	t.Helper()
	var leftCh, rightCh rune
	if i > 0 {
		leftCh = visual[i-1]
	}
	if i+1 < len(visual) {
		rightCh = visual[i+1]
	}
	shaped, suppress := purfecterm.ShapeArabicCellVisual(leftCh, visual[i], rightCh)
	if suppress {
		return image.NewRGBA(image.Rect(0, 0, boxW, boxH))
	}
	baseC := arabicBaseChar(visual[i])
	baseL := arabicBaseChar(leftCh)
	baseR := arabicBaseChar(rightCh)
	kashL, kashR := arabicKashida(baseC, baseL, baseR)
	actx := arabicRenderContext(baseC, shaped, baseL, baseR, kashL, kashR)
	return term.cellTextImage(actx.s, "ui-term", false, false, boxW, boxH, ppu, false, baseC, kashL, kashR, actx)
}

// encodePresentation turns a visual word of BASE letters into the
// presentation-form cells a pre-shaping application emits — each cell shaped
// by its base neighbours — so a render test can feed the renderer exactly the
// production wire format.
func encodePresentation(visual []rune) []rune {
	out := make([]rune, len(visual))
	for i, ch := range visual {
		var l, r rune
		if i > 0 {
			l = visual[i-1]
		}
		if i+1 < len(visual) {
			r = visual[i+1]
		}
		shaped, suppress := purfecterm.ShapeArabicCellVisual(l, ch, r)
		if suppress {
			shaped = ' '
		}
		out[i] = shaped
	}
	return out
}

func imagesEqual(a, b *image.RGBA) bool {
	if a.Rect != b.Rect {
		return false
	}
	for y := a.Rect.Min.Y; y < a.Rect.Max.Y; y++ {
		for x := a.Rect.Min.X; x < a.Rect.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				return false
			}
		}
	}
	return true
}

// THE core regression guard: the renderer must produce byte-identical masks
// whether a word arrives as base letters or as pre-shaped presentation forms.
// The historical bug made the base encoding join and the presentation
// encoding not — so equality is exactly the invariant that was broken.
func TestArabicRenderEncodingIndependent(t *testing.T) {
	term := NewPurfecTerm()
	term.SetTerminalFontFamily("ui-term")
	term.rotateGfxCaches()
	if term.gfxEngine() == nil {
		t.Skip("no gfx engine")
	}
	const boxW, boxH, ppu = 16, 30, 2.0

	// عليكم in visual order (no lam-alef, so no suppressed cells to align).
	base := []rune{'م', 'ك', 'ي', 'ل', 'ع'}
	pres := encodePresentation(base)

	// Sanity: the two encodings really are different bytes (else the test is
	// vacuous — it would pass even if arabicBaseChar were the identity).
	diff := false
	for i := range base {
		if base[i] != pres[i] {
			diff = true
		}
	}
	if !diff {
		t.Fatal("presentation encoding equals base encoding — test would be vacuous")
	}

	for i := range base {
		mb := renderArabicCell(t, term, base, i, boxW, boxH, ppu)
		mp := renderArabicCell(t, term, pres, i, boxW, boxH, ppu)
		if !imagesEqual(mb, mp) {
			t.Errorf("cell %d (%c / %c): base and presentation-form encodings render differently — the presentation-form input path has regressed",
				i, base[i], pres[i])
		}
	}
}

// A medial letter with joining neighbours paints ink to BOTH cell edges; an
// isolated letter keeps clear edges. Run over the presentation-form encoding —
// the pre-shaping apps' wire format — so this asserts the actual production path.
func TestArabicKashidaReachesEdges(t *testing.T) {
	term := NewPurfecTerm()
	term.SetTerminalFontFamily("ui-term")
	term.rotateGfxCaches()
	if term.gfxEngine() == nil {
		t.Skip("no gfx engine")
	}
	const boxW, boxH, ppu = 24, 32, 1.0

	for _, enc := range []struct {
		name string
		word []rune
	}{
		{"base", []rune{'ل', 'ي', 'ك'}},
		{"presentation", encodePresentation([]rune{'ل', 'ي', 'ك'})},
	} {
		joined := renderArabicCell(t, term, enc.word, 1, boxW, boxH, ppu)
		if !colInked(joined, 0) || !colInked(joined, boxW-1) {
			t.Errorf("%s: medial yeh should reach both cell edges; left=%v right=%v",
				enc.name, colInked(joined, 0), colInked(joined, boxW-1))
		}
	}

	lone := renderArabicCell(t, term, []rune{' ', 'ي', ' '}, 1, boxW, boxH, ppu)
	if colInked(lone, 0) || colInked(lone, boxW-1) {
		t.Errorf("isolated yeh should keep clear edges; left=%v right=%v",
			colInked(lone, 0), colInked(lone, boxW-1))
	}
}

// End to end over عليكم (presentation-form encoding): every interior cell
// boundary is bridged. The stitched word is logged as ASCII for eyeballing.
func TestArabicWordConnectsAcrossCells(t *testing.T) {
	term := NewPurfecTerm()
	term.SetTerminalFontFamily("ui-term")
	term.rotateGfxCaches()
	if term.gfxEngine() == nil {
		t.Skip("no gfx engine")
	}
	const boxW, boxH, ppu = 16, 30, 2.0

	// Visual order for logical ع ل ي ك م, in presentation forms (the wire format).
	visual := encodePresentation([]rune{'م', 'ك', 'ي', 'ل', 'ع'})
	masks := make([]*image.RGBA, len(visual))
	for i := range visual {
		masks[i] = renderArabicCell(t, term, visual, i, boxW, boxH, ppu)
	}

	stitched := image.NewRGBA(image.Rect(0, 0, boxW*len(masks), boxH))
	for i, m := range masks {
		if m != nil {
			compositeInto(stitched, m, i*boxW, 0)
		}
	}
	var sb strings.Builder
	sb.WriteString("\n")
	for y := 0; y < boxH; y++ {
		for x := 0; x < boxW*len(masks); x++ {
			if x > 0 && x%boxW == 0 {
				sb.WriteString("|")
			}
			if stitched.RGBAAt(x, y).A > 32 {
				sb.WriteString("#")
			} else {
				sb.WriteString(".")
			}
		}
		sb.WriteString("\n")
	}
	t.Log(sb.String())

	for i := 1; i < len(masks); i++ {
		x := i * boxW
		found := false
		for y := 0; y < boxH; y++ {
			if stitched.RGBAAt(x-1, y).A != 0 && stitched.RGBAAt(x, y).A != 0 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("cells %d and %d do not connect at their shared boundary", i-1, i)
		}
	}
}

// arabicBaseChar must invert whatever contextual form the pipeline's shaper
// (purfecterm.ShapeArabicCellVisual — the same call the paint path makes) can
// produce for a letter. Tying the coverage to the actual shaper, not a
// hand-list, means adding a letter to the shaper without extending the reverse
// map fails HERE instead of silently on screen.
func TestArabicBaseCharInvertsShaper(t *testing.T) {
	// A spread of dual-, right-, and non-joining letters, incl. Persian/Urdu.
	for _, base := range []rune{
		0x0628, 0x062C, 0x0633, 0x0639, 0x0643, 0x0644, 0x0645, 0x0646, 0x064A, // dual
		0x0627, 0x062F, 0x0631, 0x0648, // right-joining
		0x0621,                         // hamza (non-joining)
		0x067E, 0x0686, 0x06A9, 0x06AF, // peh, tcheh, keheh, gaf
	} {
		// Every neighbour combination exercises isolated/initial/medial/final.
		for _, l := range []rune{0, 0x0628} { // no next / dual next
			for _, r := range []rune{0, 0x0628} { // no prev / dual prev
				form, suppress := purfecterm.ShapeArabicCellVisual(l, base, r)
				if suppress || form == 0 {
					continue
				}
				if got := arabicBaseChar(form); got != base {
					t.Errorf("shaper made U+%04X from base U+%04X (l=%U r=%U), but arabicBaseChar(U+%04X)=U+%04X — reverse map is missing this form",
						form, base, l, r, form, got)
				}
			}
		}
	}
	// Non-form runes pass through unchanged.
	for _, r := range []rune{'A', '5', 0x0644, ' '} { // ascii, base letter, space
		if got := arabicBaseChar(r); got != r {
			t.Errorf("arabicBaseChar(U+%04X)=U+%04X, want passthrough", r, got)
		}
	}
}
