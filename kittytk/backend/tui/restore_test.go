package tui

import (
	"io"
	"strings"
	"testing"
)

// A backend that owns the terminal joins the registry, so an exit path with no
// reference to it can still hand the terminal back. This is the whole point:
// os.Exit runs no deferred teardown, and mew's fatal-signal handler calls it.
func TestRestoreAllHandsBackWithoutAReference(t *testing.T) {
	before := LiveCount()
	var out strings.Builder
	b := NewTUIBackend(TUIOptions{Output: io.Discard})
	b.ttyOut = &out
	registerLive(b) // Init does this once the terminal is actually ours

	if LiveCount() != before+1 {
		t.Fatalf("live count = %d, want %d", LiveCount(), before+1)
	}

	RestoreAll() // no reference to b

	if got := out.String(); !strings.Contains(got, "\033[?1049l") || !strings.Contains(got, "\033[<u") {
		t.Errorf("RestoreAll did not restore the terminal, wrote %q", got)
	}
	if LiveCount() != before {
		t.Errorf("a restored backend should leave the registry, live = %d", LiveCount())
	}
}

// Restoring twice must not double-write, and must not panic: several paths can
// reach it at once (Shutdown racing an emergency handler).
func TestRestoreIsIdempotent(t *testing.T) {
	var out strings.Builder
	b := NewTUIBackend(TUIOptions{Output: io.Discard})
	b.ttyOut = &out
	registerLive(b)

	b.RestoreTerminal()
	first := out.String()
	b.RestoreTerminal()
	RestoreAll()

	if out.String() != first {
		t.Error("restoring again wrote more escapes; it must happen exactly once")
	}
}

// Shutdown closes a channel, so a second call used to panic - a poor way to end
// an emergency, and reachable now that an emergency path also restores.
func TestShutdownTwiceDoesNotPanic(t *testing.T) {
	var out strings.Builder
	b := NewTUIBackend(TUIOptions{Output: io.Discard})
	b.ttyOut = &out
	b.Shutdown()
	b.Shutdown() // must not panic on a re-closed stopChan
}
