package render

import (
	"testing"

	"github.com/phroun/mew/internal/textwidth"
)

// An anchored (isolated) Hebrew point that has no good rendering on a dotted
// circle is shown on a faux base under rtlMarkMode iterm2/compose: the shin dot
// and sin dot on a shin glyph (U+FB2A/FB2B), the holam haser for vav on a vav
// glyph (U+FB4B) — so the isolated and well-formed forms look the same, with no
// inconsistent circle. Other anchored marks fall through unchanged.
func TestPrecomposeBareAnchoredPoint(t *testing.T) {
	circle := textwidth.MarkAnchor
	cases := []struct {
		name string
		mark rune
		want string // "" => not precomposed (falls through)
	}{
		{"bare shin dot", 0x05C1, string(rune(0xFB2A))},
		{"bare sin dot", 0x05C2, string(rune(0xFB2B))},
		{"bare holam haser", 0x05BA, string(rune(0xFB4B))},
		{"bare sheva (a vowel) is left on its circle", 0x05B0, ""},
	}
	for _, c := range cases {
		cell := bbCell{runes: []rune{circle, c.mark}, width: 1}
		got, ok := precomposeCell(cell)
		if c.want == "" {
			if ok {
				t.Errorf("%s: precomposed to %q, want untouched", c.name, got)
			}
			continue
		}
		if !ok || got != c.want {
			t.Errorf("%s: got (%q,%v), want (%q,true)", c.name, got, ok, c.want)
		}
	}
}

// Both iterm2 and compose fold points; normal mode and the flex host do not.
func TestEmitCellTextModes(t *testing.T) {
	betDagesh := bbCell{runes: []rune{0x05D1, 0x05BC}, width: 1}
	bareShinDot := bbCell{runes: []rune{textwidth.MarkAnchor, 0x05C1}, width: 1}
	composedBet := string(rune(0xFB31))
	fauxShin := string(rune(0xFB2A))
	bareBet := string([]rune{0x05D1, 0x05BC})
	bareCircleDot := string([]rune{textwidth.MarkAnchor, 0x05C1})

	for _, mode := range []string{"iterm2", "compose"} {
		b := &backBuffer{rtlMarkMode: mode}
		if got := b.emitCellText(betDagesh); got != composedBet {
			t.Errorf("%s: bet+dagesh -> %q, want %q", mode, got, composedBet)
		}
		if got := b.emitCellText(bareShinDot); got != fauxShin {
			t.Errorf("%s: bare shin dot -> %q, want %q", mode, got, fauxShin)
		}
	}
	bNorm := &backBuffer{rtlMarkMode: "normal"}
	if got := bNorm.emitCellText(betDagesh); got != bareBet {
		t.Errorf("normal: -> %q, want the bare cluster", got)
	}
	if got := bNorm.emitCellText(bareShinDot); got != bareCircleDot {
		t.Errorf("normal: bare shin dot -> %q, want the circle cluster", got)
	}
	bFlex := &backBuffer{rtlMarkMode: "iterm2", logicalCUP: true}
	if got := bFlex.emitCellText(betDagesh); got != bareBet {
		t.Errorf("flex host: -> %q, want the bare cluster", got)
	}
}
