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

// And it does not tell the host what to do about the terminal. Its own flip is
// off because the host does the turning; saying so would tell the host to stop,
// which is the one thing that must not happen.
func TestAHostedRendererDoesNotAnswerForTheTerminal(t *testing.T) {
	t.Cleanup(core.ForgetHostBidi)

	// The host has recognised a reordering terminal for itself.
	core.SetSniffedHostBidi(true, false, true)

	var out strings.Builder
	sr := NewScreenRenderer(nil, nil)
	sr.SetTerminal(&out, func() (int, int, error) { return 20, 1, nil }, false)
	sr.SetFlipBidiForHost(true)
	sr.SetFlipWordwise(false)
	sr.SetFlipRideSafeSelection(true)

	if applies, _ := core.HostAppliesBidi(); !applies {
		t.Error("a hosted renderer overruled what its host knows about the terminal")
	}
}
