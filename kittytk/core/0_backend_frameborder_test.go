package core

import "testing"

// ScaledWindowFrameBorderPx applies border law (a): the configured
// border_width is the thickness at the BASE zoom (pixels-per-unit == 1),
// scaled by zoom so the frame keeps a constant fraction of the content
// (geometry-cells-units-pixels.md). It is physically uniform on every
// window because a single desktop ppu drives it, and never below 1px.
func TestScaledWindowFrameBorderPx(t *testing.T) {
	defer SetWindowFrameBorderPx(0)

	// Default border_width is 2 (the built-in), unset.
	SetWindowFrameBorderPx(0)
	cases := []struct {
		ppu  float64
		want int
	}{
		{1, 2},   // base zoom: unchanged (backward compatible)
		{2, 4},   // scale 2 / font 24: doubles
		{1.5, 3}, // fractional zoom rounds
		{3, 6},
	}
	for _, c := range cases {
		if got := ScaledWindowFrameBorderPx(c.ppu); got != c.want {
			t.Errorf("default border, ppu=%v: got %d px, want %d", c.ppu, got, c.want)
		}
	}

	// A configured base width scales the same way.
	SetWindowFrameBorderPx(20)
	if got := ScaledWindowFrameBorderPx(2); got != 40 {
		t.Errorf("border_width=20, ppu=2: got %d px, want 40", got)
	}

	// Never thinner than 1px, even when a tiny width zooms out below 1.
	SetWindowFrameBorderPx(1)
	if got := ScaledWindowFrameBorderPx(0.25); got != 1 {
		t.Errorf("border floor: got %d px, want 1", got)
	}
	// A non-positive ppu is treated as the base zoom, not a zero border.
	if got := ScaledWindowFrameBorderPx(0); got != 1 {
		t.Errorf("ppu=0 fallback: got %d px, want 1", got)
	}
}
