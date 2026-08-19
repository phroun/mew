package tui

import (
	"io"
	"strings"
	"testing"
)

// The "kitty" keyboard flag stack is PER-SCREEN, so the push and the pop both
// belong INSIDE the alternate screen — the screen the application actually
// runs on.
//
// They used to straddle it: the push went out before ?1049h and the pop after
// ?1049l, so both landed on the MAIN screen's stack. Nothing reads that stack
// while an alternate-screen application is running, so the outer terminal never
// enabled event reporting and no key RELEASE was ever sent — the whole
// release chain below this, backend to window to trinket to emulator, was fed
// by a terminal that had not been asked for the events. The same misplacement
// left the push standing on the screen the shell came back to.
func TestKeyboardProtocolIsPushedAndPoppedInsideTheAltScreen(t *testing.T) {
	var up strings.Builder
	b := NewTUIBackend(TUIOptions{Output: io.Discard, EnableMouse: true})
	// Capture the MODE escapes specifically. Without this the result would turn
	// on whether the test runner has a controlling terminal to open.
	b.ttyOut = &up
	b.enterTerminalModes()

	got := up.String()
	enter := strings.Index(got, "\033[?1049h") // switch to the alternate screen
	// Disambiguate + report-events + report-all-keys. The last is what makes a
	// keypad key arrive as a keypad key: without it the pad's 7 goes down as
	// the bare byte "7" and comes back up as keycode 57406, one key reported
	// as two.
	push := strings.Index(got, "\033[>11u")
	if enter < 0 {
		t.Fatal("startup never enters the alternate screen")
	}
	if push < 0 {
		t.Fatal("startup never pushes the \"kitty\" keyboard protocol")
	}
	if push < enter {
		t.Errorf("push at %d precedes the alt-screen switch at %d: it lands on the "+
			"main screen's flag stack, so the screen we run on stays legacy and "+
			"no key release is ever sent", push, enter)
	}

	var down strings.Builder
	b.ttyOut = &down
	b.stopChan = make(chan struct{})
	b.Shutdown()

	got = down.String()
	leave := strings.Index(got, "\033[?1049l") // back to the main screen
	pop := strings.Index(got, "\033[<u")       // pop the flag stack
	if leave < 0 {
		t.Fatal("shutdown never leaves the alternate screen")
	}
	if pop < 0 {
		t.Fatal("shutdown never pops the \"kitty\" keyboard protocol")
	}
	if pop > leave {
		t.Errorf("pop at %d follows the alt-screen exit at %d: it would pop the main "+
			"screen's stack, which we never pushed, and leave our push standing on "+
			"the screen we just left", pop, leave)
	}
	if reset := strings.Index(got, "\033[=0;1u"); reset < 0 {
		t.Error("no explicit flag reset for terminals honouring flags but not the stack")
	}
}

// Focus reporting is enabled on the way in and disabled on the way out.
//
// The pairing is the whole point: a mode left on outlives this process and
// lands on the user's shell, which would then be sent CSI I and CSI O on every
// alt-tab and type them as text.
//
// It is enabled at all because of what happens to a key held across a focus
// change. Its key-up is delivered to whoever has the keyboard now and never
// arrives here, so without the notification the press stands for good in
// anything tracking held keys — direct-key-handler releases them on the report,
// and this is what makes the report come.
func TestFocusReportingIsEnabledAndDisabledAsAPair(t *testing.T) {
	var up strings.Builder
	b := NewTUIBackend(TUIOptions{Output: io.Discard, EnableMouse: true})
	b.ttyOut = &up
	b.enterTerminalModes()
	if !strings.Contains(up.String(), "\033[?1004h") {
		t.Error("startup never enables focus reporting, so no focus report ever " +
			"arrives and a key held across a blur is never released")
	}

	var down strings.Builder
	b.ttyOut = &down
	b.stopChan = make(chan struct{})
	b.Shutdown()

	got := down.String()
	off := strings.Index(got, "\033[?1004l")
	if off < 0 {
		t.Fatal("shutdown never disables focus reporting; the shell inherits it " +
			"and is sent CSI I / CSI O on every alt-tab")
	}
	// Before leaving the alternate screen, with the rest of the teardown: the
	// order is not load-bearing the way the flag stack's is, but a mode turned
	// off after the screen it was turned on for is a habit worth not forming.
	if leave := strings.Index(got, "\033[?1049l"); leave >= 0 && off > leave {
		t.Errorf("focus reporting disabled at %d, after the alt-screen exit at %d",
			off, leave)
	}
}
