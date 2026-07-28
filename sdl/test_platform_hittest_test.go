//go:build sdl

package sdl

import "testing"

// snapForward mirrors the raster backend's cell-snapped unit->pixel
// mapping on one axis (denomination cells land on exact cellPx multiples,
// the sub-cell remainder rounds). toUnits must invert it so a click lands
// on the same grid the UI paints on.
func snapForward(u, denom, cellPx int) int {
	cells := u / denom
	rem := u % denom
	return cells*cellPx + (rem*cellPx+denom/2)/denom
}

// toUnits inverts the backend's font_size-aware pixel mapping: mapping a
// unit forward to pixels and back returns the same unit at every
// font_size and device zoom (exactly on cell boundaries, within a unit
// off the snap elsewhere).
func TestToUnitsInvertsBackendMapping(t *testing.T) {
	for _, fs := range []int{6, 12, 18, 24} {
		for _, scale := range []int{1, 2} {
			p := New("t", 100, 100)
			p.SetScale(scale)
			p.SetFontSize(fs)
			denomW, denomH := p.rootDenomination()
			cwPx, chPx := p.cellPx(denomW), p.cellPx(denomH)

			// Whole cells must round-trip exactly (crisp hit grid).
			for cells := 0; cells < 6; cells++ {
				ux := cells * denomW
				pxx := snapForward(ux, denomW, cwPx)
				if gx, _ := p.toUnits(int32(pxx), 0); int(gx) != ux {
					t.Errorf("fs=%d scale=%d: x cell %d (%d px) -> %d units, want %d", fs, scale, cells, pxx, gx, ux)
				}
				uy := cells * denomH
				pxy := snapForward(uy, denomH, chPx)
				if _, gy := p.toUnits(0, int32(pxy)); int(gy) != uy {
					t.Errorf("fs=%d scale=%d: y cell %d (%d px) -> %d units, want %d", fs, scale, cells, pxy, gy, uy)
				}
			}
		}
	}
}

// At the 12pt base with device zoom 1, hit-testing is the historical
// one-unit-per-pixel identity.
func TestToUnitsIdentityAtBase(t *testing.T) {
	p := New("t", 100, 100)
	p.SetScale(1)
	p.SetFontSize(12)
	for _, v := range []int32{0, 1, 7, 8, 16, 100} {
		if x, y := p.toUnits(v, v); int(x) != int(v) || int(y) != int(v) {
			t.Errorf("toUnits(%d,%d) = (%d,%d), want identity", v, v, x, y)
		}
	}
}
