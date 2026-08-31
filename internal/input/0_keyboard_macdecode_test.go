package input

import (
	"bytes"
	"io"
	"testing"
)

// With macOS Option decoding on, a plain ASCII double quote must arrive as a
// straight quote — not decoded to M-[. Regression for a decode-table entry that
// keyed on ASCII '"' (U+0022) instead of the curly '“' (U+201C) Option+[ emits,
// which round-tripped a typed double quote into a curly one (dep >= v0.3.5).
// A genuine Option+[ (U+201C) must still decode to M-[ so real combos bind.
func TestOptionDecodeKeepsPlainDoubleQuote(t *testing.T) {
	// Plain double quote, then the character a real Option+[ produces (U+201C).
	input := bytes.NewReader([]byte("\"“"))

	kh := NewKeyboardHandler(input, io.Discard)
	kh.SetDecodeMacOSOption(true) // simulate macOS (off by default off-Darwin)
	if err := kh.handler.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer kh.Stop()

	evs := make(chan InputEvent, 8)
	go func() {
		for {
			evs <- kh.GetEvent()
		}
	}()

	if ev := nextEvent(t, evs); ev.Key != "\"" {
		t.Fatalf("plain double quote decoded to %q, want a straight %q", ev.Key, "\"")
	}
	if ev := nextEvent(t, evs); ev.Key != "M-[" {
		t.Fatalf("real Option+[ (U+201C) should decode to M-[, got %q", ev.Key)
	}
}
