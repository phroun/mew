package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The always-present paste targets implement core.PasteHandler, so the focus
// manager routes a paste to them. (The mew Editor, which embeds *PurfecTerm, is
// asserted in the -tags mew build; the stock !mew Editor is an external-editor
// placeholder with no in-surface buffer to paste into.)
var (
	_ core.PasteHandler = (*PurfecTerm)(nil)
	_ core.PasteHandler = (*TextInput)(nil)
)

// captureChild wires a PurfecTerm's input sink to collect the bytes it would
// send to its child process.
func captureChild(t *PurfecTerm) *[]byte {
	buf := &[]byte{}
	t.SetInputSink(func(b []byte) { *buf = append(*buf, b...) })
	return buf
}

// HandlePaste forwards pasted text to the child RAW when the child has not
// enabled bracketed paste. The host's own bracketing does not leak through: the
// child's mode decides, and here it is off.
func TestHandlePasteSendsRawWhenChildHasNoBracketedPaste(t *testing.T) {
	term := NewPurfecTerm()
	got := captureChild(term)

	if !term.HandlePaste(core.PasteEvent{Text: "abc"}) {
		t.Fatal("HandlePaste declined a paste the terminal can hold")
	}
	if string(*got) != "abc" {
		t.Errorf("child received %q, want raw %q", string(*got), "abc")
	}
}

// HandlePaste RE-BRACKETS for the child when the child enabled bracketed paste
// (mew does exactly this). The child then sees a real paste, atomically, rather
// than a run of keystrokes — which is the whole point of the fix.
func TestHandlePasteReBracketsWhenChildEnabledBracketedPaste(t *testing.T) {
	term := NewPurfecTerm()
	// The child turns bracketed paste on by emitting DECSET 2004; the emulator
	// records it in its buffer, exactly as an inner mew would.
	term.Feed([]byte("\x1b[?2004h"))
	got := captureChild(term)

	if !term.HandlePaste(core.PasteEvent{Text: "abc"}) {
		t.Fatal("HandlePaste declined a paste the terminal can hold")
	}
	want := "\x1b[200~abc\x1b[201~"
	if string(*got) != want {
		t.Errorf("child received %q, want re-bracketed %q", string(*got), want)
	}
}

// Newlines are normalized to CR on the way to the child, so the child's line
// discipline acts on them (LF alone would be swallowed).
func TestHandlePasteNormalizesNewlines(t *testing.T) {
	term := NewPurfecTerm()
	got := captureChild(term)

	term.HandlePaste(core.PasteEvent{Text: "a\nb"})
	if string(*got) != "a\rb" {
		t.Errorf("child received %q, want %q", string(*got), "a\rb")
	}
}
