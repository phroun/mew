package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/hostterm"
	"github.com/phroun/kittytk/style"
)

// This backend orders a line itself and holds it in VISUAL order -- the Hebrew
// turned over, ready to be stamped left to right. Sent to a terminal that runs
// its OWN bidi over what it is given, that line is ordered again and comes back
// the wrong way round.
//
// So on such a host each right-to-left run goes out turned BACK, and the host's
// own pass turns it forward into the picture this backend meant.
func TestARowTurnsBackForAHostThatOrdersIt(t *testing.T) {
	t.Cleanup(core.ForgetHostBidi)

	// "שלום" as this backend holds it: leftmost cell first.
	const laidOut = "םולש"
	const asRead = "שלום"

	frame := func() string {
		b, out := newTestTUI(20, 2)
		b.BeginFrame()
		b.DrawText(0, 0, laidOut, style.DefaultStyle(), nil)
		b.EndFrame()
		return glyphsOf(out.String())
	}

	// A terminal that leaves what it is sent alone gets the picture itself.
	core.SetHostAppliesBidi(false, false, false)
	if got := frame(); !strings.Contains(got, laidOut) {
		t.Errorf("a stream-order host was not sent the row as laid out: %q", got)
	}

	// One that reorders gets it turned back.
	core.SetHostAppliesBidi(true, false, false)
	got := frame()
	if !strings.Contains(got, asRead) {
		t.Errorf("a reordering host was not sent the row turned back: %q", got)
	}
	if strings.Contains(got, laidOut) {
		t.Errorf("a reordering host was sent the row as laid out too: %q", got)
	}
}

// A row with nothing right-to-left in it is the same row either way, and goes
// out through the ordinary diff -- there is nothing to turn.
func TestARowWithNothingToTurnIsLeftAlone(t *testing.T) {
	t.Cleanup(core.ForgetHostBidi)
	core.SetHostAppliesBidi(true, false, false)

	b, out := newTestTUI(20, 2)
	b.BeginFrame()
	b.DrawText(0, 0, "hello", style.DefaultStyle(), nil)
	b.EndFrame()
	if got := out.String(); !strings.Contains(got, "hello") {
		t.Errorf("a left-to-right row did not go out as itself: %q", got)
	}

	// And it is still a diff: changing one cell does not repaint the row.
	out.Reset()
	b.BeginFrame()
	b.DrawText(0, 0, "hellp", style.DefaultStyle(), nil)
	b.EndFrame()
	if got := out.String(); strings.Contains(got, "hell") {
		t.Errorf("a one-cell change repainted the whole row: %q", got)
	}
}

// A bracket this backend mirrored goes out as the character it mirrors: the
// host mirrors it again on the way in.
func TestAMirroredBracketIsSentUnmirrored(t *testing.T) {
	t.Cleanup(core.ForgetHostBidi)
	core.SetHostAppliesBidi(true, false, false)

	b, out := newTestTUI(20, 2)
	b.BeginFrame()
	// "(שלום)" as laid out: the run reversed, both brackets mirrored.
	b.DrawText(0, 0, "(םולש)", style.DefaultStyle(), nil)
	b.EndFrame()

	got := glyphsOf(out.String())
	open, close := strings.IndexRune(got, '('), strings.IndexRune(got, ')')
	if open < 0 || close < 0 {
		t.Fatalf("the brackets did not survive: %q", got)
	}
	if open > close {
		t.Errorf("the brackets went out still mirrored: %q", got)
	}
}

// A reordering host re-processes each parsed line, and attributes coalesced on
// a pen do not survive that -- the colours vanish. So a turned row carries its
// style with every glyph.
func TestATurnedRowCarriesItsStyleWithEveryGlyph(t *testing.T) {
	t.Cleanup(core.ForgetHostBidi)
	core.SetHostAppliesBidi(true, false, false)

	// The row goes out in colour, so a reset alone cannot pass for a style.
	const red = "\033[31m"
	b, out := newTestTUI(20, 2)
	b.colorDepth = 256
	b.BeginFrame()
	b.DrawText(0, 0, "םולש", style.DefaultStyle().WithFg(style.ColorRed), nil)
	b.EndFrame()

	got := out.String()
	body := got[strings.Index(got, "\033[2K")+len("\033[2K"):]
	if n := strings.Count(body, red); n < 4 {
		t.Errorf("four glyphs went out under %d colour escapes; each needs its own: %q",
			n, body)
	}
	// And the colour is set again for each of them, never left standing from the
	// glyph before: no two glyphs are adjacent in the stream.
	for i, r := range []rune("שלום") {
		at := strings.IndexRune(body, r)
		if at < 0 {
			t.Fatalf("glyph %d (%q) did not go out at all: %q", i, r, body)
		}
		if !strings.HasSuffix(body[:at], red) {
			t.Errorf("glyph %d (%q) went out without its own colour: %q", i, r, body)
		}
	}
}

// glyphsOf is what a terminal would actually show of a frame: the escapes
// stripped out, leaving the characters in the order they were sent. A turned
// row carries a style with every glyph, so the glyphs are never adjacent in the
// stream itself.
func glyphsOf(frame string) string {
	var b strings.Builder
	for i := 0; i < len(frame); {
		if frame[i] == 0x1b {
			for i < len(frame) && frame[i] != 'H' && frame[i] != 'm' && frame[i] != 'K' {
				i++
			}
			i++
			continue
		}
		r, n := utf8.DecodeRuneInString(frame[i:])
		b.WriteRune(r)
		i += n
	}
	return b.String()
}

// The backend settles this from the terminal it finds itself in, so a row of
// right-to-left text comes out right without an application having to know --
// or having to tell it.
func TestTheBackendRecognisesTheTerminalItself(t *testing.T) {
	t.Cleanup(func() {
		core.ForgetHostBidi()
		hostterm.Override(hostterm.TerminalUnknown)
	})

	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	hostterm.Override(hostterm.TerminalAppleTerminal)
	core.ForgetHostBidi() // as if nothing had been recognised or said yet
	NewTUIBackend(TUIOptions{Output: &strings.Builder{}})
	if applies, _ := core.HostAppliesBidi(); !applies {
		t.Error("a backend built inside a reordering terminal did not recognise it")
	}

	// And an application that PROBED the terminal -- which can answer for one no
	// name recognises -- has the last word.
	core.SetHostAppliesBidi(false, false, false)
	if applies, _ := core.HostAppliesBidi(); applies {
		t.Error("a host that probed the terminal was overruled by the name it goes by")
	}
}
