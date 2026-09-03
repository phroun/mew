package raster

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Runs painted one after another leave no seam between their backgrounds.
//
// A run's image is its background as well as its ink, and the pixel width of
// that image is the advance the next run is placed from. Rounded, a run whose
// width fell a fraction short left the surface showing in the seam. The width
// is an EXTENT, so it ceils: the next run then starts at or before this one's
// right edge, and their backgrounds meet whatever the two rates do.
//
// The ink is free to fall where it falls -- letters may sit a fraction from
// where a unit measurement would put them. What must not gap is the
// background.
func TestRunBackgroundsMeetWithNoSeam(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	// A surface whose pixels-per-unit is FRACTIONAL, which is where a rounded
	// width can fall short of the run it holds. At the default 12pt a unit is
	// a whole pixel and the conversion is exact, so nothing can gap there
	// however it rounds.
	for _, hostSize := range []int{11, 13, 14, 17, 19, 23} {
		b, err := NewScaled(400, 60, 1)
		if err != nil {
			t.Fatal(err)
		}
		b.SetFontSize(hostSize)
		core.SetTextMeasurer(b)
		size := hostSize
		f := &core.Font{Name: "ui-text", Size: size}

		ground := style.RGB(255, 0, 255)
		b.Clear(style.DefaultStyle().WithBg(ground))
		p := core.NewPainter(b)
		run := style.DefaultStyle().WithBg(style.RGB(0, 0, 128)).WithFg(style.RGB(255, 255, 255))

		// Segment after segment, each placed at the UNIT advance the last one
		// measured -- which is how a title with an accelerator, or a prefix
		// and an ellipsis, is assembled where the target has no pixel text
		// path. It is also the placement that gapped: the next run lands on
		// the snapped pixel of its unit, and snapping can put that past where
		// the last image ended.
		x := core.Unit(0)
		for _, seg := range []string{"Hel", "p", "...", "Menu"} {
			p.DrawText(x, 0, seg, run, f)
			x += f.MeasureTextIn(seg, core.DefaultCellMetrics())
		}
		xPx := b.pxLen(x)

		gr, gg, gb := ground.RGBComponents()
		img := b.Image()
		for x := 0; x < xPx; x++ {
			seam := true
			for y := 0; y < b.pxLen(core.FontLineBudget(f)); y++ {
				if c := img.RGBAAt(x, y); c.R != gr || c.G != gg || c.B != gb {
					seam = false
					break
				}
			}
			if seam {
				t.Errorf("%dpt: the ground shows through a full-height seam at x=%d of %d",
					size, x, xPx)
				break
			}
		}
	}
}
