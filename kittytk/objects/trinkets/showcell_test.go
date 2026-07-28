package trinkets

import (
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/text"
)

// ONE CELL, full transparency: the yeh of عليكم. Shows (1) the WHOLE shaped
// window with the tatweels, uncropped, with every span marked; (2) the final
// 3-cell mask the renderer produces from it.
func TestShowYehCell(t *testing.T) {
	term := NewPurfecTerm()
	term.SetTerminalFontFamily("ui-term")
	term.rotateGfxCaches()
	eng := term.gfxEngine()
	if eng == nil {
		t.Skip("no gfx engine")
	}
	// Production geometry (measured): cell 7x16 units, pt 12, retina ppu 2.
	const ppu = 2.0
	const boxW, boxH = 14, 32
	pt := int(math.Round(float64(boxH)/ppu)) * 3 / 4
	f := &core.Font{Name: "ui-term", Size: pt}

	// yeh cell: visual left = ك (logical next), visual right = ل (logical prev)
	actx := arabicRenderContext('ي', 0xFEF4, 'ك', 'ل', true, true)
	t.Logf("window=%q  seg=[%d,%d) rt=[%d,%d) lt=[%d,%d)", actx.s, actx.seg0, actx.seg1, actx.rt0, actx.rt1, actx.lt0, actx.lt1)

	sp := eng.ShapeRun(f, actx.s)
	w := int(math.Round(float64(sp.Width()) * ppu))
	h := int(math.Round(float64(eng.LineHeight(f)) * ppu))
	raw := image.NewRGBA(image.Rect(0, 0, w, h))
	text.Render(raw, sp, 0, 0, ppu, color.RGBA{255, 255, 255, 255})

	rs := []rune(actx.s)
	for i := 0; i < len(rs); i++ {
		if x0, x1, ok := sp.RuneSpanX(i, i+1); ok {
			t.Logf("rune %d (%c U+%04X): px [%d,%d)", i, rs[i], rs[i], int(x0*ppu), int(x1*ppu))
		} else {
			t.Logf("rune %d (%c U+%04X): NO SPAN", i, rs[i], rs[i])
		}
	}

	dump := func(img *image.RGBA, label string, cellMarks bool) {
		var sb strings.Builder
		sb.WriteString("\n" + label + "\n")
		for y := 0; y < img.Rect.Dy(); y++ {
			for x := 0; x < img.Rect.Dx(); x++ {
				if cellMarks && x > 0 && x%boxW == 0 {
					sb.WriteString("|")
				}
				if img.RGBAAt(x, y).A > 32 {
					sb.WriteString("#")
				} else {
					sb.WriteString(".")
				}
			}
			sb.WriteString("\n")
		}
		t.Log(sb.String())
	}
	dump(raw, "WHOLE WINDOW (uncropped, with tatweels):", false)

	mask := term.cellTextImage(actx.s, "ui-term", false, false, boxW, boxH, ppu, false, 'ي', true, true, actx)
	dump(mask, "FINAL CELL MASK (exactly one cell wide):", true)
}
