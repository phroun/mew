package text

import "math"

// RuneSpanX reports the horizontal extent, in units, that the glyphs of rune
// range [start, end) occupy within the shaped paragraph's first line — the
// window a caller needs to cut one cluster's pixels out of a shaped run (e.g.
// the per-cell Arabic renderer, which shapes prev+tatweel+letter+tatweel+next
// as one joined run and then crops to the piece it wants). Cluster indices come
// from the shaper, so ligatures (lam-alef) and marks attribute to the cluster's
// lowest rune. ok is false if no glyph maps into the range.
func (p *ShapedParagraph) RuneSpanX(start, end int) (x0, x1 float64, ok bool) {
	minX := math.Inf(1)
	maxX := math.Inf(-1)
	for li := range p.Lines {
		line := &p.Lines[li]
		for ri := range line.Runs {
			run := &line.Runs[ri]
			pen := float64(run.X)
			for _, g := range run.raw.Glyphs {
				adv := float64(g.Advance) / 64
				if g.ClusterIndex >= start && g.ClusterIndex < end {
					if pen < minX {
						minX = pen
					}
					if pen+adv > maxX {
						maxX = pen + adv
					}
				}
				pen += adv
			}
		}
	}
	if minX > maxX {
		return 0, 0, false
	}
	return minX, maxX, true
}
