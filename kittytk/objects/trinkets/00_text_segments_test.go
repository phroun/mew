package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// segmentInk paints something on a fresh surface at the given font size and
// reports the first and last device-pixel column it inked.
func segmentInk(t *testing.T, fontSize int, draw func(*core.Painter, *core.Font)) (first, last int) {
	t.Helper()
	b, err := raster.NewScaled(600, 60, 1)
	if err != nil {
		t.Fatal(err)
	}
	b.SetFontSize(fontSize)
	core.SetTextMeasurer(b)

	b.Clear(style.DefaultStyle().WithBg(style.RGB(255, 255, 255)))
	p := core.NewPainter(b)
	draw(p, &core.Font{Name: "ui-text", Size: fontSize})

	img := b.Image()
	first, last = -1, -1
	for x := 0; x < 600; x++ {
		for y := 0; y < 60; y++ {
			if c := img.RGBAAt(x, y); int(c.R)+int(c.G)+int(c.B) < 600 {
				if first < 0 {
					first = x
				}
				last = x
				break
			}
		}
	}
	if first < 0 {
		t.Fatalf("font size %d: nothing was inked", fontSize)
	}
	return first, last
}

// A run split into styled segments occupies the same pixels as the same run
// drawn whole -- at font sizes where a unit is NOT a whole device pixel, which
// is the only place the two ways of advancing can disagree.
//
// The segments advance by accumulating the DEVICE-PIXEL advance from one
// anchor. Re-snapping each segment's start through the unit grid instead
// rounds once per segment, and at a fractional pixels-per-unit those roundings
// pile up: by the end of a dozen segments the run is visibly wider or narrower
// than the same text drawn in one piece.
func TestDrawTextSegmentsMatchOneRun(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	const text = "Preferences and Settings"
	metrics := core.DefaultCellMetrics()
	ink := style.DefaultStyle().WithFg(style.RGB(0, 0, 0)).WithBg(style.ColorTransparent)

	// 12 is the base size, where a unit is exactly a pixel; the others put the
	// unit and pixel rates out of step in both directions.
	for _, fontSize := range []int{11, 12, 13, 17, 23} {
		wantFirst, wantLast := segmentInk(t, fontSize, func(p *core.Painter, f *core.Font) {
			p.DrawText(0, 0, text, ink, f)
		})
		gotFirst, gotLast := segmentInk(t, fontSize, func(p *core.Painter, f *core.Font) {
			drawTextSegments(p, 0, 0, f, metrics,
				textSegment{text[:1], ink}, textSegment{text[1:12], ink},
				textSegment{text[12:], ink})
		})

		if gotFirst != wantFirst {
			t.Errorf("font size %d: the segmented run starts at pixel %d, the whole run at %d",
				fontSize, gotFirst, wantFirst)
		}
		// Each segment is shaped on its own and its advance rounded to a
		// whole pixel, so the two can honestly differ by a pixel per
		// boundary between segments -- two of them here.
		if d := (gotLast - gotFirst) - (wantLast - wantFirst); d < -2 || d > 2 {
			t.Errorf("font size %d: the segmented run inks %d px wide against the whole run's %d",
				fontSize, gotLast-gotFirst+1, wantLast-wantFirst+1)
		}
	}
}

// Segments carry their own styles, and each is drawn where the one before it
// ended: the accelerator letter of "Preferences" lands on the "P" and nowhere
// else.
func TestDrawTextSegmentsPlaceEachStyle(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	const text = "Preferences"
	metrics := core.DefaultCellMetrics()
	plain := style.DefaultStyle().WithFg(style.RGB(0, 0, 0)).WithBg(style.ColorTransparent)
	marked := style.DefaultStyle().WithFg(style.RGB(255, 0, 0)).WithBg(style.ColorTransparent)

	for _, fontSize := range []int{11, 12, 17} {
		// Where the whole word's ink starts, and where its second letter does.
		wordFirst, _ := segmentInk(t, fontSize, func(p *core.Painter, f *core.Font) {
			p.DrawText(0, 0, text, plain, f)
		})
		tailFirst, _ := segmentInk(t, fontSize, func(p *core.Painter, f *core.Font) {
			var segs []textSegment
			segs = append(segs, textSegment{string(text[0]), plain})
			segs = append(segs, textSegment{text[1:], marked})
			drawTextSegments(p, 0, 0, f, metrics, segs...)
		})
		if tailFirst != wordFirst {
			t.Errorf("font size %d: the segmented word starts at %d, the plain one at %d",
				fontSize, tailFirst, wordFirst)
		}

		// The red run must begin after the "P" and before the word ends.
		b, err := raster.NewScaled(600, 60, 1)
		if err != nil {
			t.Fatal(err)
		}
		b.SetFontSize(fontSize)
		core.SetTextMeasurer(b)
		b.Clear(style.DefaultStyle().WithBg(style.RGB(255, 255, 255)))
		p := core.NewPainter(b)
		f := &core.Font{Name: "ui-text", Size: fontSize}
		drawTextSegments(p, 0, 0, f, metrics,
			textSegment{string(text[0]), plain}, textSegment{text[1:], marked})

		img := b.Image()
		redFirst := -1
		for x := 0; x < 600 && redFirst < 0; x++ {
			for y := 0; y < 60; y++ {
				if c := img.RGBAAt(x, y); c.R > 200 && c.G < 100 && c.B < 100 {
					redFirst = x
					break
				}
			}
		}
		if redFirst < 0 {
			t.Fatalf("font size %d: the second segment left no ink of its own", fontSize)
		}
		pWidth := p.UnitsToPx(f.MeasureTextIn(string(text[0]), metrics))
		if d := redFirst - (wordFirst + pWidth); d < -2 || d > 2 {
			t.Errorf("font size %d: the second segment inks from %d, %d px off the end of the first",
				fontSize, redFirst, d)
		}
	}
}
