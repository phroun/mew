package editor

import (
	"strings"
	"testing"
	"unicode"
)

// degenerateToASCII stands where the display-time decision will eventually be
// made. For now it is unconditional, and this is its contract: typographic
// characters become ASCII stand-ins and everything else is left exactly alone.
func TestDegenerateToASCII(t *testing.T) {
	cases := []struct{ in, want string }{
		{"You can’t stop me", "You can't stop me"},
		{"the manual’s viewport", "the manual's viewport"},
		{"‘quoted’ and “quoted”", "'quoted' and \"quoted\""},
		{"an em — dash and an en – dash", "an em -- dash and an en - dash"},
		{"wait…", "wait..."},
		{"no break", "no break"},
		{"6′2″", `6'2"`},
		// Already ASCII, and text with no typography at all, pass through.
		{"plain ASCII 'quotes' and -- dashes", "plain ASCII 'quotes' and -- dashes"},
		{"", ""},
	}
	for _, c := range cases {
		if got := degenerateToASCII(c.in); got != c.want {
			t.Errorf("degenerateToASCII(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// It degenerates TYPOGRAPHY, not Unicode. mew is an editor for every script it
// can render, and a function that flattened non-ASCII in general would be a bug
// waiting to be pointed at a message containing a filename.
func TestDegenerateToASCIILeavesRealTextAlone(t *testing.T) {
	for _, s := range []string{
		"שלום",       // Hebrew
		"مرحبا",      // Arabic
		"日本語",        // CJK
		"naïve café", // Latin with diacritics
		"→ ← ↑ ↓",    // arrows: no honest ASCII equivalent, so left as they are
		"• bullet",   // likewise
		"░█",         // mew's own scrollbar glyphs
	} {
		if got := degenerateToASCII(s); got != s {
			t.Errorf("degenerateToASCII(%q) = %q, want it untouched", s, got)
		}
	}
}

// The one real caller today: the help wiki's refusal is written with real
// apostrophes and reaches the screen as ASCII. Both halves are asserted, because
// the point of the exercise is that the SOURCE stays correct while the OUTPUT
// bends — a message that had been flattened at the source would pass a test of
// the output alone.
func TestWikiDeclineIsTypographicAtSourceAndASCIIOnScreen(t *testing.T) {
	why := wikiDeclineExec("help")
	if !strings.Contains(why, "’") {
		t.Errorf("the registered message should use a real apostrophe; got %q", why)
	}

	e, _ := newTestEditor(t, "x\n")
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	e.executeCommand("help_open")
	e.executeCommand("shell")

	var shown string
	for _, vw := range e.ViewportManager.AllViewports() {
		if vw.Class == "warning" && strings.Contains(vw.MessageTopInner, "help manual") {
			shown = vw.MessageTopInner
		}
	}
	if shown == "" {
		t.Fatal("no refusal warning found")
	}
	for _, r := range shown {
		if r > unicode.MaxASCII {
			t.Fatalf("warning reached the screen with non-ASCII %q in %q", r, shown)
		}
	}
	if !strings.Contains(shown, "can't") {
		t.Errorf("warning = %q, want the apostrophe degenerated to ASCII", shown)
	}
}
