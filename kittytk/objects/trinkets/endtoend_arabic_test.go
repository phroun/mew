package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/purfecterm"
)

// The definitive reproduction: run the REAL production pipeline — raster
// backend, real painter, the trinket's actual Paint() — on السلام عليكم in
// visual order, and dump the actual framebuffer. No reconstructed geometry.
func TestEndToEndArabicPaint(t *testing.T) {
	endToEndArabicPaint(t, 1, 12)
}

// Retina + common font sizes: the mac app runs scale 2 and a configured
// font_size; the join must survive every geometry.
func TestEndToEndArabicPaintRetina(t *testing.T)   { endToEndArabicPaint(t, 2, 12) }
func TestEndToEndArabicPaintRetina14(t *testing.T) { endToEndArabicPaint(t, 2, 14) }
func TestEndToEndArabicPaintRetina16(t *testing.T) { endToEndArabicPaint(t, 2, 16) }

func endToEndArabicPaint(t *testing.T, scale, fontSize int) {
	b, err := raster.NewScaled(800, 120, scale)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	if tm, ok := interface{}(b).(core.TextMeasurer); ok {
		core.SetTextMeasurer(tm)
		defer core.SetTextMeasurer(nil)
	}
	b.SetFontSize(fontSize)

	term := NewPurfecTerm()
	term.SetTerminalFontFamily("ui-term")
	term.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 200, Height: 30})

	p := core.NewPainter(b)
	if !p.Graphical() {
		t.Skip("painter not graphical")
	}
	term.Paint(p) // size the grid
	// mew emits RTL in VISUAL order AND pre-shaped to PRESENTATION forms
	// (internal/bidi/shape.go) — feed exactly what mew feeds: each visual
	// cell's contextual form.
	visual := []rune("مكيلع مالسلا")
	out := make([]rune, 0, len(visual))
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
		out = append(out, shaped)
	}
	term.Feed([]byte(string(out)))
	term.Paint(p)

	img := b.Image()
	bounds := img.Bounds()
	var sb strings.Builder
	sb.WriteString("\n")
	longestRun := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		row := ""
		any := false
		run := 0
		for x := bounds.Min.X; x < bounds.Max.X && x < 200; x++ {
			c := img.RGBAAt(x, y)
			if int(c.R)+int(c.G)+int(c.B) > 96 {
				row += "#"
				any = true
				run++
				if run > longestRun {
					longestRun = run
				}
			} else {
				row += "."
				run = 0
			}
		}
		if any {
			sb.WriteString(row + "\n")
		}
	}
	t.Log(sb.String())

	// The join assertion, on the REAL framebuffer: a joined Arabic word has a
	// continuous baseline, so the longest horizontal run of lit pixels spans
	// many cells; isolated letters (the bug) cap the longest run near a single
	// letter's width (< one cell). Threshold is cell-relative so it holds at any
	// scale: a joined 5-letter word clears 3 cells easily; isolated forms cannot.
	cwU, _ := term.cellDims()
	cellPx := int(float64(cwU) * p.PxPerUnitF())
	if cellPx < 1 {
		cellPx = 1
	}
	if longestRun < 3*cellPx {
		t.Errorf("longest baseline run = %d px (cell=%d px) — Arabic is not joining across cells; isolated forms cap the run below one cell",
			longestRun, cellPx)
	}
}

// Log the REAL production geometry numbers.
func TestEndToEndGeometryNumbers(t *testing.T) {
	b, err := raster.New(400, 60)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	if tm, ok := interface{}(b).(core.TextMeasurer); ok {
		core.SetTextMeasurer(tm)
		defer core.SetTextMeasurer(nil)
	}
	term := NewPurfecTerm()
	term.SetTerminalFontFamily("ui-term")
	term.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 200, Height: 30})
	p := core.NewPainter(b)
	term.Paint(p)
	cw, ch := term.cellDims()
	ppu := p.PxPerUnitF()
	t.Logf("cellDims: cw=%v ch=%v units; ppu=%v; boxW=%dpx boxH=%dpx; pt=%d",
		cw, ch, ppu, int(float64(cw)*ppu), int(float64(ch)*ppu), int(float64(ch))*3/4)
}
