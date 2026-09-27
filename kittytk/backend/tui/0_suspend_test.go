package tui

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/phroun/direct-key-handler/keyboard"
	"github.com/phroun/kittytk/core"
)

// suspendableBackend is a backend with its mode escapes captured and no
// terminal of its own, so a suspend can run in a test without stopping it.
func suspendableBackend(t *testing.T) (*TUIBackend, *strings.Builder) {
	t.Helper()
	var out strings.Builder
	b := NewTUIBackend(TUIOptions{Output: io.Discard, EnableMouse: true})
	b.ttyOut = &out
	b.fd = -1
	b.cols, b.rows = 80, 24
	b.allocateBuffers()
	return b, &out
}

// The terminal goes back to the shell BEFORE the process stops and is taken
// again AFTER it is continued, with the keyboard protocol popped inside the
// alternate screen on the way out and pushed inside it on the way back, as at
// exit and at startup.
func TestASuspendHandsTheTerminalBackAroundTheStop(t *testing.T) {
	b, out := suspendableBackend(t)
	var atStop string
	b.suspend(func() { atStop = out.String() })
	after := strings.TrimPrefix(out.String(), atStop)

	leave := strings.Index(atStop, "\033[?1049l")
	pop := strings.Index(atStop, "\033[<u")
	if leave < 0 || pop < 0 {
		t.Fatalf("the process stopped before the terminal was handed back; wrote %q", atStop)
	}
	if pop > leave {
		t.Errorf("pop at %d follows the alt-screen exit at %d; it would pop the "+
			"shell's screen's stack instead of ours", pop, leave)
	}
	for _, mode := range []string{"\033[?1004l", "\033[?2004l", "\033[?1000l"} {
		if !strings.Contains(atStop, mode) {
			t.Errorf("hand-back leaves %q on for the shell", mode)
		}
	}

	enter := strings.Index(after, "\033[?1049h")
	_, push, pushed := pushedKeyboardFlags(after)
	if enter < 0 || !pushed {
		t.Fatalf("the terminal was not taken back after the stop; wrote %q", after)
	}
	if push < enter {
		t.Errorf("push at %d precedes the alt-screen switch at %d", push, enter)
	}
}

// Pixel mouse is turned on by the startup probe, not with the other modes, so
// a retake has to turn it back on itself or clicks drop to cell resolution for
// the rest of the session.
func TestPixelMouseComesBackAfterASuspend(t *testing.T) {
	b, out := suspendableBackend(t)
	b.pixelMouse = true
	var atStop string
	b.suspend(func() { atStop = out.String() })
	if !strings.Contains(strings.TrimPrefix(out.String(), atStop), "\033[?1016h") {
		t.Error("pixel mouse was on before the suspend and is off after it")
	}
}

// What the backend believed the screen showed is forgotten, because the
// alternate screen comes back empty: every row is written again, the caret's
// shape and colour are sent again, and pictures are placed again.
func TestARetakeForgetsWhatTheScreenShowed(t *testing.T) {
	b, _ := suspendableBackend(t)
	b.cursorStyleSent = 2
	b.shownImages = []placedImage{{col: 1, row: 1}}
	b.suspend(func() {})

	if !b.needsLineClear {
		t.Error("the next frame is not a full repaint; the retaken screen stays blank")
	}
	if b.cursorShown {
		t.Error("the caret is believed shown, but the retake hid it")
	}
	if b.cursorStyleSent != -1 {
		t.Errorf("caret shape believed sent as %d; the shell may have changed it", b.cursorStyleSent)
	}
	if b.shownImages != nil {
		t.Error("pictures believed on screen; they went with the old alternate screen")
	}
}

// A frame is asked for once the terminal is back, or nothing would be drawn
// until the next input arrived.
func TestARetakeAsksForAFrame(t *testing.T) {
	b, _ := suspendableBackend(t)
	b.suspend(func() {})
	select {
	case ev := <-b.eventQueue:
		r, ok := ev.(core.ResizeEvent)
		if !ok {
			t.Fatalf("queued %T, want a ResizeEvent", ev)
		}
		if r.Cols != 80 || r.Rows != 24 {
			t.Errorf("repaint at %dx%d, want 80x24", r.Cols, r.Rows)
		}
	default:
		t.Fatal("no frame asked for after the retake")
	}
}

// While the shell has the terminal an emergency exit has nothing to restore,
// and writing the restore escapes then would land on the shell's screen. The
// backend leaves the registry for the stop and rejoins it after.
func TestASuspendedBackendIsNotRestoredByAnEmergencyExit(t *testing.T) {
	b, _ := suspendableBackend(t)
	registerLive(b)
	t.Cleanup(func() { unregisterLive(b) })
	before := LiveCount()

	var during int
	b.suspend(func() { during = LiveCount() })

	if during != before-1 {
		t.Errorf("live count during the stop = %d, want %d", during, before-1)
	}
	if LiveCount() != before {
		t.Errorf("live count after the retake = %d, want %d", LiveCount(), before)
	}
}

// The key reader is paused for the stop and resumed after it, and a key held
// across the suspend comes up: its real key-up is typed into the shell.
func TestTheKeyReaderIsPausedForTheStop(t *testing.T) {
	b, _ := suspendableBackend(t)
	pr, pw := io.Pipe()
	defer pw.Close()
	manage := false
	kb := keyboard.New(keyboard.Options{InputReader: pr, ManageTerminal: &manage})
	kb.OnKey = b.handleKey
	if err := kb.Start(); err != nil {
		t.Fatal(err)
	}
	defer kb.Stop()
	b.keyboard = kb

	pw.Write([]byte("\x1b[97;5u")) // ^A down
	waitFor(t, func() bool { return len(b.eventQueue) > 0 })
	drain(b)

	var pausedAtStop bool
	b.suspend(func() { pausedAtStop = kb.IsPaused() })

	if !pausedAtStop {
		t.Error("the key reader still had the terminal in raw mode when the process stopped")
	}
	if kb.IsPaused() {
		t.Error("the key reader is still paused after the retake")
	}
	var released bool
	for _, ev := range drain(b) {
		if _, ok := ev.(core.KeyReleaseEvent); ok {
			released = true
		}
	}
	if !released {
		t.Error("a key held across the suspend never came up")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
