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
	writeKittyImage(&sb, img, 1)
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
		writeKittyImage(&sb, img, 1)
	}
}
