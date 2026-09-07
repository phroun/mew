package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// edgeFillRecorder keeps where and how wide every device-pixel fill was, and in
// what colours, so a test can find the end-of-run arrows among them.
type edgeFillRecorder struct {
	*raster.Backend
	fills []edgeFill
}

type edgeFill struct {
	x, w int
	st   style.CellStyle
}

func (b *edgeFillRecorder) FillRectPx(x, y, w, h int, st style.CellStyle) {
	b.fills = append(b.fills, edgeFill{x: x, w: w, st: st})
	b.Backend.FillRectPx(x, y, w, h, st)
}

// The arrows saying the run carries on sit on the field's own extreme edges --
// the left one starting at its first pixel, the right one ending at its last --
// so they read as the boundary of the field rather than as more text.
//
// They wear the active input method's colours inverted, which is the one pair
// nothing else in a field uses, and they show whether or not it is focused: the
// fact that text is out of sight belongs to the text, not to who is editing it.
func TestAnArrowSitsOnTheFieldsEdge(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	base, err := raster.New(600, 60)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(base)

	const cell = core.Unit(8)
	long := strings.Repeat("abcdefghij", 6)

	for _, focused := range []bool{true, false} {
		base.Clear(style.DefaultStyle())
		rec := &edgeFillRecorder{Backend: base}

		ti := NewTextInput()
		ti.SetText(long)
		ti.SetBounds(core.UnitRect{Width: 20 * cell, Height: 16})
		if focused {
			ti.SetFocus()
		}
		// Somewhere in the middle, so the run carries on BOTH ways.
		ti.SetCursorPosition(30)
		ti.ensureCursorVisible()
		if !ti.moreLeft || !ti.moreRight {
			t.Fatalf("focused=%v: a caret in the middle of a long run hides text both "+
				"ways; left=%v right=%v", focused, ti.moreLeft, ti.moreRight)
		}

		p := core.NewPainter(rec)
		ti.Paint(p)

		scheme := ti.GetScheme()
		active := scheme.GetFocusedEditBoxIMEActiveClause()
		want := active.WithFg(active.Bg).WithBg(active.Fg)
		right := p.UnitSpanPxX(0, ti.Bounds().Width)

		var atLeft, atRight bool
		for _, f := range rec.fills {
			if f.st.Bg != want.Bg || f.w <= 0 {
				continue
			}
			if f.x == 0 {
				atLeft = true
			}
			if f.x+f.w == right {
				atRight = true
			}
		}
		if !atLeft {
			t.Errorf("focused=%v: no arrow starting at the field's first pixel", focused)
		}
		if !atRight {
			t.Errorf("focused=%v: no arrow ending at the field's last pixel (%d)", focused, right)
		}
	}
}
