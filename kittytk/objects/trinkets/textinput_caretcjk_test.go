//go:build sdl

package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// caretRecorder keeps the fills a paint made, so a test can find the caret
// bar among them.
type caretRecorder struct {
	*raster.Backend
	fills []preeditFill
}

func (b *caretRecorder) FillRectPx(x, y, w, h int, st style.CellStyle) {
	b.fills = append(b.fills, preeditFill{xPx: x, yPx: y, wPx: w, hPx: h, bg: st.Bg})
	b.Backend.FillRectPx(x, y, w, h, st)
}

// caretPx paints a focused field holding text with its caret at pos, and
// returns the caret bar's device-pixel x: a tall, thin fill.
func caretPx(t *testing.T, text string, pos int) int {
	t.Helper()
	b, err := raster.NewScaled(800, 40, 2)
	if err != nil {
		t.Fatal(err)
	}
	b.SetFontSize(10)
	core.SetTextMeasurer(b)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	ti := NewTextInput()
	ti.SetText(text)
	ti.SetBounds(core.UnitRect{Width: 600, Height: b.LineHeight(ti.EffectiveFont())})
	ti.SetFocus()
	ti.SetCursorPosition(pos)
	ti.caretOn = true

	rec := &caretRecorder{Backend: b}
	b.Clear(style.DefaultStyle())
	p := core.NewPainter(rec)
	ti.Paint(p)

	lineH := p.UnitsToPx(ti.EffectiveFont().LineHeight())
	x := -1
	for _, f := range rec.fills {
		if f.hPx >= lineH/2 && f.wPx > 0 && f.wPx <= p.DeviceScale()*2 {
			x = f.xPx
		}
	}
	if x < 0 {
		t.Fatalf("no caret bar painted for %q at %d: %+v", text, pos, rec.fills)
	}
	return x
}

// Equal characters must advance the caret equally.
//
// They cannot advance it by exactly the same number of PIXELS - a space beside
// CJK text is about four and a half of them, and a caret lands on whole ones -
// but two of them can never differ by more than one. Measuring the prefix in
// whole units and scaling to pixels afterwards rounds twice, and the second
// space advanced the caret by three pixels where the first had moved it five:
// far enough to read as the caret sitting before the space rather than after
// it.
func TestEqualCharactersAdvanceTheCaretEqually(t *testing.T) {
	for _, text := range []string{"日", "日本語", "ab", "a日"} {
		n := len([]rune(text))
		at := []int{
			caretPx(t, text, n),
			caretPx(t, text+" ", n+1),
			caretPx(t, text+"  ", n+2),
			caretPx(t, text+"   ", n+3),
		}
		var steps []int
		for i := 1; i < len(at); i++ {
			steps = append(steps, at[i]-at[i-1])
		}
		for i, step := range steps {
			if step <= 0 {
				t.Errorf("%q: space %d did not advance the caret (%v)", text, i+1, at)
			}
		}
		lo, hi := steps[0], steps[0]
		for _, step := range steps {
			if step < lo {
				lo = step
			}
			if step > hi {
				hi = step
			}
		}
		if hi-lo > 1 {
			t.Errorf("%q: spaces advanced the caret by %v - equal characters "+
				"cannot differ by more than the one pixel a whole-pixel caret "+
				"costs (positions %v)", text, steps, at)
		}
	}
}
