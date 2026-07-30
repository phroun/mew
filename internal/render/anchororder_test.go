package render

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/textwidth"
)

// Under rtlMarkMode "iterm2" an isolated shin dot or sin dot anchored on a
// dotted circle is emitted with a leading ZERO WIDTH SPACE on a plain terminal:
// ZWSP · point · circle. U+25CC is East Asian ambiguous; a terminal that draws
// it at the wide advance hangs the point a full cell to the right of the circle
// (iTerm2), and a letter abutting the circle could otherwise capture the mark.
// The ZWSP is a zero-advance base the point rides instead of the wide circle,
// pinning it at the cell's origin and breaking the join that would let an
// abutting letter steal it. The cell mew stores is unchanged — only the wire
// bytes change — so the flex-width host, which composes the cluster itself, sees
// no ZWSP.
func TestShinSinDotAnchorLeadsWithZWSPOnPlainTerminal(t *testing.T) {
	const circle = string(textwidth.MarkAnchor) // ◌
	const zwsp = string(zeroWidthSpace)
	cases := []struct {
		name string
		mark rune
	}{
		{"shin dot", 0x05C1},
		{"sin dot", 0x05C2},
	}
	for _, c := range cases {
		anchored := circle + string(c.mark)

		// Plain terminal, rtlMarkMode=iterm2: ZWSP, then point, then circle.
		b := newBackBuffer(10, 2)
		b.rtlMarkMode = "iterm2"
		out := plain(paint(b, mv(1, 1), wr("\x1b[33m"+anchored)))
		wantPlain := zwsp + string(c.mark) + circle
		if !strings.Contains(out, wantPlain) {
			t.Errorf("%s iterm2: wire %q, want ZWSP·point·circle (%q)", c.name, out, wantPlain)
		}

		// Flex-width host (2027), even under iterm2 mode: no ZWSP, natural order.
		bf := newBackBuffer(10, 2)
		bf.rtlMarkMode = "iterm2"
		bf.logicalCUP = true
		outf := plain(paint(bf, mv(1, 1), wr("\x1b[33m"+anchored)))
		if !strings.Contains(outf, anchored) {
			t.Errorf("%s flex: wire %q, want the natural circle-then-point (%q)", c.name, outf, anchored)
		}
		if strings.Contains(outf, zwsp) {
			t.Errorf("%s flex: wire should carry no ZWSP, got %q", c.name, outf)
		}
	}
}

// rtlMarkMode "normal" (the default) leaves the emission alone: the bare
// circle-then-point cluster on every surface, no ZWSP, even on a plain terminal.
func TestShinDotAnchorNormalModeIsBareCluster(t *testing.T) {
	const circle = string(textwidth.MarkAnchor)
	const zwsp = string(zeroWidthSpace)
	anchored := circle + string(rune(0x05C1))

	for _, mode := range []string{"", "normal"} {
		b := newBackBuffer(10, 2)
		b.rtlMarkMode = mode
		out := plain(paint(b, mv(1, 1), wr("\x1b[33m"+anchored)))
		if !strings.Contains(out, anchored) {
			t.Errorf("mode %q: wire %q, want the bare circle-then-point (%q)", mode, out, anchored)
		}
		if strings.Contains(out, zwsp) {
			t.Errorf("mode %q: wire should carry no ZWSP, got %q", mode, out)
		}
	}
}

// The reorder is scoped to the two corner-sitting dots. A vowel point that sits
// below its base (hiriq) shows no visible horizontal misplacement, so its
// anchored cluster is emitted in the natural order even under iterm2 mode.
func TestBelowBaseAnchorNotReversed(t *testing.T) {
	const circle = string(textwidth.MarkAnchor)
	const hiriq = 0x05B4
	anchored := circle + string(rune(hiriq))

	b := newBackBuffer(10, 2)
	b.rtlMarkMode = "iterm2"
	out := plain(paint(b, mv(1, 1), wr("\x1b[33m"+anchored)))
	if !strings.Contains(out, anchored) {
		t.Errorf("hiriq anchor: wire %q, want the natural circle-then-point (%q)", out, anchored)
	}
}

// The stored cell is identical either way — the reorder is a wire-only concern,
// so the diff still recognises an unchanged anchored cell and re-emits nothing
// on a quiet frame.
func TestReversedAnchorCellIsStableAcrossFrames(t *testing.T) {
	const circle = string(textwidth.MarkAnchor)
	anchored := circle + string(rune(0x05C1))

	b := newBackBuffer(10, 2)
	b.rtlMarkMode = "iterm2"
	paint(b, mv(1, 1), wr("\x1b[33m"+anchored)) // first frame
	out := plain(paint(b, mv(1, 1), wr("\x1b[33m"+anchored)))
	if strings.ContainsRune(out, textwidth.MarkAnchor) || strings.ContainsRune(out, 0x05C1) {
		t.Errorf("an unchanged anchored cell should re-emit no glyphs, got %q", out)
	}
}
