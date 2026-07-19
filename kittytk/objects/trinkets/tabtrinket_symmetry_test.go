package trinkets

import (
	"image"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// The selected tab's silhouette must be left-right symmetric: the leading
// (foot+shoulder) corner is the mirror of the trailing one. A per-arc interior
// offset that only cancels at one sub-cell phase renders as a notch on one side
// but not the other, so at small/fractional font sizes the tab looks lopsided.
// Render the middle-selected tab and check that each scanline's dark-tab span
// is centered on the same column - i.e. left and right extents mirror.
func TestTabSilhouetteLeftRightSymmetric(t *testing.T) {
	for _, fs := range []int{6, 8, 10, 12, 14, 16, 20, 24, 32} {
		img := renderMidSelectedTab(t, fs)
		lefts, rights, midY := tabRowExtents(img)
		if len(lefts) < 4 {
			t.Fatalf("fs=%d: too few tab rows detected (%d)", fs, len(lefts))
		}
		// The strip's own center column, from the widest (bottom) row.
		center := float64(lefts[len(lefts)-1]+rights[len(rights)-1]) / 2
		worst := 0.0
		var worstY int
		for i := range lefts {
			// Distance from center on each side must match (mirror symmetry).
			dl := center - float64(lefts[i])
			dr := float64(rights[i]) - center
			if d := dl - dr; d < 0 {
				d = -d
			} else {
				_ = d
			}
			asym := dl - dr
			if asym < 0 {
				asym = -asym
			}
			if asym > worst {
				worst, worstY = asym, midY+i
			}
		}
		t.Logf("fs=%d: worst left/right asymmetry=%.1fpx at y=%d (center col %.1f)", fs, worst, worstY, center)
		// One device pixel of asymmetry is unavoidable antialiasing noise; a real
		// per-side offset shows as ~a stroke width (2px+) on one side only.
		if worst > 1.5 {
			t.Errorf("fs=%d: tab silhouette is lopsided by %.1fpx (want mirror-symmetric)", fs, worst)
		}

		// Each edge must widen monotonically from top to bottom (the tab flares
		// out). An inward STEP - the left edge jumping right, or the right edge
		// jumping left - is the shoulder/foot seam jog (strokes on opposite sides
		// of the tangent). A symmetric jog passes the mirror check above, so guard
		// it directly here. One px of AA noise is allowed.
		stepL, stepR, stepY := 0, 0, 0
		for i := 1; i < len(lefts); i++ {
			if in := lefts[i] - lefts[i-1]; in > stepL { // left moved inward (right)
				stepL, stepY = in, midY+i
			}
			if in := rights[i-1] - rights[i]; in > stepR { // right moved inward (left)
				stepR = in
			}
		}
		t.Logf("fs=%d: worst inward seam step: left=%dpx right=%dpx (y~%d)", fs, stepL, stepR, stepY)
		if stepL > 1 || stepR > 1 {
			t.Errorf("fs=%d: shoulder/foot seam jogs inward (left=%dpx right=%dpx); want a clean inflection", fs, stepL, stepR)
		}
	}
}

// renderMidSelectedTab renders 5 tabs with the middle one selected (its feet
// flare onto the dark bar, in isolation) at the given font size, scale 1.
func renderMidSelectedTab(t *testing.T, fs int) *image.RGBA {
	t.Helper()
	b, err := raster.NewScaled(700, 60, 1)
	if err != nil {
		t.Fatal(err)
	}
	b.SetFontSize(fs)
	d := NewDesktop()
	d.SetBackend(b)
	tabs := NewTabTrinket()
	tabs.SetParent(d)
	for i := 0; i < 5; i++ {
		tabs.AddTab("Tab", nil)
	}
	tabs.SetCurrentIndex(2)
	m := b.Metrics()
	// Make the strip (blue bar) fill the whole image width at every font size,
	// so the only dark region inside the strip is the selected tab - the dark
	// desktop lives strictly below the strip (full-width rows, filtered out).
	tabs.SetBounds(core.UnitRect{X: 0, Y: 0, Width: m.CellWidth * 200, Height: m.CellHeight})
	b.Clear(style.DefaultStyle())
	tabs.Paint(core.NewPainter(b))
	return b.Image()
}

// tabRowExtents returns, for each scanline that crosses the selected (dark) tab
// body, its leftmost and rightmost dark column - excluding the final bar-edge
// row that spans the whole strip. midY is the first such row's y.
func tabRowExtents(img *image.RGBA) (lefts, rights []int, midY int) {
	dark := func(x, y int) bool {
		r, g, bl, _ := img.At(x, y).RGBA()
		return r>>8 < 60 && bl>>8 < 90 && g>>8 < 90
	}
	// The selected (dark) tab is the only dark region inside the strip; the
	// desktop area below the strip is also dark but spans the whole width, so
	// scan the full width and drop any row whose dark span is wide (that's the
	// desktop, or the full-width bar-edge line - not the tab body).
	W := img.Bounds().Dx()
	first := -1
	for y := 0; y < img.Bounds().Dy(); y++ {
		l, r := -1, -1
		for x := 2; x < W-2; x++ {
			if dark(x, y) {
				if l < 0 {
					l = x
				}
				r = x
			}
		}
		if l < 0 || r-l > W/3 {
			continue
		}
		if first < 0 {
			first = y
		}
		lefts = append(lefts, l)
		rights = append(rights, r)
	}
	return lefts, rights, first
}
