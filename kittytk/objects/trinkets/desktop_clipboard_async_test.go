package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/objects/window"
)

// asyncClipBackend is a headless backend that also implements
// core.AsyncClipboardReader, so the desktop drives its async read path.
type asyncClipBackend struct {
	*nullBackend
	clip     string
	handler  func(string)
	replyOK  bool // RequestClipboardRead's return (terminal may answer)
	requests int
}

func (b *asyncClipBackend) GetClipboard() string  { return b.clip }
func (b *asyncClipBackend) SetClipboard(s string) { b.clip = s }
func (b *asyncClipBackend) RequestClipboardRead() bool {
	b.requests++
	return b.replyOK
}
func (b *asyncClipBackend) SetClipboardReadHandler(fn func(string)) { b.handler = fn }
func (b *asyncClipBackend) deliver(s string) {
	if b.handler != nil {
		b.handler(s)
	}
}

func newAsyncClipDesktop(t *testing.T, be *asyncClipBackend) *Desktop {
	t.Helper()
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	d.SetBackend(be) // wires the read handler (Post runs inline without a platform)
	return d
}

func modalCount(d *Desktop) int { return len(d.windowManager.Windows()) }

// When the terminal can't answer (RequestClipboardRead false), the read
// resolves immediately with the internal clipboard - no modal, no pending wait.
func TestReadClipboardAsyncSyncFallback(t *testing.T) {
	be := &asyncClipBackend{nullBackend: &nullBackend{}, clip: "sync-value", replyOK: false}
	d := newAsyncClipDesktop(t, be)

	got := ""
	called := 0
	d.ReadClipboardAsync(func(s string) { got = s; called++ })

	if called != 1 || got != "sync-value" {
		t.Errorf("resolved %d times with %q, want once with sync-value", called, got)
	}
	if d.clipWait != nil {
		t.Error("no async wait should remain")
	}
	if modalCount(d) != 0 {
		t.Error("no modal should be shown for a synchronous read")
	}
}

// A reply arriving before the grace period resolves the read without ever
// showing the modal.
func TestReadClipboardAsyncFastReply(t *testing.T) {
	be := &asyncClipBackend{nullBackend: &nullBackend{}, clip: "internal", replyOK: true}
	d := newAsyncClipDesktop(t, be)

	got := ""
	d.ReadClipboardAsync(func(s string) { got = s })
	if d.clipWait == nil {
		t.Fatal("read should be pending while awaiting the terminal")
	}

	be.deliver("from-terminal")
	if got != "from-terminal" {
		t.Errorf("resolved with %q, want from-terminal", got)
	}
	if d.clipWait != nil {
		t.Error("wait should be cleared after the reply")
	}
	if modalCount(d) != 0 {
		t.Error("a fast reply must not show the waiting modal")
	}
}

// When the grace period elapses with no reply, the waiting modal appears; a
// later reply closes it and delivers the terminal's value.
func TestReadClipboardAsyncModalThenReply(t *testing.T) {
	be := &asyncClipBackend{nullBackend: &nullBackend{}, clip: "internal", replyOK: true}
	d := newAsyncClipDesktop(t, be)

	got := ""
	d.ReadClipboardAsync(func(s string) { got = s })
	w := d.clipWait
	if w == nil {
		t.Fatal("read should be pending")
	}

	// Fire the grace timer directly (no real wait).
	d.clipboardGraceElapsed(w)
	if w.modal == nil || modalCount(d) != 1 {
		t.Fatalf("waiting modal not shown (modal=%v count=%d)", w.modal != nil, modalCount(d))
	}
	if got != "" {
		t.Error("must not resolve just because the modal appeared")
	}

	be.deliver("from-terminal")
	if got != "from-terminal" {
		t.Errorf("resolved with %q, want from-terminal", got)
	}
	if d.clipWait != nil {
		t.Error("wait should be cleared after the reply")
	}
}

// Cancelling the modal (no reply) resolves with the internal clipboard.
func TestReadClipboardAsyncModalCancel(t *testing.T) {
	be := &asyncClipBackend{nullBackend: &nullBackend{}, clip: "internal-fallback", replyOK: true}
	d := newAsyncClipDesktop(t, be)

	got := ""
	resolved := 0
	d.ReadClipboardAsync(func(s string) { got = s; resolved++ })
	w := d.clipWait
	d.clipboardGraceElapsed(w)
	if w.modal == nil {
		t.Fatal("waiting modal not shown")
	}

	// Cancel the dialog.
	w.modal.done(ResultCancel)
	if got != "internal-fallback" || resolved != 1 {
		t.Errorf("cancel resolved %d times with %q, want once with internal-fallback", resolved, got)
	}

	// A reply arriving after cancel must not resolve again (exactly-once).
	be.deliver("late-terminal")
	if resolved != 1 || got != "internal-fallback" {
		t.Errorf("post-cancel reply changed result to %q (resolved %d times)", got, resolved)
	}
}

// A second read while one is pending resolves immediately with the internal
// clipboard rather than starting a competing query.
func TestReadClipboardAsyncSinglePending(t *testing.T) {
	be := &asyncClipBackend{nullBackend: &nullBackend{}, clip: "internal", replyOK: true}
	d := newAsyncClipDesktop(t, be)

	d.ReadClipboardAsync(func(string) {})
	if be.requests != 1 {
		t.Fatalf("first read should query once, got %d", be.requests)
	}

	second := ""
	d.ReadClipboardAsync(func(s string) { second = s })
	if second != "internal" {
		t.Errorf("second concurrent read = %q, want internal (no competing query)", second)
	}
	if be.requests != 1 {
		t.Errorf("second read should not query again, requests=%d", be.requests)
	}
}
