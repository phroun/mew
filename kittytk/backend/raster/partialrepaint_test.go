package raster_test

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// The SDL partial-repaint relies on three properties of the persistent
// framebuffer: a clipped Clear fills only the damaged region, draws outside the
// clip are blocked, and everything outside stays byte-identical. Confirm them.
func TestPartialRepaintLeavesOutsideUntouched(t *testing.T) {
	b, err := raster.New(200, 100)
	if err != nil {
		t.Fatal(err)
	}
	bg := style.DefaultStyle().WithBg(style.Color(256 + 0x102030))
	red := style.DefaultStyle().WithBg(style.Color(256 + 0xFF0000))
	green := style.DefaultStyle().WithBg(style.Color(256 + 0x00FF00))

	// Full frame: background everywhere, a red marker on the right.
	p := core.NewPainter(b)
	p.Clear(core.UnitRect{Width: 200, Height: 100}, bg)
	p.FillRect(core.UnitRect{X: 150, Y: 10, Width: 30, Height: 30}, ' ', red)
	before := b.Image().RGBAAt(160, 20) // inside the red marker

	// Partial repaint clipped to the left region.
	dmg := core.UnitRect{X: 0, Y: 0, Width: 60, Height: 100}
	pp := core.NewPainter(b).WithClip(dmg)
	pp.Clear(core.UnitRect{Width: 200, Height: 100}, green) // clipped -> only the left region
	// Attempt to overpaint the right marker through the clip - must be blocked.
	pp.FillRect(core.UnitRect{X: 150, Y: 10, Width: 30, Height: 30}, ' ', bg)

	// Inside the damage: repainted green.
	if c := b.Image().RGBAAt(30, 50); c.R != 0 || c.G != 255 || c.B != 0 {
		t.Errorf("inside damage not repainted (got %v, want green)", c)
	}
	// Outside the damage: untouched (the red marker survives byte-for-byte).
	if c := b.Image().RGBAAt(160, 20); c != before {
		t.Errorf("outside damage changed: was %v, now %v", before, c)
	}
}
