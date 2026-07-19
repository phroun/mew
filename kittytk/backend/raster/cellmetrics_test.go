package raster

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// The root denomination stays 8x16 regardless of font_size: font_size
// scales the cell's PIXEL size (pixels-per-unit), not its subdivision
// count.
func TestFontSizeLeavesDenominationAt8x16(t *testing.T) {
	b, err := New(200, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{6, 12, 18, 24} {
		b.SetFontSize(size)
		if m := b.Metrics(); m.CellWidth != 8 || m.CellHeight != 16 {
			t.Fatalf("%dpt denomination = %dx%d, want 8x16", size, m.CellWidth, m.CellHeight)
		}
	}
}

// SetCellMetrics re-seeds Metrics() (a non-default root denomination) and
// clamps degenerate values to 1. font_size never routes through here.
func TestSetCellMetrics(t *testing.T) {
	b, err := New(100, 100)
	if err != nil {
		t.Fatal(err)
	}
	if m := b.Metrics(); m.CellWidth != 8 || m.CellHeight != 16 {
		t.Fatalf("default Metrics = %+v, want 8x16", m)
	}
	b.SetCellMetrics(core.CellMetrics{CellWidth: 10, CellHeight: 20})
	if m := b.Metrics(); m.CellWidth != 10 || m.CellHeight != 20 {
		t.Fatalf("after set Metrics = %+v, want 10x20", m)
	}
	b.SetCellMetrics(core.CellMetrics{CellWidth: 0, CellHeight: 0})
	if m := b.Metrics(); m.CellWidth != 1 || m.CellHeight != 1 {
		t.Fatalf("degenerate metrics = %+v, want clamped to 1x1", m)
	}
}

// The cell primitive (DrawCell) fills one cell whose PIXEL size scales
// with font_size: at 12pt the default cell is 8x16 px, at 24pt it is
// 16x32 px (double). The denomination is unchanged; only pixels-per-unit
// grows, so chrome glyphs grow with the host font_size.
func TestDrawCellFillScalesWithFontSize(t *testing.T) {
	// A distinctive opaque background so the filled cell is measurable.
	bg := style.DefaultStyle().WithBg(style.RGB(10, 20, 200))
	measure := func(size int) (w, h int) {
		b, err := New(200, 200)
		if err != nil {
			t.Fatal(err)
		}
		b.SetFontSize(size)
		b.Clear(style.DefaultStyle())
		b.DrawCell(0, 0, ' ', bg) // space: background fill only, no glyph
		img := b.Image()
		for x := 0; x < 200; x++ {
			if c := img.RGBAAt(x, 0); c.B > 150 && c.R < 100 {
				w = x + 1
			}
		}
		for y := 0; y < 200; y++ {
			if c := img.RGBAAt(0, y); c.B > 150 && c.R < 100 {
				h = y + 1
			}
		}
		return w, h
	}

	if w, h := measure(12); w != 8 || h != 16 {
		t.Errorf("12pt cell fill = %dx%d px, want 8x16", w, h)
	}
	if w, h := measure(24); w != 16 || h != 32 {
		t.Errorf("24pt cell fill = %dx%d px, want 16x32 (double)", w, h)
	}
}

// Whole cells land on exact device-pixel multiples of the cell size, so
// cell-aligned geometry stays crisp at any font_size (the sub-cell
// remainder interpolates).
func TestCellSnapping(t *testing.T) {
	b, err := New(400, 400)
	if err != nil {
		t.Fatal(err)
	}
	b.SetFontSize(18) // cell = 12x24 px (8*18/12 x 16*18/12)
	if got := b.pxX(8 * 4); got != 12*4 {
		t.Errorf("4 cells across (32 units) = %d px, want %d", got, 12*4)
	}
	if got := b.pxY(16 * 3); got != 24*3 {
		t.Errorf("3 cells down (48 units) = %d px, want %d", got, 24*3)
	}
	// A sub-cell offset interpolates: half a cell (4 units of 8) is ~6 px.
	if got := b.pxX(4); got != 6 {
		t.Errorf("half a cell (4 units) = %d px, want 6", got)
	}
}
