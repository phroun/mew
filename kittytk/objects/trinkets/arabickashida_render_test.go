package trinkets

import (
	"image"
	"strings"
	"testing"

	"github.com/phroun/purfecterm"
)

// The gfx renderer joins cursive Arabic by shaping a five-piece window —
// prev + tatweel + letter + tatweel + next — as ONE run (so the font's GSUB
// produces the true joined forms) and cutting this cell's piece out by cluster
// position, stretching the tatweel connectors to the cell edges. These tests
// lock in that the joined pieces really reach the edges and that adjacent
// cells connect at their shared boundary.

// renderArabicCell mirrors the paint path for one visual cell of a word.
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
	kashL, kashR := arabicKashida(visual[i], leftCh, rightCh)
	actx := arabicRenderContext(visual[i], shaped, leftCh, rightCh, kashL, kashR)
	return term.cellTextImage(actx.s, "ui-term", false, false, boxW, boxH, ppu, false, visual[i], kashL, kashR, actx)
}

// A medial letter with joining neighbours paints ink to BOTH cell edges; an
// isolated letter (space neighbours) keeps clear edges.
func TestArabicKashidaReachesEdges(t *testing.T) {
	term := NewPurfecTerm()
	term.SetTerminalFontFamily("ui-term")
	term.rotateGfxCaches()
	if term.gfxEngine() == nil {
		t.Skip("no gfx engine")
	}
	const boxW, boxH, ppu = 24, 32, 1.0

	// The mask is exactly one cell: a joined medial letter carries ink at BOTH
	// edge columns (the slice cuts mid-stroke); an isolated letter does not.
	joined := renderArabicCell(t, term, []rune{'ل', 'ي', 'ك'}, 1, boxW, boxH, ppu)
	if !colInked(joined, 0) || !colInked(joined, boxW-1) {
		t.Errorf("medial yeh should reach both cell edges; left=%v right=%v",
			colInked(joined, 0), colInked(joined, boxW-1))
	}

	lone := renderArabicCell(t, term, []rune{' ', 'ي', ' '}, 1, boxW, boxH, ppu)
	if colInked(lone, 0) || colInked(lone, boxW-1) {
		t.Errorf("isolated yeh should keep clear edges; left=%v right=%v",
			colInked(lone, 0), colInked(lone, boxW-1))
	}
}

// End to end over عليكم: every interior cell boundary is bridged — some row has
// ink in both the left cell's last column and the right cell's first column.
// The stitched word is logged as ASCII so joins can be inspected by eye.
func TestArabicWordConnectsAcrossCells(t *testing.T) {
	term := NewPurfecTerm()
	term.SetTerminalFontFamily("ui-term")
	term.rotateGfxCaches()
	if term.gfxEngine() == nil {
		t.Skip("no gfx engine")
	}
	const boxW, boxH, ppu = 16, 30, 2.0

	// Visual order (RTL reversed to LTR cells) for logical ع ل ي ك م.
	visual := []rune{'م', 'ك', 'ي', 'ل', 'ع'}
	masks := make([]*image.RGBA, len(visual))
	for i := range visual {
		masks[i] = renderArabicCell(t, term, visual, i, boxW, boxH, ppu)
		// Show each cell's FINAL clip in full.
		var cb strings.Builder
		cb.WriteString("\ncell ")
		cb.WriteRune(visual[i])
		cb.WriteString(":\n")
		for y := 0; y < boxH; y++ {
			for x := 0; x < masks[i].Rect.Dx(); x++ {
				if x > 0 && x%boxW == 0 {
					cb.WriteString("|")
				}
				if masks[i].RGBAAt(x, y).A > 32 {
					cb.WriteString("#")
				} else {
					cb.WriteString(".")
				}
			}
			cb.WriteString("\n")
		}
		t.Log(cb.String())
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

	// Every interior cell boundary of the stitched word carries ink.
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
			t.Errorf("cells %d(%c) and %d(%c) do not connect at their shared boundary",
				i-1, visual[i-1], i, visual[i])
		}
	}
}
