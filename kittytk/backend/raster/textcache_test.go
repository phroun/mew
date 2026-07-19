package raster_test

import (
	"fmt"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Cached and freshly rendered text must be pixel-identical, and the
// cached path must honor clips.
func TestDrawTextCacheConsistency(t *testing.T) {
	b, err := raster.New(400, 64)
	if err != nil {
		t.Fatal(err)
	}
	st := style.DefaultStyle().WithFg(style.Color(256 + 0xFFFFFF)).WithBg(style.Color(256 + 0x000080))

	b.Clear(style.DefaultStyle())
	w1 := b.DrawText(8, 8, "cache me", st, core.FontUIText12) // miss: renders
	first := *b.Image()
	firstPix := make([]uint8, len(first.Pix))
	copy(firstPix, first.Pix)

	b.Clear(style.DefaultStyle())
	w2 := b.DrawText(8, 8, "cache me", st, core.FontUIText12) // hit: blits
	if w1 != w2 {
		t.Errorf("advance changed on cache hit: %d then %d", w1, w2)
	}
	for i := range firstPix {
		if firstPix[i] != b.Image().Pix[i] {
			t.Fatal("cached blit differs from fresh render")
		}
	}

	// A clip must still confine the cached blit.
	b.Clear(style.DefaultStyle())
	b.SetClip(core.UnitRect{X: 0, Y: 0, Width: 1, Height: 1})
	b.DrawText(8, 8, "cache me", st, core.FontUIText12)
	if c := b.Image().RGBAAt(60, 12); c.B > 200 && c.R < 100 {
		t.Error("cached blit ignored the clip")
	}
}

// The regression the cache exists to fix: repainting the same strings
// every frame must not re-shape and re-rasterize them.
func BenchmarkDrawTextRepeatedFrame(bm *testing.B) {
	b, _ := raster.New(1280, 800)
	st := style.DefaultStyle()
	lines := make([]string, 24)
	for i := range lines {
		lines[i] = fmt.Sprintf("Window content line %d: the quick brown fox", i)
	}
	bm.ResetTimer()
	for n := 0; n < bm.N; n++ {
		for i, s := range lines {
			b.DrawText(8, core.Unit(16*i), s, st, core.FontUIText12)
		}
	}
}

// The uncached cost, for comparison: every string unique, so every
// draw is a full shape + rasterize.
func BenchmarkDrawTextUniqueStrings(bm *testing.B) {
	b, _ := raster.New(1280, 800)
	st := style.DefaultStyle()
	bm.ResetTimer()
	for n := 0; n < bm.N; n++ {
		for i := 0; i < 24; i++ {
			s := fmt.Sprintf("Unique %d/%d: the quick brown fox", n, i)
			b.DrawText(8, core.Unit(16*i), s, st, core.FontUIText12)
		}
	}
}
