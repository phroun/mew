package main

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// BenchmarkPaintDemoWindow renders the full demo window repeatedly. It
// guards the raster fill path against regressions (the clip is snapped to
// pixels once per op, not per pixel, and solid runs fill by row).
func BenchmarkPaintDemoWindow(bb *testing.B) {
	b, win := buildDemoWindow(&testing.T{}, 10) // fractional font_size
	p := core.NewPainter(b)
	bb.ResetTimer()
	for i := 0; i < bb.N; i++ {
		b.Clear(style.DefaultStyle())
		win.Paint(p)
	}
}
