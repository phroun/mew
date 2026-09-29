package trinkets

// The separator between two tabs belongs to both: a press on it selects the
// one it fell nearer. Beside the selected tab the separator is three cells,
// and its middle cell -- the slash between the two -- goes to whichever of
// them is already selected, staying on the earlier one when neither is. A
// two-cell separator splits down the middle.

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

func TestAPressOnASeparatorPicksTheNearerTab(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	cases := []struct {
		after int   // the tab whose separator is pressed
		want  []int // the tab selected by a press on each of its cells
	}{
		{1, []int{1, 2, 2}}, // Bravo, before the selected C: its slash goes to C
		{2, []int{2, 2, 3}}, // C, the selected tab: its slash stays with C
		{4, []int{4, 5}},    // E, beside neither: split down the middle
	}
	for _, c := range cases {
		probe := longStrip(t, TabsTop, core.DirLTR, 60, 4)
		if probe.CurrentIndex() != 2 {
			t.Fatalf("setup: the current tab is %d, want 2", probe.CurrentIndex())
		}
		var sp stripSpan
		for _, s := range probe.stripSpans {
			if s.owner == c.after {
				sp = s
			}
		}
		cw := probe.EffectiveCellMetrics().UnitsPerCellWidth
		if cells := int((sp.x + sp.w - sp.labelEnd) / cw); cells != len(c.want) {
			t.Fatalf("the separator after tab %d is %d cells, want %d", c.after, cells, len(c.want))
		}
		for cell, want := range c.want {
			tt := longStrip(t, TabsTop, core.DirLTR, 60, 4)
			x := sp.labelEnd + core.Unit(cell)*cw + cw/2
			tt.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: x, Y: 1})
			if got := tt.CurrentIndex(); got != want {
				t.Errorf("a press on cell %d of the separator after tab %d selects %d, want %d", cell, c.after, got, want)
			}
		}
	}
}
