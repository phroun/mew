package core

import "testing"

// hairlineBackend is a raster-like surface: it reports the pixel a unit lands
// on for the metrics it is given, which is all a hairline needs to ask.
type hairlineBackend struct {
	RenderBackend
	metrics CellMetrics
	cellPx  int
}

func (b *hairlineBackend) Metrics() CellMetrics { return b.metrics }
func (b *hairlineBackend) Size() UnitSize {
	return UnitSize{Width: 100 * b.metrics.UnitsPerCellWidth, Height: 50 * b.metrics.UnitsPerCellHeight}
}
func (b *hairlineBackend) SetClip(UnitRect) {}
func (b *hairlineBackend) PxPerUnit() float64 {
	return float64(b.cellPx) / float64(b.metrics.UnitsPerCellWidth)
}
func (b *hairlineBackend) UnitToPxX(u Unit) int {
	return int(float64(u) * float64(b.cellPx) / float64(b.metrics.UnitsPerCellWidth))
}
func (b *hairlineBackend) UnitToPxY(u Unit) int {
	return int(float64(u) * float64(b.cellPx*2) / float64(b.metrics.UnitsPerCellHeight))
}

// A hairline is on the glass at every denomination.
//
// One screen unit converted into local units lands on a whole count, and that
// count can span no device pixel at all: at column_units=12 a local unit is
// two-thirds of a pixel, so the 1 the conversion answered painted NOTHING and
// a separator's rule or a splitter's divider was simply absent. Clamping the
// unit count at 1 never reached it, because 1 was the answer.
func TestAHairlineAlwaysPaintsAPixel(t *testing.T) {
	outer := DefaultCellMetrics()
	for _, m := range []CellMetrics{
		{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16},
		{UnitsPerCellWidth: 16, UnitsPerCellHeight: 32},
		{UnitsPerCellWidth: 4, UnitsPerCellHeight: 8},
		{UnitsPerCellWidth: 12, UnitsPerCellHeight: 20},
		{UnitsPerCellWidth: 10, UnitsPerCellHeight: 18},
		{UnitsPerCellWidth: 3, UnitsPerCellHeight: 7},
	} {
		b := &hairlineBackend{metrics: m, cellPx: 8}
		p := NewPainter(b).WithDenomination(outer, m)

		w := p.HairlineWidth()
		if px := p.UnitSpanPxX(0, w); px < 1 {
			t.Errorf("at %dx%d a %d-unit hairline spans %d device pixels",
				m.UnitsPerCellWidth, m.UnitsPerCellHeight, w, px)
		}
		h := p.HairlineHeight()
		if px := p.UnitSpanPxY(0, h); px < 1 {
			t.Errorf("at %dx%d a %d-unit hairline spans %d device rows",
				m.UnitsPerCellWidth, m.UnitsPerCellHeight, h, px)
		}
		// Thinnest, not merely visible: one unit less must not do.
		if w > 1 {
			if px := p.UnitSpanPxX(0, w-1); px >= 1 {
				t.Errorf("at %dx%d the hairline is %d units where %d already paints",
					m.UnitsPerCellWidth, m.UnitsPerCellHeight, w, w-1)
			}
		}
	}
}
