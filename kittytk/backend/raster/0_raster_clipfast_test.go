package raster_test

import (
	"image/color"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// The rounded clip only carves four corners, so blits and composites that
// land clear of them take their unclipped fast paths. That shortcut must not
// leak anything past the arcs: text drawn INTO a corner is still cut, whether
// it goes through the opaque blit or the alpha composite, and whether or not
// the run is already in the glyph cache.
func TestRoundedClipStillCutsTextAtCorners(t *testing.T) {
	const w, h, rad = 200, 120, 24
	for _, tc := range []struct {
		name   string
		opaque bool
	}{
		{"opaque blit", true},
		{"alpha composite", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := raster.New(w, h)
			if err != nil {
				t.Fatal(err)
			}
			black := style.Color(256 + 0x000000)
			b.Clear(style.DefaultStyle().WithBg(black))

			st := style.DefaultStyle().WithFg(style.Color(256 + 0xFF0000))
			if tc.opaque {
				st = st.WithBg(style.Color(256 + 0xFF0000))
			} else {
				st = st.WithBg(style.ColorTransparent)
			}

			p := core.NewPainter(b).
				WithRoundedClipRegion(core.UnitRect{X: 0, Y: 0, Width: w, Height: h}, rad)
			// Twice: the second pass is served from the text cache, which is
			// the run that takes the fast path.
			for pass := 0; pass < 2; pass++ {
				p.DrawText(0, 0, "MMMMMMMMMM", st, nil)
				p.DrawText(0, core.Unit(h-12), "MMMMMMMMMM", st, nil)
			}

			// The extreme corner pixels are far outside a radius-24 arc.
			img := b.Image()
			for _, pt := range [][2]int{{0, 0}, {1, 1}, {0, h - 1}, {1, h - 2}} {
				if c := img.RGBAAt(pt[0], pt[1]); c.R != 0 {
					t.Errorf("(%d,%d) leaked past the rounded clip: %v", pt[0], pt[1], c)
				}
			}
		})
	}
}

// The fast path must be pixel-identical to the per-pixel path it replaces:
// paint the same scene inside the rounded region two ways - once where every
// run clears the corners (fast) and once forced through the per-pixel test by
// a redundant rectangular clip - and compare the framebuffers.
func TestRoundedClipFastPathMatchesPerPixel(t *testing.T) {
	const w, h, rad = 200, 120, 24
	region := core.UnitRect{X: 0, Y: 0, Width: w, Height: h}

	paint := func(rectClip bool) *[w * h]color.RGBA {
		b, err := raster.New(w, h)
		if err != nil {
			t.Fatal(err)
		}
		b.Clear(style.DefaultStyle().WithBg(style.Color(256 + 0x101010)))
		p := core.NewPainter(b).WithRoundedClipRegion(region, rad)
		if rectClip {
			// An all-covering rectangular clip changes no pixel but sets
			// hasClip, which forces every composite off its fast path.
			p = p.WithClip(region)
		}
		fill := style.DefaultStyle().WithBg(style.Color(256 + 0x204060))
		p.FillRect(core.UnitRect{X: 0, Y: 0, Width: w, Height: h}, ' ', fill)
		opaque := style.DefaultStyle().
			WithFg(style.Color(256 + 0xFFFFFF)).WithBg(style.Color(256 + 0x800000))
		alpha := style.DefaultStyle().
			WithFg(style.Color(256 + 0x00FF00)).WithBg(style.ColorTransparent)
		for i := 0; i < 8; i++ {
			y := core.Unit(i * 14)
			p.DrawText(core.Unit(i*3), y, "Frame edge Wg", opaque, nil)
			p.DrawText(core.Unit(w-60+i), y+6, "Wg edge", alpha, nil)
		}
		var out [w * h]color.RGBA
		img := b.Image()
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out[y*w+x] = img.RGBAAt(x, y)
			}
		}
		return &out
	}

	fast, slow := paint(false), paint(true)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if fast[y*w+x] != slow[y*w+x] {
				t.Fatalf("fast path differs at (%d,%d): %v vs %v",
					x, y, fast[y*w+x], slow[y*w+x])
			}
		}
	}
}
