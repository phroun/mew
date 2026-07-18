package input

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// nextEvent returns the next input event or fails if none arrives promptly.
// A regressed paste path (where pasted characters are re-emitted as keys and
// mismatch-swallowed) would starve real keystrokes and trip this timeout.
func nextEvent(t *testing.T, evs <-chan InputEvent) InputEvent {
	t.Helper()
	select {
	case ev := <-evs:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for input event (paste likely swallowed real keys)")
		return InputEvent{}
	}
}

// A large bracketed paste (well over the 256-slot Keys channel) must deliver
// its content through paste events only, and a real keystroke typed afterward
// must still come through. This is the regression guard for the paste hang:
// the handler runs with EmitPasteKeys=false, so paste never floods the Keys
// channel and no dropped-echo bookkeeping can strand real input.
func TestLargePasteDoesNotSwallowKeys(t *testing.T) {
	const pasteStart = "\x1b[200~"
	const pasteEnd = "\x1b[201~"
	body := strings.Repeat("a", 500) // > KeyBufferSize (256)

	// Paste the body, then type a real 'b' after it.
	input := bytes.NewReader([]byte(pasteStart + body + pasteEnd + "b"))

	kh := NewKeyboardHandler(input, io.Discard)
	if err := kh.handler.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer kh.Stop()

	// Pump events off the handler on a background goroutine so the test can
	// apply a timeout to each read.
	evs := make(chan InputEvent, 16)
	go func() {
		for {
			evs <- kh.GetEvent()
		}
	}()

	// Collect the paste, which arrives as one or more paste events. No key
	// event may appear before the paste's final chunk — that would mean the
	// paste leaked onto the Keys channel.
	var pasted strings.Builder
	for {
		ev := nextEvent(t, evs)
		if ev.Key != "" {
			t.Fatalf("paste content leaked as a key event: %q", ev.Key)
		}
		if ev.Paste == nil {
			t.Fatalf("unexpected event before paste completed: %+v", ev)
		}
		pasted.Write(ev.Paste.Content)
		if ev.Paste.IsFinal {
			break
		}
	}
	if pasted.String() != body {
		t.Fatalf("paste content mismatch: got %d bytes, want %d", pasted.Len(), len(body))
	}

	// The keystroke typed after the paste must still be delivered.
	ev := nextEvent(t, evs)
	if ev.Key != "b" {
		t.Fatalf("expected real keystroke 'b' after paste, got %+v", ev)
	}
}
