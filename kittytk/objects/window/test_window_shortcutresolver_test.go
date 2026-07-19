package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A window with no chrome of its own (a torn-off child) services its
// app's shortcuts through the resolver the desktop installs, checked
// before the focused trinket sees the key.
func TestShortcutResolverHandlesKey(t *testing.T) {
	win := NewWindow("child")

	got := ""
	win.SetShortcutResolver(func(ev core.KeyPressEvent) bool {
		if ev.Key == "C-x" {
			got = ev.Key
			return true
		}
		return false
	})

	if !win.HandleKeyPress(core.KeyPressEvent{Key: "C-x"}) {
		t.Fatal("resolver key was not consumed")
	}
	if got != "C-x" {
		t.Errorf("resolver saw %q, want C-x", got)
	}

	// A key the resolver rejects is not consumed by it.
	if win.HandleKeyPress(core.KeyPressEvent{Key: "C-q"}) {
		t.Error("unrelated key was wrongly consumed by the resolver")
	}

	// Clearing the resolver stops it servicing keys.
	win.SetShortcutResolver(nil)
	got = ""
	win.HandleKeyPress(core.KeyPressEvent{Key: "C-x"})
	if got != "" {
		t.Error("cleared resolver still fired")
	}
}

// Raw key input makes the window pass its next key straight to the
// focused trinket, bypassing its own shortcut handling, then fires the
// done callback and reverts to normal (one-shot) handling.
func TestBeginRawKeyInputBypassesShortcuts(t *testing.T) {
	win := NewWindow("term")

	shortcutHits := 0
	win.SetShortcutResolver(func(ev core.KeyPressEvent) bool {
		shortcutHits++
		return true // pretend every key is an app shortcut
	})

	// Normally the resolver eats the key.
	win.HandleKeyPress(core.KeyPressEvent{Key: "C-c"})
	if shortcutHits != 1 {
		t.Fatalf("resolver hits = %d, want 1 before raw mode", shortcutHits)
	}

	done := 0
	win.BeginRawKeyInput(func() { done++ })

	// In raw mode the next key bypasses the resolver and the done
	// callback fires.
	if !win.HandleKeyPress(core.KeyPressEvent{Key: "C-c"}) {
		t.Error("raw key was not consumed")
	}
	if shortcutHits != 1 {
		t.Errorf("resolver fired during raw mode: hits = %d", shortcutHits)
	}
	if done != 1 {
		t.Errorf("raw-key done callback fired %d times, want 1", done)
	}

	// Mode is one-shot: the following key hits the resolver again.
	win.HandleKeyPress(core.KeyPressEvent{Key: "C-c"})
	if shortcutHits != 2 {
		t.Errorf("resolver hits = %d after raw mode, want 2", shortcutHits)
	}
}
