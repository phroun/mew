package trinkets

import (
	"image"
	"testing"

	"github.com/phroun/purfecterm"
)

// The gfx renderer joins cursive Arabic by extending each letter's own
// connecting stroke from its real ink edge out to the cell boundary. These
// tests lock in that a joined form reaches the cell edge (so adjacent cells
// connect) while an unjoined form stays clear — the regression the Arabic work
// chased (a centered letter with no connecting stroke looked broken apart).

// A medial form asked to join on both sides paints ink to BOTH cell edges;
// asked to join on neither, it stays centered with clear edges.
func TestArabicKashidaReachesEdges(t *testing.T) {
	term := NewPurfecTerm()
	term.SetTerminalFontFamily("ui-term")
	term.rotateGfxCaches()
	if term.gfxEngine() == nil {
		t.Skip("no gfx engine")
	}
	const boxW, boxH, ppu = 24, 32, 1.0

	medialAin := "ﻌ" // U+FECC, joins on both sides
	joined := term.cellTextImage(medialAin, "ui-term", false, false, boxW, boxH, ppu, false, 'ﻌ', true, true)
	if !colInked(joined, 0) || !colInked(joined, boxW-1) {
		t.Errorf("joined medial form should reach both cell edges; left=%v right=%v",
			colInked(joined, 0), colInked(joined, boxW-1))
	}

	// New cache key (kashL/kashR false) — must not be served the joined mask.
	lone := term.cellTextImage(medialAin, "ui-term", false, false, boxW, boxH, ppu, false, 'ﻌ', false, false)
	if colInked(lone, 0) || colInked(lone, boxW-1) {
		t.Errorf("unjoined form should stay centered with clear edges; left=%v right=%v",
			colInked(lone, 0), colInked(lone, boxW-1))
	}
}

// End to end over a real word: laying out عليكم in visual cells and shaping +
// joining each cell as the paint loop does, every interior cell boundary is
// bridged — some row has ink in both the left cell's last column and the right
// cell's first column, so the letters visibly connect.
func TestArabicWordConnectsAcrossCells(t *testing.T) {
	term := NewPurfecTerm()
	term.SetTerminalFontFamily("ui-term")
	term.rotateGfxCaches()
	if term.gfxEngine() == nil {
		t.Skip("no gfx engine")
	}
	const boxW, boxH, ppu = 14, 30, 1.0

	// Visual order (RTL reversed to LTR cells) for logical ع ل ي ك م.
	visual := []rune{'م', 'ك', 'ي', 'ل', 'ع'}
	masks := make([]*image.RGBA, len(visual))
	for i, ch := range visual {
		var leftCh, rightCh rune
		if i > 0 {
			leftCh = visual[i-1]
		}
		if i+1 < len(visual) {
			rightCh = visual[i+1]
		}
		shaped, suppress := purfecterm.ShapeArabicCellVisual(leftCh, ch, rightCh)
		if suppress {
			masks[i] = image.NewRGBA(image.Rect(0, 0, boxW, boxH))
			continue
		}
		kashL, kashR := arabicKashida(ch, leftCh, rightCh)
		// Exercise the real render path: ZWJ-joined base letters, not the legacy
		// presentation-form codepoint.
		shapeStr := arabicRenderString(ch, shaped, kashL, kashR)
		masks[i] = term.cellTextImage(shapeStr, "ui-term", false, false, boxW, boxH, ppu, false, ch, kashL, kashR)
	}

	bridged := func(left, right *image.RGBA) bool {
		for y := 0; y < boxH; y++ {
			if left.RGBAAt(boxW-1, y).A != 0 && right.RGBAAt(0, y).A != 0 {
				return true
			}
		}
		return false
	}
	for i := 0; i+1 < len(masks); i++ {
		if !bridged(masks[i], masks[i+1]) {
			t.Errorf("cells %d(%c) and %d(%c) do not connect at their shared boundary",
				i, visual[i], i+1, visual[i+1])
		}
	}
}
