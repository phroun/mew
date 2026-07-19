package trinkets

import (
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// renderSelectedTabStrip renders a top tab strip with the first tab selected
// at the given root denomination and font size, returning the framebuffer.
// denomW/denomH set the cell subdivision; fontSize + scale together fix the
// physical cell size (cellPx = ceil(denom*fontSize/12)*scale).
func renderSelectedTabStrip(t *testing.T, denomW, denomH, fontSize, scale int) *image.RGBA {
	t.Helper()
	b, err := raster.NewScaled(1200, 200, scale)
	if err != nil {
		t.Fatal(err)
	}
	b.SetCellMetrics(core.CellMetrics{CellWidth: core.Unit(denomW), CellHeight: core.Unit(denomH)})
	b.SetFontSize(fontSize)

	d := NewDesktop()
	d.SetBackend(b)

	tabs := NewTabTrinket()
	tabs.SetParent(d)
	tabs.AddTab("One", nil)
	tabs.AddTab("Two", nil)
	tabs.SetCurrentIndex(0)
	// Bounds in units: one row tall, wide enough that the two short tabs
	// leave plain bar area on the right to probe the content-edge line.
	m := b.Metrics()
	tabs.SetBounds(core.UnitRect{X: m.CellWidth, Y: m.CellHeight, Width: m.CellWidth * 16, Height: m.CellHeight})

	b.Clear(style.DefaultStyle())
	tabs.Paint(core.NewPainter(b))
	return b.Image()
}

// edgeLineThicknessPx returns the longest contiguous run of tab-line (near-
// white) pixels in the given column - the content-edge hairline weight in
// device pixels. In a plain bar column the only white is that horizontal line,
// so its run length is its thickness. The line is the bar foreground over the
// bar bg, reading much brighter (high on all channels) than the blue bar (low
// red) or the dark desktop.
func edgeLineThicknessPx(img *image.RGBA, col int) int {
	bright := func(y int) bool {
		r, g, bl, _ := img.At(col, y).RGBA()
		return (r>>8) > 150 && (g>>8) > 150 && (bl>>8) > 150
	}
	best, run := 0, 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		if bright(y) {
			run++
			if run > best {
				best = run
			}
		} else {
			run = 0
		}
	}
	return best
}

// The tab outline is one physical hairline; its thickness must NOT change
// when the root denomination changes (re-denomination pairs a unit-count
// change with a compensating font_size to keep the physical size, so a
// pxPerUnit-derived weight would silently thin/thicken the line). Two
// denominations rendered at the same physical cell size must produce the
// same edge-line thickness in device pixels.
func TestTabOutlineThicknessDenominationInvariant(t *testing.T) {
	const scale = 4
	// Both configs make a physical cell of ceil(denom*fontSize/12)*scale =
	// 16*scale px: (8x16 @ 12pt) and (16x32 @ 6pt).
	imgA := renderSelectedTabStrip(t, 8, 16, 12, scale)
	imgB := renderSelectedTabStrip(t, 16, 32, 6, scale)

	// Probe a column in the plain bar area on the right (past the two tabs,
	// before the strip ends near x=544 = origin 32 + 128 units * 4 px).
	col := 500

	tA := edgeLineThicknessPx(imgA, col)
	tB := edgeLineThicknessPx(imgB, col)
	t.Logf("edge thickness: denom16=%dpx denom32=%dpx (scale=%d)", tA, tB, scale)

	if dir := os.Getenv("KITTYTK_PROOF_DIR"); dir != "" {
		writePNG(t, imgA, filepath.Join(dir, "tab_edge_denom16.png"))
		writePNG(t, imgB, filepath.Join(dir, "tab_edge_denom32.png"))
	}

	if tA == 0 || tB == 0 {
		t.Fatalf("probe column %d missed the edge line (denom16=%d denom32=%d); adjust col", col, tA, tB)
	}
	if tA != tB {
		t.Errorf("edge-line thickness changed with denomination: denom16=%dpx denom32=%dpx", tA, tB)
	}

	// Optional visual dump.
	if dir := os.Getenv("KITTYTK_PROOF_DIR"); dir != "" {
		writePNG(t, imgA, filepath.Join(dir, "tab_edge_denom16.png"))
		writePNG(t, imgB, filepath.Join(dir, "tab_edge_denom32.png"))
	}
}

func writePNG(t *testing.T, img *image.RGBA, path string) {
	t.Helper()
	b, err := raster.NewScaled(img.Bounds().Dx(), img.Bounds().Dy(), 1)
	if err != nil {
		t.Fatal(err)
	}
	copy(b.Image().Pix, img.Pix)
	if err := b.WritePNG(path); err != nil {
		t.Fatalf("WritePNG: %v", err)
	}
	t.Logf("wrote %s", path)
}
