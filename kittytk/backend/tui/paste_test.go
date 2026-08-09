package tui

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/phroun/direct-key-handler/keyboard"
	"github.com/phroun/kittytk/core"
)

// wireKeyboardLikeInit builds a keyboard.Handler wired the way
// TUIBackend.Init wires it: OnKey -> handleKey, OnPaste -> deliverPaste, and
// EmitPasteKeys off so paste is delivered whole rather than re-echoed as keys.
// The InputReader is a pipe so the test can feed raw bytes; the returned writer
// is the feed end.
func wireKeyboardLikeInit(t *testing.T, b *TUIBackend) io.WriteCloser {
	t.Helper()
	pr, pw := io.Pipe()
	noPasteKeys := false
	h := keyboard.New(keyboard.Options{InputReader: pr, EmitPasteKeys: &noPasteKeys})
	h.OnKey = b.handleKey
	h.OnPaste = func(content []byte) { b.deliverPaste(string(content)) }
	if err := h.Start(); err != nil {
		t.Fatalf("keyboard start: %v", err)
	}
	t.Cleanup(func() { _ = h.Stop(); _ = pw.Close() })
	return pw
}

// drainEvents polls the backend's event queue until it is quiet for a short
// settle window, returning everything it produced.
func drainEvents(b *TUIBackend) []core.Event {
	var out []core.Event
	idle := 0
	for idle < 20 { // ~200ms of quiet ends the drain
		if ev := b.PollEvent(); ev != nil {
			out = append(out, ev)
			idle = 0
			continue
		}
		time.Sleep(10 * time.Millisecond)
		idle++
	}
	return out
}

// A bracketed paste arriving from the outer terminal is delivered to the app as
// exactly one core.PasteEvent carrying the whole body — NOT as a burst of
// per-character key events. This is the fix for the paste-flood/drop bug: the
// terminal host takes paste via OnPaste and does not re-echo it as keystrokes.
func TestBracketedPasteIsOnePasteEvent(t *testing.T) {
	opts := DefaultTUIOptions()
	opts.Output = io.Discard
	b := NewTUIBackend(opts)

	pw := wireKeyboardLikeInit(t, b)

	const body = "hello, this is a pasted line"
	if _, err := io.WriteString(pw, "\x1b[200~"+body+"\x1b[201~"); err != nil {
		t.Fatalf("feed paste: %v", err)
	}

	var pastes, keys int
	var got strings.Builder
	for _, ev := range drainEvents(b) {
		switch e := ev.(type) {
		case core.PasteEvent:
			pastes++
			got.WriteString(e.Text)
		case core.KeyPressEvent:
			keys++
		}
	}

	if pastes != 1 {
		t.Errorf("want exactly 1 PasteEvent, got %d", pastes)
	}
	if keys != 0 {
		t.Errorf("paste must not be re-emitted as keys, got %d KeyPressEvent(s)", keys)
	}
	if got.String() != body {
		t.Errorf("paste body = %q, want %q", got.String(), body)
	}
}

// A paste far larger than the 256-slot event queue still arrives intact,
// because deliverPaste enqueues one blocking event rather than 1000 droppable
// ones. This is the regression guard for "if it's too long it starts dropping
// pieces out."
func TestLargePasteArrivesIntact(t *testing.T) {
	opts := DefaultTUIOptions()
	opts.Output = io.Discard
	b := NewTUIBackend(opts)

	pw := wireKeyboardLikeInit(t, b)

	body := strings.Repeat("abcdefghij", 100) // 1000 chars, 4x the queue
	if _, err := io.WriteString(pw, "\x1b[200~"+body+"\x1b[201~"); err != nil {
		t.Fatalf("feed paste: %v", err)
	}

	var got strings.Builder
	for _, ev := range drainEvents(b) {
		if e, ok := ev.(core.PasteEvent); ok {
			got.WriteString(e.Text)
		}
	}

	if got.String() != body {
		t.Errorf("large paste lost data: delivered %d of %d bytes", got.Len(), len(body))
	}
}
