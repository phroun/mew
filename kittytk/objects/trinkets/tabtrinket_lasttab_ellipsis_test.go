package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// recordingBackend wraps a RenderBackend and remembers every DrawText string,
// so a test can assert which tab labels were drawn in full versus truncated.
type recordingBackend struct {
	core.RenderBackend
	texts []string
}

func (r *recordingBackend) DrawText(x, y core.Unit, text string, s style.CellStyle, font *core.Font) core.Unit {
	r.texts = append(r.texts, text)
	return r.RenderBackend.DrawText(x, y, text, s, font)
}

func (r *recordingBackend) drew(s string) bool {
	for _, t := range r.texts {
		if t == s {
			return true
		}
	}
	return false
}

// When the actual last tab is the last visible one and it fits (per
// isLastTabFullyVisible), the draw path must render its complete label. A
// prior bug reserved trailing "more tabs" overflow-ellipsis room after every
// selected/next-to-selected tab - including the last one, which has no tab
// after it - so the final selected tab was truncated ("Bottom Ta...") even
// though it fit. This sweeps widths and asserts that wherever the strip
// scrolls and reports the last tab fully visible, the whole label is drawn.
func TestLastTabDrawnInFullWhenItFits(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(700, 96)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(px)

	const lastLabel = "Bottom Tabs"
	build := func() *TabTrinket {
		tw := NewTabTrinket()
		for _, name := range []string{"Basics", "Buttons", "Inputs", "Lists", "Scrolling", "Progress", lastLabel} {
			tw.AddTab(name, NewLabel(name))
		}
		tw.SetCurrentIndex(len(tw.tabs) - 1) // select the last tab
		return tw
	}

	checked := 0
	for w := core.Unit(200); w <= 460; w += 4 {
		tw := build()
		tw.SetBounds(core.UnitRect{Width: w, Height: 96})
		tw.ensureCurrentTabVisible()
		if !tw.tabsNeedScrolling() || !tw.isLastTabFullyVisible() {
			continue
		}
		checked++
		rec := &recordingBackend{RenderBackend: px}
		tw.Paint(core.NewPainter(rec))
		if !rec.drew(lastLabel) {
			t.Errorf("width=%d: last tab reported fully visible but its label %q was not drawn in full (drawn: %v)", w, lastLabel, rec.texts)
		}
	}
	if checked == 0 {
		t.Fatal("test setup: no swept width both scrolled and fully showed the last tab")
	}
}
