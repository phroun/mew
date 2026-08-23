package tui

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// With read-back enabled, RequestClipboardRead emits the OSC 52 query and
// reports that a reply may arrive. GetClipboard itself never blocks - it just
// returns the internal clipboard, which the registered handler updates when the
// terminal answers.
func TestTUIClipboardRequestRead(t *testing.T) {
	var out bytes.Buffer
	opts := DefaultTUIOptions()
	opts.Output = &out
	opts.OSC52Clipboard = true
	opts.OSC52Paste = true
	b := NewTUIBackend(opts)
	b.SetClipboard("internal-old")
	out.Reset()

	if !b.RequestClipboardRead() {
		t.Fatal("RequestClipboardRead should report a possible reply")
	}
	if !strings.Contains(out.String(), "\033]52;c;?\a") {
		t.Errorf("OSC 52 query not emitted; output = %q", out.String())
	}
	// GetClipboard is non-blocking and returns the internal value.
	if got := b.GetClipboard(); got != "internal-old" {
		t.Errorf("GetClipboard = %q, want internal-old", got)
	}
}

// The registered read handler receives the terminal's reply (as the keyboard
// handler's OnClipboard callback would deliver it) and the internal clipboard
// is updated to match.
func TestTUIClipboardReadHandler(t *testing.T) {
	var out bytes.Buffer
	opts := DefaultTUIOptions()
	opts.Output = &out
	opts.OSC52Paste = true
	b := NewTUIBackend(opts)

	got := ""
	b.SetClipboardReadHandler(func(s string) { got = s })
	// Simulate the keyboard handler delivering an OSC 52 reply.
	b.deliverClipboard("from-terminal")

	if got != "from-terminal" {
		t.Errorf("read handler got %q, want from-terminal", got)
	}
	if cb := b.GetClipboard(); cb != "from-terminal" {
		t.Errorf("internal clipboard = %q, want from-terminal", cb)
	}
}

// Without read-back, RequestClipboardRead reports false and emits nothing - the
// caller uses the internal clipboard.
func TestTUIClipboardNoReadBack(t *testing.T) {
	var out bytes.Buffer
	opts := DefaultTUIOptions()
	opts.Output = &out
	opts.OSC52Paste = false
	b := NewTUIBackend(opts)
	b.SetClipboard("kept")
	out.Reset() // ignore the SetClipboard OSC 52 write

	if b.RequestClipboardRead() {
		t.Error("read-back off: RequestClipboardRead should report false")
	}
	if out.Len() != 0 {
		t.Errorf("read-back off should emit no query, got %q", out.String())
	}
	if got := b.GetClipboard(); got != "kept" {
		t.Errorf("GetClipboard = %q, want kept", got)
	}
}

// With OSC 52 enabled, SetClipboard stores the text internally AND emits the
// OSC 52 set sequence (ESC ] 52 ; c ; <base64> BEL) so the terminal's clipboard
// receives the copy. GetClipboard returns the internal copy (Paste source).
func TestTUIClipboardOSC52(t *testing.T) {
	var out bytes.Buffer
	opts := DefaultTUIOptions()
	opts.Output = &out
	opts.OSC52Clipboard = true
	b := NewTUIBackend(opts)

	b.SetClipboard("hi there")

	if got := b.GetClipboard(); got != "hi there" {
		t.Errorf("GetClipboard = %q, want %q", got, "hi there")
	}

	want := "\033]52;c;" + base64.StdEncoding.EncodeToString([]byte("hi there")) + "\a"
	if got := out.String(); got != want {
		t.Errorf("OSC 52 output = %q, want %q", got, want)
	}
}

// With OSC 52 disabled ([tui] clipboard=internal), SetClipboard writes nothing
// to the terminal and keeps an internal-only clipboard.
func TestTUIClipboardInternalOnly(t *testing.T) {
	var out bytes.Buffer
	opts := DefaultTUIOptions()
	opts.Output = &out
	opts.OSC52Clipboard = false
	b := NewTUIBackend(opts)

	b.SetClipboard("secret")

	if out.Len() != 0 {
		t.Errorf("internal-only mode wrote %q to the terminal, want nothing", out.String())
	}
	if got := b.GetClipboard(); got != "secret" {
		t.Errorf("GetClipboard = %q, want secret", got)
	}
}

// The emitted sequence must be a well-formed OSC 52 set: no stray ESC/BEL
// inside, correct target selection ("c").
func TestTUIClipboardOSC52Framing(t *testing.T) {
	var out bytes.Buffer
	opts := DefaultTUIOptions()
	opts.Output = &out
	opts.OSC52Clipboard = true
	b := NewTUIBackend(opts)

	b.SetClipboard("x")
	s := out.String()
	if !strings.HasPrefix(s, "\033]52;c;") {
		t.Errorf("missing OSC 52 header in %q", s)
	}
	if !strings.HasSuffix(s, "\a") {
		t.Errorf("OSC 52 not BEL-terminated in %q", s)
	}
}
