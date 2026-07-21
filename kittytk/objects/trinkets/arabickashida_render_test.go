package trinkets

import (
	"image"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/purfecterm"
)

// The gfx renderer joins cursive Arabic by filling the gap between a centered
// letter and the cell edge with the font's own kashida (U+0640 tatweel). These
// tests lock in that the join actually renders and reaches the cell boundary,
// so adjacent cells connect — the regression the "font-native kashida" work
// chased (a centered letter with no connecting stroke looked broken apart).

func colInked(img *image.RGBA, x int) bool {
	b := img.Bounds()
	if x < b.Min.X || x >= b.Max.X {
		return false
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		if img.RGBAAt(x, y).A != 0 {
			return true
		}
	}
	return false
}

// The embedded Arabic fallback provides a tatweel, and a medial form asked to
// join on both sides paints ink to BOTH cell edges; asked to join on neither,
// it stays centered with clear edges.
func TestArabicKashidaReachesEdges(t *testing.T) {
	term := NewPurfecTerm()
	term.SetTerminalFontFamily("ui-term")
	term.rotateGfxCaches()
	if term.gfxEngine() == nil {
		t.Skip("no gfx engine")
	}
	const boxW, boxH, ppu = 24, 32, 1.0

	f := &core.Font{Name: "ui-term", Size: boxH * 3 / 4}
	if km := term.kashidaMask(f, boxH, ppu); km == nil {
		t.Fatalf("kashidaMask returned nil — the embedded Arabic fallback has no tatweel")
	}

	medialAin := "ﻌ" // U+FECC, joins on both sides
	joined := term.cellTextImage(medialAin, "ui-term", false, false, boxW, boxH, ppu, false, 'ﻌ', true, true)
	if !colInked(joined, 0) || !colInked(joined, boxW-1) {
		t.Errorf("joined medial form should reach both cell edges; left=%v right=%v",
			colInked(joined, 0), colInked(joined, boxW-1))
	}

	// New key (kashL/kashR false) — must not be served the joined mask from cache.
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
		masks[i] = term.cellTextImage(string(shaped), "ui-term", false, false, boxW, boxH, ppu, false, ch, kashL, kashR)
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
