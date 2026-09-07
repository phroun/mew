package tui

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// A layout asks how wide a run is; this backend then advances a cell at a time
// drawing it. Those two have to be the same number. Where they are not, every
// column after the disagreement is in the wrong place -- a caption centred
// against a width that never appears, a label cut a cell early, a run
// overflowing what was reserved for it -- and nothing in the layout can tell.
//
// So the rule is one rule: the backend installs the one it emits by, and
// core.CellWidth hands it to whoever measures.
func TestAMeasuredRunIsTheRunThatIsDrawn(t *testing.T) {
	cases := []struct{ name, text string }{
		{"ascii", "hello"},
		{"cjk", "日本語"},
		{"hebrew", "שלום"},

		// Points and harakat paint into the cell before them and advance
		// nothing, so a measure that gave them a cell each ran long.
		{"hebrew niqqud", "שָׁלוֹם"},
		{"arabic harakat", "مَرْحَبًا"},
		{"latin combining", "café"},

		// Wide glyphs outside the common ideograph blocks: a measure that
		// knew only those blocks called each of these one cell and ran short,
		// which is the direction that overflows.
		{"cjk radical", "⺀⺁"},
		{"fullwidth latin", "ＡＢＣ"},
		{"katakana", "カタカナ"},
		{"circled ideograph", "㊀㊁"},

		// Runes a terminal gives no cell to that carry no combining class:
		// the Thai vowels that hang off their base, and a Hangul jamo filler.
		{"thai", "กาำ"},
		{"jamo filler", "ᄀᅠᅡ"},

		{"mixed", "a שלום 日 b"},
	}

	for _, c := range cases {
		b, _ := newTestTUI(120, 2)
		b.BeginFrame()
		drawn := b.DrawText(0, 0, c.text, style.DefaultStyle(), nil)
		b.EndFrame()

		if measured := core.DefaultFont().MeasureText(c.text); measured != drawn {
			t.Errorf("%s: measured %d units, drew %d", c.name, measured, drawn)
		}
	}
}
