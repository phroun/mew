package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
)

// newShadowPane builds a pane with a near-white background: the shading
// has to have somewhere to go, and the default desktop fill is already
// at the dark end.
func newShadowPane(bounds core.UnitRect) *MDIPane {
	m := NewMDIPane()
	m.SetDrawPattern(false)
	white := style.RGB(250, 250, 250)
	m.SetBackgroundColor(&white)
	m.SetBounds(bounds)
	return m
}

// paintPane paints the pane onto a white backend and returns a reader
// for the red channel of any pixel (enough to compare shading).
func paintPane(t *testing.T, m *MDIPane, w, h int) func(x, y int) int {
	t.Helper()
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := raster.New(w, h)
	if err != nil {
		t.Fatalf("raster.New: %v", err)
	}
	b.BeginFrame()
	b.Clear(style.CellStyle{Bg: style.RGB(255, 255, 255)})
	m.Paint(core.NewPainter(b))
	b.EndFrame()

	img := b.Image()
	return func(x, y int) int { return int(img.Pix[img.PixOffset(x, y)]) }
}

// An MDI child casts a drop shadow. It paints into its parent window's
// surface — inside the compositor layer that window occupies — so it has
// no layer of its own for the compositor to shadow, and the shadow has
// to be painted right here, under the child.
func TestMDIChildCastsDropShadow(t *testing.T) {
	m := newShadowPane(core.UnitRect{Width: 400, Height: 300})

	child := window.NewWindow("Child")
	child.SetBounds(core.UnitRect{X: 100, Y: 80, Width: 160, Height: 120})
	m.AddWindow(child)

	at := paintPane(t, m, 400, 300)

	// Just past the child's right edge (260), mid-height: the strongest
	// part of the falloff, cast 2 units right.
	near := at(263, 140)
	open := at(370, 140)
	if near >= open {
		t.Errorf("pixel just off the child = %d, open background = %d; "+
			"want the near one darker (no shadow is being cast)", near, open)
	}

	// And it fades: further out is lighter, and past the blur it stops.
	mid := at(268, 140)
	if mid <= near || mid >= open {
		t.Errorf("falloff broken: edge %d, 5px out %d, open %d — want edge < 5px out < open",
			near, mid, open)
	}
	if far := at(285, 140); far != open {
		t.Errorf("well past the blur = %d, want the untouched background %d", far, open)
	}
}

// The shadow stays inside the pane. A child parked against the pane's
// right edge would otherwise smear its shadow across whatever the pane
// sits next to in the parent window.
func TestMDIChildShadowStaysInsidePane(t *testing.T) {
	m := newShadowPane(core.UnitRect{Width: 200, Height: 300})

	child := window.NewWindow("Edge")
	child.SetBounds(core.UnitRect{X: 100, Y: 80, Width: 95, Height: 120})
	m.AddWindow(child)

	at := paintPane(t, m, 400, 300)

	// Everything right of the pane must be exactly as the clear left it.
	for _, x := range []int{205, 240, 300} {
		if got := at(x, 140); got != 255 {
			t.Errorf("pixel (%d,140) = %d, want 255 — a child's shadow escaped the pane", x, got)
		}
	}
}

// Shadows interleave with the windows in z-order: each is painted just
// before its own window, so a shadow lands on everything below it and is
// covered by everything above. Painting them all up front would leave a
// lower window's shadow smeared across the window on top of it.
func TestMDIChildShadowsPaintInZOrder(t *testing.T) {
	m := newShadowPane(core.UnitRect{Width: 400, Height: 300})

	lower := window.NewWindow("Lower")
	lower.SetBounds(core.UnitRect{X: 40, Y: 40, Width: 160, Height: 120})
	m.AddWindow(lower)

	// The upper window covers the bottom-right of where the lower one's
	// shadow falls, and leaves the top-right of it exposed.
	upper := window.NewWindow("Upper")
	upper.SetBounds(core.UnitRect{X: 150, Y: 130, Width: 180, Height: 130})
	m.AddWindow(upper)
	m.ActivateWindow(upper)

	at := paintPane(t, m, 400, 300)

	// First prove the lower window's shadow really does reach this far
	// right — otherwise the check below passes for the wrong reason.
	if spill, open := at(205, 100), at(370, 100); spill >= open {
		t.Fatalf("lower window's shadow at (205,100) = %d vs open background %d; "+
			"it does not reach the region this test is about", spill, open)
	}

	// Same x, but now inside the upper window: it must look exactly like
	// the rest of the upper window's interior.
	covered := at(160, 150)
	clean := at(300, 150)
	if covered != clean {
		t.Errorf("upper window at (160,150) = %d but %d elsewhere on the same row; "+
			"the lower window's shadow is painting over the window above it", covered, clean)
	}
}
