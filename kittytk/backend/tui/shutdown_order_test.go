package tui

import (
	"io"
	"strings"
	"testing"
)

// The Kitty keyboard flag stack is PER-SCREEN, so the push and the pop have to
// straddle the alternate-screen switch the same way. Init pushes while still on
// the main screen; Shutdown must therefore pop only after returning to it.
// Popping first leaves the main screen's push in place, the shell inherits it,
// and Ctrl+C starts printing "…9;5u" instead of interrupting.
func TestShutdownPopsKeyboardProtocolAfterLeavingAltScreen(t *testing.T) {
	var out strings.Builder
	b := NewTUIBackend(TUIOptions{Output: io.Discard, EnableMouse: true})
	// Capture the MODE escapes specifically. Without this the result would turn
	// on whether the test runner has a controlling terminal to open.
	b.ttyOut = &out
	b.stopChan = make(chan struct{})
	b.Shutdown()

	got := out.String()
	leave := strings.Index(got, "\033[?1049l") // back to the main screen
	pop := strings.Index(got, "\033[<u")       // pop the flag stack
	if leave < 0 {
		t.Fatal("shutdown never leaves the alternate screen")
	}
	if pop < 0 {
		t.Fatal("shutdown never pops the Kitty keyboard protocol")
	}
	if pop < leave {
		t.Errorf("pop at %d precedes the alt-screen exit at %d: it would apply to "+
			"the alternate screen's flag stack and leave the main screen's push live", pop, leave)
	}
	if reset := strings.Index(got, "\033[=0;1u"); reset < 0 {
		t.Error("no explicit flag reset for terminals honouring flags but not the stack")
	}
}
