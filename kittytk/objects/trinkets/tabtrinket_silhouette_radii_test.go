package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The selected tab's curve must keep its shape when the row
// denomination stretches the tab (the demo's grid toggle takes rows from
// 16 to 32 units). The horizontal radii stay tied to the cell width, the
// vertical radii scale with the row height, and the shoulder and foot
// always span the whole row so no straight edge is stranded between them.
func TestTabSilhouetteRadiiScaleWithRow(t *testing.T) {
	// Base 8x16: the two axes coincide (a circle), matching the historical
	// cw-derived radii (rSmall=6, rBig=10).
	sx, bx, sy, by := tabSilhouetteRadii(8, 16)
	if sx != 6 || bx != 10 || sy != 6 || by != 10 {
		t.Fatalf("8x16 radii = (%d,%d,%d,%d), want (6,10,6,10)", sx, bx, sy, by)
	}

	// Stretch the row to 32 units at the same 8-wide cell: horizontal
	// radii are unchanged (the foot still fits the slash cell) while the
	// vertical radii double, so the corners stretch with the tab.
	sx2, bx2, sy2, by2 := tabSilhouetteRadii(8, 32)
	if sx2 != sx || bx2 != bx {
		t.Errorf("horizontal radii changed with the row: (%d,%d) vs (%d,%d)", sx2, bx2, sx, bx)
	}
	if sy2 != 2*sy || by2 != 2*by {
		t.Errorf("vertical radii = (%d,%d), want double the 16-unit (%d,%d)", sy2, by2, 2*sy, 2*by)
	}

	// The shoulder and foot always fill the row exactly (no gap, no
	// overlap) at every denomination.
	for _, rowH := range []core.Unit{16, 24, 32, 40} {
		_, _, rs, rb := tabSilhouetteRadii(8, rowH)
		if rs+rb != rowH {
			t.Errorf("rowH=%d: rSmallY+rBigY=%d, want %d", rowH, rs+rb, rowH)
		}
	}
}
