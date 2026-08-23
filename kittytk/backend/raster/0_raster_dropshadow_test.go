package raster

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// lum reads a pixel's red channel — enough to compare shading on a
// grey/white ground.
func lum(b *Backend, x, y int) int {
	o := b.img.PixOffset(x, y)
	return int(b.img.Pix[o])
}

func whiteBackend(t *testing.T, w, h int) *Backend {
	t.Helper()
	b, err := New(w, h)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b.Clear(style.CellStyle{Bg: style.RGB(255, 255, 255)})
	return b
}

// A drop shadow darkens where the caster is and around it, fades with
// distance, and stops entirely past the blur — the falloff is what makes
// it read as a shadow rather than an outline.
func TestDrawDropShadowPxFalloff(t *testing.T) {
	b := whiteBackend(t, 160, 120)

	const (
		x, y, w, h = 40, 30, 60, 40
		radius     = 4.0
		blur       = 8.0
		alpha      = 0.35
	)
	b.DrawDropShadowPx(x, y, w, h, radius, blur, alpha)

	center := lum(b, x+w/2, y+h/2)
	if center >= 255 {
		t.Errorf("center of the cast rect = %d, want darkened (the caster paints over it)", center)
	}

	// Walking out from the right edge, the shadow only gets lighter.
	prev := -1
	for d := 0; d <= int(blur)+2; d++ {
		got := lum(b, x+w+d, y+h/2)
		if prev >= 0 && got < prev {
			t.Errorf("shadow at %d px past the edge = %d, darker than %d at %d px — falloff must be monotonic",
				d, got, prev, d-1)
		}
		prev = got
	}

	if just := lum(b, x+w+1, y+h/2); just >= 255 {
		t.Errorf("1 px past the edge = %d, want some shadow", just)
	}
	if far := lum(b, x+w+int(blur)+4, y+h/2); far != 255 {
		t.Errorf("well past the blur = %d, want 255 (untouched)", far)
	}
	if left := lum(b, x-int(blur)-4, y+h/2); left != 255 {
		t.Errorf("well left of the caster = %d, want 255 (untouched)", left)
	}
}

// The corners round: at the caster's corner the shadow is lighter than
// at the middle of an edge the same distance out, because the rounded
// distance field pulls away there.
func TestDrawDropShadowPxRoundsCorners(t *testing.T) {
	b := whiteBackend(t, 160, 120)

	const (
		x, y, w, h = 40, 30, 60, 40
		radius     = 10.0
		blur       = 8.0
	)
	b.DrawDropShadowPx(x, y, w, h, radius, blur, 0.5)

	edge := lum(b, x+w+2, y+h/2)   // straight out from the right edge
	corner := lum(b, x+w+2, y+h+2) // diagonally off the bottom-right
	if corner <= edge {
		t.Errorf("corner shading %d is at least as dark as edge shading %d; "+
			"a rounded caster's corner must fall off sooner", corner, edge)
	}
}

// Clipping applies: a shadow cast at the edge of a clip must not paint
// outside it. MDI children rely on this — a child's shadow may spill
// past the child, never past the pane.
func TestDrawDropShadowPxRespectsClip(t *testing.T) {
	b := whiteBackend(t, 160, 120)

	b.SetClip(core.UnitRect{X: 0, Y: 0, Width: 80, Height: 120})
	b.DrawDropShadowPx(40, 30, 60, 40, 4, 8, 0.5)

	if inside := lum(b, 70, 50); inside >= 255 {
		t.Errorf("inside the clip = %d, want shadow", inside)
	}
	if outside := lum(b, 85, 50); outside != 255 {
		t.Errorf("outside the clip = %d, want 255 — the shadow escaped its clip", outside)
	}
}

// Alpha climbs toward opaque as the shadow darkens, so a shadow stays
// visible over a surface that started transparent — a torn-off window's
// framebuffer is cleared to alpha 0, and a shadow that only scaled the
// existing alpha would be invisible there.
func TestDrawDropShadowPxBuildsAlphaOverTransparency(t *testing.T) {
	b, err := New(160, 120)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// A fresh backend is zero-filled: fully transparent.
	if a := b.img.Pix[b.img.PixOffset(70, 50)+3]; a != 0 {
		t.Fatalf("fresh backend alpha = %d, want 0", a)
	}

	b.DrawDropShadowPx(40, 30, 60, 40, 4, 8, 0.5)

	if a := b.img.Pix[b.img.PixOffset(70, 50)+3]; a == 0 {
		t.Error("shadow over a transparent surface left alpha at 0; it would not be visible")
	}
}

// A zero-alpha or empty style draws nothing at all rather than a
// full-strength black rectangle.
func TestDrawDropShadowPxDegenerateInputs(t *testing.T) {
	for _, tc := range []struct {
		name                string
		w, h                int
		radius, blur, alpha float64
	}{
		{"zero alpha", 60, 40, 4, 8, 0},
		{"zero width", 0, 40, 4, 8, 0.5},
		{"zero height", 60, 0, 4, 8, 0.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := whiteBackend(t, 160, 120)
			b.DrawDropShadowPx(40, 30, tc.w, tc.h, tc.radius, tc.blur, tc.alpha)
			if got := lum(b, 70, 50); got != 255 {
				t.Errorf("pixel = %d, want 255 (nothing drawn)", got)
			}
		})
	}
}

// A blur of zero is a hard-edged shadow, not a divide by zero.
func TestDrawDropShadowPxZeroBlurIsHardEdged(t *testing.T) {
	b := whiteBackend(t, 160, 120)
	b.DrawDropShadowPx(40, 30, 60, 40, 0, 0, 0.5)

	if inside := lum(b, 70, 50); inside >= 255 {
		t.Errorf("inside the rect = %d, want shaded", inside)
	}
	if outside := lum(b, 102, 50); outside != 255 {
		t.Errorf("2 px outside the rect = %d, want 255 with no blur", outside)
	}
}

// The backend advertises the capability the painter looks for; without
// this the painter silently draws nothing.
func TestBackendImplementsDropShadowDrawer(t *testing.T) {
	b := whiteBackend(t, 16, 16)
	var _ core.DropShadowDrawer = b
}
