package editor

import (
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// os_exchange swaps the block with the clipboard: the block's text goes out,
// the clipboard's text comes in to replace it. This is the whole reason the
// command exists — os_copy followed by os_paste cannot do it, because after
// the copy the clipboard holds the block and the paste only puts it back.
func TestOSExchangeSwapsBothWays(t *testing.T) {
	e, w := newTestEditor(t, "hello world\n")
	clip := stubClipboard(e)
	*clip = "GOODBYE"
	markBlock(w, 0, 0, 0, 5)
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})

	if !e.osExchangeApply(*clip) {
		t.Fatal("os_exchange should have applied")
	}
	if got := docContent(w); got != "GOODBYE world" {
		t.Errorf("buffer = %q, want %q", got, "GOODBYE world")
	}
	if *clip != "hello" {
		t.Errorf("clipboard = %q, want the outgoing block %q", *clip, "hello")
	}
}

// A round trip returns both sides to where they started — the property that
// makes it an exchange rather than two independent operations.
func TestOSExchangeRoundTrips(t *testing.T) {
	e, w := newTestEditor(t, "hello world\n")
	clip := stubClipboard(e)
	*clip = "GOODBYE"
	markBlock(w, 0, 0, 0, 5)
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})

	e.osExchangeApply(*clip)
	e.osExchangeApply(*clip)

	if got := docContent(w); got != "hello world" {
		t.Errorf("after two exchanges buffer = %q, want the original", got)
	}
	if *clip != "GOODBYE" {
		t.Errorf("after two exchanges clipboard = %q, want the original", *clip)
	}
}

// With no block marked there is nothing to exchange: the command warns and
// leaves the clipboard alone rather than pasting at the caret.
func TestOSExchangeNoBlock(t *testing.T) {
	e, _ := newTestEditor(t, "hello\n")
	clip := stubClipboard(e)
	*clip = "sentinel"

	if e.osExchangeApply("incoming") {
		t.Fatal("no block: exchange must not apply")
	}
	if *clip != "sentinel" {
		t.Errorf("clipboard = %q, want it untouched", *clip)
	}
	if !hasWarning(e, "No block marked") {
		t.Error("expected the no-block warning")
	}
}

// The caret outside the block is the case where osPasteText would INSERT
// rather than replace. That is not an exchange, so it is refused whole: the
// buffer and the clipboard both stay as they were.
func TestOSExchangeCaretOutsideBlockRefuses(t *testing.T) {
	e, w := newTestEditor(t, "hello world\n")
	clip := stubClipboard(e)
	*clip = "GOODBYE"
	markBlock(w, 0, 0, 0, 5)
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 9}) // past the block's end

	if e.osExchangeApply(*clip) {
		t.Fatal("caret outside the block: exchange must not apply")
	}
	if got := docContent(w); got != "hello world" {
		t.Errorf("buffer = %q, want it untouched", got)
	}
	if *clip != "GOODBYE" {
		t.Errorf("clipboard = %q, want it untouched", *clip)
	}
}

// The ordering guard: when the replace FAILS the clipboard must still hold
// what it held, not the outgoing block. An empty clipboard is the case that
// reaches the replace and refuses there — a read-only viewport bails at the
// content-lock check further up, so it never exercises this at all.
//
// Publishing before replacing would lose the user's clipboard AND give them
// nothing back for it.
func TestOSExchangePublishesOnlyAfterTheReplaceLands(t *testing.T) {
	e, w := newTestEditor(t, "hello world\n")
	clip := stubClipboard(e)
	*clip = "" // an empty clipboard makes osPasteText refuse
	markBlock(w, 0, 0, 0, 5)
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})

	if e.osExchangeApply(*clip) {
		t.Fatal("empty clipboard: exchange must not apply")
	}
	if got := docContent(w); got != "hello world" {
		t.Errorf("buffer = %q, want it untouched", got)
	}
	if *clip != "" {
		t.Errorf("clipboard = %q, want it still empty — a refused exchange must not "+
			"publish the outgoing block when nothing came back for it", *clip)
	}
}

// A read-only viewport rejects the mutation outright, at the content lock.
func TestOSExchangeReadOnlyLeavesClipboard(t *testing.T) {
	e, w := newTestEditor(t, "hello world\n")
	clip := stubClipboard(e)
	*clip = "GOODBYE"
	markBlock(w, 0, 0, 0, 5)
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
	e.setOption(w, "readOnly", "true")

	if e.osExchangeApply(*clip) {
		t.Fatal("read-only: exchange must not apply")
	}
	if got := docContent(w); got != "hello world" {
		t.Errorf("buffer = %q, want it untouched", got)
	}
	if *clip != "GOODBYE" {
		t.Errorf("clipboard = %q, want it untouched — a refused exchange must not "+
			"leave the block on the clipboard with nothing given back", *clip)
	}
}

// os_exchange with no host clipboard warns instead of half-running.
func TestOSExchangeNoHostClipboard(t *testing.T) {
	e, w := newTestEditor(t, "hello world\n")
	markBlock(w, 0, 0, 0, 5)
	if e.osExchange() {
		t.Fatal("no host clipboard: os_exchange must report failure")
	}
	if !hasWarning(e, "No host clipboard") {
		t.Error("expected the no-clipboard warning")
	}
}
