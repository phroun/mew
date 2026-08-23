package tui

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// realisticPage is web-page-ish content: a flat background with text-like runs.
// Real screen content compresses; random noise would measure nothing useful.
func realisticPage(w, h int) *image.RGBA {
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{13, 17, 23, 255}
			if y%22 < 10 && x%9 < 5 && x > 60 && x < w-200 {
				c = color.RGBA{230, 237, 243, 255}
			}
			im.SetRGBA(x, y, c)
		}
	}
	return im
}

// A full-window frame must not cost megabytes. Raw RGBA base64'd measured
// 4.83 MB, which crosses a pty and is decoded by the terminal before anything
// appears — for one frame, of a picture that changes whenever the page does.
//
// The bound here is deliberately loose. It is not asserting a compression
// ratio, which depends on the content; it is asserting that the payload is not
// in the megabytes, which is the difference between usable and not.
func TestFramePayloadIsNotMegabytes(t *testing.T) {
	img := realisticPage(1090, 830)
	var sb strings.Builder
	writeKittyImage(&sb, img, 1, 0)
	t.Logf("one full-window frame on the wire: %.2f MB", float64(sb.Len())/1e6)
	if sb.Len() > 1<<20 {
		t.Errorf("a single frame is %.2f MB on the wire", float64(sb.Len())/1e6)
	}
}

func BenchmarkWriteKittyFrame(b *testing.B) {
	img := realisticPage(1090, 830)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var sb strings.Builder
		writeKittyImage(&sb, img, 1, 0)
	}
}

// What a delta actually saves on a realistic frame: a page whose video region
// repaints while the chrome around it holds still.
//
// The baseline is a full send of the NEW frame, not the old one — the question
// is what it costs to get from here to there, two ways. And the changing region
// is gradient-like rather than white noise: video compresses, and noise would
// measure the incompressibility of the test data rather than the mechanism.
func TestDeltaSavingOnAPartialRepaint(t *testing.T) {
	page := realisticPage(1090, 830)

	// A video region repainting: a third of the width, half the height.
	next := image.NewRGBA(page.Bounds())
	copy(next.Pix, page.Pix)
	for y := 200; y < 615; y++ {
		for x := 300; x < 663; x++ {
			next.SetRGBA(x, y, color.RGBA{byte(x / 3), byte(y / 3), byte((x + y) / 4), 255})
		}
	}

	var full strings.Builder
	writeKittyImage(&full, next, 1, 0)

	r, changed := changedCellRect(page, next, 10, 20)
	if !changed {
		t.Fatal("no change found")
	}
	var delta strings.Builder
	writeKittyImage(&delta, next.SubImage(r).(*image.RGBA), 2, 1)

	t.Logf("full send of the new frame %.3f MB; the changed region alone %.3f MB (%.1fx less)",
		float64(full.Len())/1e6, float64(delta.Len())/1e6,
		float64(full.Len())/float64(delta.Len()))
	if delta.Len() >= full.Len() {
		t.Errorf("the delta (%d bytes) is no smaller than the whole frame (%d)",
			delta.Len(), full.Len())
	}

	// And the case a delta is really for: a small change on a busy page — a
	// caret blinking, a hover highlight, a line of a terminal scrolling.
	small := image.NewRGBA(page.Bounds())
	copy(small.Pix, page.Pix)
	for y := 300; y < 320; y++ {
		for x := 400; x < 410; x++ {
			small.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	var fullSmall, deltaSmall strings.Builder
	writeKittyImage(&fullSmall, small, 3, 0)
	rs, _ := changedCellRect(page, small, 10, 20)
	writeKittyImage(&deltaSmall, small.SubImage(rs).(*image.RGBA), 4, 1)
	t.Logf("one cell changed: full send %.3f MB; delta %.6f MB (%.0fx less)",
		float64(fullSmall.Len())/1e6, float64(deltaSmall.Len())/1e6,
		float64(fullSmall.Len())/float64(deltaSmall.Len()))
}
