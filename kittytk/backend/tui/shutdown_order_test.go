package tui

import (
	"io"
	"strings"
	"testing"
)

// The Kitty keyboard flag stack is PER-SCREEN, so the push and the pop both
// belong INSIDE the alternate screen — the screen the application actually runs
// on.
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
	push := strings.Index(got, "\033[>3u")     // push disambiguate + report-events
	if enter < 0 {
		t.Fatal("startup never enters the alternate screen")
	}
	if push < 0 {
		t.Fatal("startup never pushes the Kitty keyboard protocol")
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
		t.Fatal("shutdown never pops the Kitty keyboard protocol")
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
