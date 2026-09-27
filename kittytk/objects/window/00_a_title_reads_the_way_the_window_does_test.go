package window

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// titleInk records what a cell target is handed to draw.
type titleInk struct {
	core.RenderBackend
	texts []string
}

func (i *titleInk) DrawText(x, y core.Unit, text string, s style.CellStyle, f *core.Font) core.Unit {
	i.texts = append(i.texts, text)
	return i.RenderBackend.DrawText(x, y, text, s, f)
}

// A window's title is a caption like any other, and the bar knows which way it
// reads: the metrics a title is drawn from carry the direction, so a title
// arrives at a cell target in the order it will be stamped -- and what the bar
// centres is the run it draws, not the text behind it.
func TestATitleReadsTheWayTheWindowDoes(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)

	const shalom = "שלום"
	turned := string([]rune{'ם', 'ו', 'ל', 'ש'})

	drawn := func(dir core.Direction, focused bool) []string {
		px, err := raster.New(900, 200)
		if err != nil {
			t.Fatal(err)
		}
		ink := &titleInk{RenderBackend: px}
		p := core.NewPainter(ink)
		tm := TitleBarMetricsFor(core.DefaultCellMetrics(), core.DefaultFont(), false)
		tm.Dir = dir
		if focused {
			PaintFocusedTitleDecoration(p, tm, 40*8, shalom, style.DefaultStyle())
		} else {
			PaintTitleBarText(p, tm, shalom, style.DefaultStyle(), 0, 40*8, 40*8)
		}
		return ink.texts
	}

	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		got := strings.Join(drawn(dir, false), "")
		if !strings.Contains(got, turned) {
			t.Errorf("%v: the bar drew %q, want the turned-over title %q", dir, got, turned)
		}
		// The focused title wears its brackets, and the whole thing is one run:
		// the title inside them still reads the way the window does.
		got = strings.Join(drawn(dir, true), "")
		if !strings.Contains(got, turned) {
			t.Errorf("%v focused: the bar drew %q, want the turned-over title %q", dir, got, turned)
		}
	}

	// A bar that names no direction draws what it was given.
	got := strings.Join(drawn(core.DirInherit, false), "")
	if !strings.Contains(got, shalom) {
		t.Errorf("with no direction the bar drew %q, want the title untouched", got)
	}
}
