package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// wideIMeasurer fakes a proportional font: 'W' is 3x the width of 'i'.
// MeasurePos/etc. are unused by elidedTitlePrefix.
type wideIMeasurer struct{}

func (wideIMeasurer) MeasureText(f *core.Font, text string) core.Unit {
	total := core.Unit(0)
	for _, ch := range text {
		if ch == 'W' {
			total += 12
		} else {
			total += 4
		}
	}
	return total
}

// The elided title's fit is measured in the SAME font it renders with.
// A monospace (cells × cell-width) calculation cuts a proportional title
// at the wrong glyph: too early for narrow letters, too late for wide
// ones. Regression for the elided menu title measuring mono.
func TestElidedTitlePrefixMeasuresProportionally(t *testing.T) {
	core.SetTextMeasurer(wideIMeasurer{})
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	font := &core.Font{Name: "ui-text", Size: 12}

	// "iiii" is 16 units proportionally — all four fit in a 16 budget.
	// Mono math (cell width 8) would fit only two.
	if got := elidedTitlePrefix(font, core.DefaultCellMetrics(), []rune("iiii"), 16); got != 4 {
		t.Errorf("narrow letters: prefix = %d, want 4", got)
	}

	// "WW" is 24 units — only one 'W' fits in a 16 budget, even though
	// mono math would fit two 8-unit cells.
	if got := elidedTitlePrefix(font, core.DefaultCellMetrics(), []rune("WW"), 16); got != 1 {
		t.Errorf("wide letters: prefix = %d, want 1", got)
	}

	// Zero and negative budgets fit nothing; a huge budget fits all.
	if got := elidedTitlePrefix(font, core.DefaultCellMetrics(), []rune("Wi"), 0); got != 0 {
		t.Errorf("zero budget: prefix = %d, want 0", got)
	}
	if got := elidedTitlePrefix(font, core.DefaultCellMetrics(), []rune("Window"), 1000); got != 6 {
		t.Errorf("ample budget: prefix = %d, want 6", got)
	}
}
