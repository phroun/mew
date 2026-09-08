package render

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

// A renderer writing to the terminal itself turns each right-to-left run back
// for a host that reorders, so the host's own pass lands it the right way
// round. A renderer whose output has been VIRTUALIZED writes into another cell
// renderer, and that one turns the runs back from what it knows of the terminal
// -- so this one must not, or every run is turned back twice and lands
// reversed.
func TestAHostedRendererLeavesTheTurnToItsHost(t *testing.T) {
	t.Cleanup(core.ForgetHostBidi)

	const visual = "םולש" // as the grid holds it, leftmost cell first
	const logical = "שלום"

	// glyphs are the characters a frame carries, in the order they went out.
	glyphs := func(frame string) string {
		var b strings.Builder
		for _, r := range frame {
			if r >= 0x0590 && r <= 0x05FF {
				b.WriteRune(r)
			}
		}
		return b.String()
	}

	frame := func(hosted bool) string {
		var out strings.Builder
		sr := NewScreenRenderer(nil, nil)
		if hosted {
			sr.SetTerminal(&out, func() (int, int, error) { return 20, 1, nil }, false)
		}
		sr.SetFlipBidiForHost(true)
		b := sr.frame
		b.begin()
		for i, r := range []rune(visual) {
			b.cur[0][i] = bbCell{runes: []rune{r}, width: 1}
		}
		var sb strings.Builder
		b.present(&sb)
		return sb.String()
	}

	if got := glyphs(frame(false)); got != logical {
		t.Errorf("a renderer that owns the terminal sent %q, want the run turned "+
			"back to %q", got, logical)
	}
	if got := glyphs(frame(true)); got != visual {
		t.Errorf("a hosted renderer sent %q, want the run as laid out (%q) -- its "+
			"host turns it back", got, visual)
	}
}

// flipBidiForHost says what the TERMINAL does, and that is one answer for the
// whole program however many renderers the bytes pass through. So a hosted
// renderer still passes it on: it is the host that acts on it, and the option
// would be dead exactly where it has the most to say if it did not.
func TestAHostedRendererStillSaysWhatTheTerminalDoes(t *testing.T) {
	t.Cleanup(core.ForgetHostBidi)

	var out strings.Builder
	sr := NewScreenRenderer(nil, nil)
	sr.SetTerminal(&out, func() (int, int, error) { return 20, 1, nil }, false)

	for _, want := range []bool{true, false, true} {
		sr.SetFlipBidiForHost(want)
		if applies, _ := core.HostAppliesBidi(); applies != want {
			t.Errorf("flipBidiForHost=%v reached the host as %v", want, applies)
		}
		// And it still turns nothing back itself, whichever way it is set.
		if sr.frame.flipBidi {
			t.Errorf("flipBidiForHost=%v had a hosted renderer turn its own runs "+
				"back as well as its host", want)
		}
	}
}

// Setting it takes effect on the screen rather than only in a field: hosted,
// this renderer's own bytes do not change, so the frame it feeds its host has
// to be laid down again for the host to turn the runs the new way.
func TestChangingItRepaintsEvenWhenHosted(t *testing.T) {
	t.Cleanup(core.ForgetHostBidi)

	var out strings.Builder
	sr := NewScreenRenderer(nil, nil)
	sr.SetTerminal(&out, func() (int, int, error) { return 20, 1, nil }, false)
	sr.SetFlipBidiForHost(false)

	// The same row, painted again and again. Once it is on the display, an
	// unchanged frame emits none of it.
	paint := func() string {
		sr.frame.begin()
		for i, r := range []rune("םולש") {
			sr.frame.cur[0][i] = bbCell{runes: []rune{r}, width: 1}
		}
		var sb strings.Builder
		sr.frame.present(&sb)
		return sb.String()
	}
	paint()
	if got := paint(); strings.Contains(got, "םולש") {
		t.Fatalf("an unchanged frame emitted the row again, so this test cannot "+
			"tell a repaint from a diff: %q", got)
	}

	// Changing the option changes nothing about the cells, and the row still
	// has to go out again -- the host turns it the other way now.
	sr.SetFlipBidiForHost(true)
	if got := paint(); !strings.Contains(got, "םולש") {
		t.Errorf("the option changed and the row was not laid down again: %q", got)
	}
}
