package raster_test

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// paintScene approximates a busy desktop: a full-surface clear plus n scattered
// proportional text runs (the dominant per-frame cost).
func paintScene(p *core.Painter, n int) {
	bg := style.DefaultStyle().WithBg(style.Color(256 + 0x202020))
	p.Clear(core.UnitRect{Width: 800, Height: 600}, bg)
	fg := style.DefaultStyle().WithFg(style.Color(256 + 0xE0E0E0))
	for i := 0; i < n; i++ {
		x := core.Unit((i * 53) % 780)
		y := core.Unit((i * 31) % 580)
		p.DrawText(x, y, "Hello, world 123", fg, nil)
	}
}

// A full-surface repaint of the busy scene (the old every-frame cost).
func BenchmarkSceneFull(b *testing.B) {
	be, _ := raster.New(800, 600)
	p := core.NewPainter(be)
	paintScene(p, 400) // warm the text cache
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		paintScene(p, 400)
	}
}

// The same scene clipped to a caret-sized region: the dirty-rect + off-clip
// reject path. This is what a blinking caret now costs per frame.
func BenchmarkScenePartialCaret(b *testing.B) {
	be, _ := raster.New(800, 600)
	p := core.NewPainter(be)
	paintScene(p, 400) // warm cache (full)
	pp := core.NewPainter(be).WithClip(core.UnitRect{X: 100, Y: 100, Width: 120, Height: 20})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		paintScene(pp, 400)
	}
}

// Cache-hit text draw: shows the struct key is allocation-free (-benchmem).
func BenchmarkTextDrawCacheHit(b *testing.B) {
	be, _ := raster.New(240, 40)
	p := core.NewPainter(be)
	fg := style.DefaultStyle().WithFg(style.Color(256 + 0xE0E0E0))
	p.DrawText(0, 0, "warm cache line", fg, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.DrawText(0, 0, "warm cache line", fg, nil)
	}
}

// Alpha rectangle fill (edge fades / modal dim) - the integer blend path.
func BenchmarkFillRectAlpha(b *testing.B) {
	be, _ := raster.New(800, 600)
	p := core.NewPainter(be)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.FillRectPixelsAlpha(0, 0, 0, 0, 800, 16, 20, 20, 20, 0.5)
	}
}
