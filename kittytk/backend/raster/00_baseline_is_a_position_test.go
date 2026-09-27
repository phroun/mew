package raster

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// A line's BOX is ceiled and its BASELINE is floored, from the same measured
// ascent.
//
// Ceiling both is a downward bias chosen twice over: the box grows to hold
// the glyphs, and then the glyphs are pushed down inside it. Every run came
// out sitting low in its row with its descenders flush against the bottom
// edge -- and flush against an edge is where they got cut, which read as the
// box being too short when it was the text being too low.
//
// Read off the paint: how much clear space a run leaves above and below its
// ink, inside its own line box.
func TestBaselineIsFlooredWhileTheBoxIsCeiled(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	b, err := NewScaled(300, 200, 1)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)

	slack := func(f *core.Font, s string) (above, below int) {
		b.Clear(style.DefaultStyle().WithBg(style.RGB(255, 255, 255)))
		core.NewPainter(b).DrawText(0, 0, s,
			style.DefaultStyle().WithFg(style.RGB(0, 0, 0)).WithBg(style.ColorTransparent), f)
		img := b.Image()
		first, last := -1, -1
		for y := 0; y < 60; y++ {
			for x := 0; x < 250; x++ {
				if c := img.RGBAAt(x, y); int(c.R)+int(c.G)+int(c.B) < 400 {
					if first < 0 {
						first = y
					}
					last = y
					break
				}
			}
		}
		if first < 0 {
			t.Fatalf("no ink for %q at %dpt", s, f.Size)
		}
		return first, int(core.FontLineBudget(f)) - 1 - last
	}

	for _, size := range []int{9, 11, 12, 13, 14, 16, 18, 20} {
		f := &core.Font{Name: "ui-text", Size: size}

		// A descender must not end ON the box's last row: that is where it
		// gets cut, and it is the signature of a baseline pushed down into a
		// box that grew to meet it.
		if _, below := slack(f, "Undo Ep"); below < 1 {
			t.Errorf("%dpt: a descender ends %d rows short of the line box's bottom, want at least 1",
				size, below)
		}

		// And a run with no descender is not bottom-heavy: whatever slack the
		// face leaves, no more of it is above the caps than below the
		// baseline. Ceiling the baseline put a whole extra unit above.
		above, below := slack(f, "Undo")
		if above > below+1 {
			t.Errorf("%dpt: %d rows clear above the caps against %d below, which is a downward bias",
				size, above, below)
		}
	}
}
