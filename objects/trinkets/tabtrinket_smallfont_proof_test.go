package trinkets

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Renders the selected tab's corners at small font sizes (fractional pixels-
// per-unit, where snapping is worst) for manual inspection. The middle tab is
// selected so its feet flare onto the dark bar, visible in isolation. Set
// KITTYTK_PROOF_DIR to keep the PNGs.
func TestTabSmallFontLeadingCornerProof(t *testing.T) {
	dir := os.Getenv("KITTYTK_PROOF_DIR")
	if dir == "" {
		t.Skip("set KITTYTK_PROOF_DIR to dump the small-font proofs")
	}
	for _, fs := range []int{6, 8, 10, 12, 20} {
		const scale = 1
		b, err := raster.NewScaled(700, 60, scale)
		if err != nil {
			t.Fatal(err)
		}
		b.SetFontSize(fs)
		d := NewDesktop()
		d.SetBackend(b)
		tabs := NewTabTrinket()
		tabs.SetParent(d)
		for i := 0; i < 5; i++ {
			tabs.AddTab("Tab", nil)
		}
		tabs.SetCurrentIndex(2)
		m := b.Metrics()
		tabs.SetBounds(core.UnitRect{X: 0, Y: 0, Width: m.CellWidth * 40, Height: m.CellHeight})
		b.Clear(style.DefaultStyle())
		tabs.Paint(core.NewPainter(b))
		out := filepath.Join(dir, "smallfont_fs"+strconv.Itoa(fs)+".png")
		if err := b.WritePNG(out); err != nil {
			t.Fatalf("WritePNG: %v", err)
		}
		t.Logf("fs=%d ppu=%.3f -> %s", fs, b.PxPerUnit(), out)
	}
}
