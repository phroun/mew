package keys

import (
	"strings"
	"testing"
)

// A letter must not be held as the *prefix of a key name*. With "^B return"
// bound, ^B is a sequence starter and "return" is a continuation key. Pressing
// ^B then the letter r must NOT be swallowed as a partial match of "^B return"
// — "r" is a whole key, not the first character of the key name "return" (the
// classic "r waits for return, t waits for tab" bug). It must unwind and
// dispatch r.
func TestLetterNotHeldAsPrefixOfKeyword(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"^B return": "cmd_return",
		"r":         "insert_r",
	})

	sp.ProcessKey("^B") // held as a sequence starter
	sp.ProcessKey("r")  // must not be held as a prefix of "^B return"

	if seq := sp.GetActiveSequence(); seq != "" {
		t.Fatalf("after ^B then r, still holding %q — the letter r was matched as a prefix of the key name \"return\"", seq)
	}
	foundR := false
	for _, c := range h.calls {
		if strings.HasPrefix(c, "r→") {
			foundR = true
		}
	}
	if !foundR {
		t.Fatalf("the letter r was swallowed instead of dispatched; calls=%v", h.calls)
	}
}

// The whole-token prefix must still HOLD a genuine chord continuation: after
// ^B, pressing the real "return" key (or a key that truly continues a bound
// sequence) resolves the chord rather than unwinding.
func TestGenuineChordContinuationStillResolves(t *testing.T) {
	sp, h := newCaptureSP(map[string]string{
		"^B return": "cmd_return",
	})
	sp.ProcessKey("^B")
	sp.ProcessKey("return")
	if got := h.calls; len(got) != 1 || !strings.HasSuffix(got[0], "→cmd_return") {
		t.Fatalf("^B return should fire cmd_return; calls=%v", got)
	}
}
