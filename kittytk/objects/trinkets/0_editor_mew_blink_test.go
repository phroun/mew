//go:build mew

package trinkets

import (
	"testing"
)

// Mouse MOTION reports must not wake the caret: an application tracking
// motion gets one per pointer step, and waking on those pins the caret lit —
// the same failure feeding on every render produced.
func TestMouseMotionReportsDoNotWakeTheCaret(t *testing.T) {
	cases := []struct {
		name   string
		bytes  string
		motion bool
	}{
		{"SGR press", "\x1b[<0;10;5M", false},
		{"SGR release", "\x1b[<0;10;5m", false},
		{"SGR drag (motion flag)", "\x1b[<32;10;5M", true},
		{"SGR move, no button", "\x1b[<35;10;5M", true},
		{"SGR wheel up", "\x1b[<64;10;5M", false},
		{"X10 press", "\x1b[M\x20\x30\x30", false},
		{"X10 drag", "\x1b[M\x40\x30\x30", true},
		{"a typed letter", "a", false},
		{"a tinput word", "hello", false},
		{"an arrow key", "\x1b[A", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		if got := isMouseMotionReport([]byte(c.bytes)); got != c.motion {
			t.Errorf("%s (%q): motion=%v, want %v", c.name, c.bytes, got, c.motion)
		}
	}
}

// The wrapper wakes on write, and preserves an exit-status readout when the
// inner session has one — claiming it for a session that does not would tell
// mew a lie about what this host can answer.
type fakeSession struct{ written []byte }

func (f *fakeSession) Read(p []byte) (int, error) { return 0, nil }
func (f *fakeSession) Write(p []byte) (int, error) {
	f.written = append(f.written, p...)
	return len(p), nil
}
func (f *fakeSession) Resize(cols, rows int) error { return nil }
func (f *fakeSession) Close() error                { return nil }

type fakeSessionWithExit struct{ fakeSession }

func (f *fakeSessionWithExit) ExitStatus() (int, bool) { return 42, true }

func TestWakeOnWritePreservesTheSession(t *testing.T) {
	e := NewEditor()
	defer e.Close()

	plain := &fakeSession{}
	w := e.wakeOnWrite(plain)
	if _, ok := w.(interface{ ExitStatus() (int, bool) }); ok {
		t.Error("a session with no exit status must not gain one through the wrapper")
	}
	if _, err := w.Write([]byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if string(plain.written) != "hi" {
		t.Errorf("the wrapper must pass writes through; inner saw %q", plain.written)
	}

	withExit := &fakeSessionWithExit{}
	we := e.wakeOnWrite(withExit)
	ex, ok := we.(interface{ ExitStatus() (int, bool) })
	if !ok {
		t.Fatal("a session WITH an exit status must keep it through the wrapper")
	}
	if code, exited := ex.ExitStatus(); code != 42 || !exited {
		t.Errorf("exit status = (%d,%v), want (42,true)", code, exited)
	}

	if e.wakeOnWrite(nil) != nil {
		t.Error("nil session should wrap to nil")
	}
}

// Typing snaps the view back to the live edge; mouse input never does.
// Someone reading scrollback who starts typing means to be at the prompt —
// but a wheel tick is how they got up there, and a click is how they select
// text there, so neither may yank the view away.
func TestOnlyTypingSnapsBackToTheBottom(t *testing.T) {
	cases := []struct {
		name  string
		bytes string
		snap  bool
	}{
		{"a letter", "a", true},
		{"a tinput word", "hello", true},
		{"a newline", "\r", true},
		{"an arrow key", "\x1b[A", true},
		{"SGR press", "\x1b[<0;10;5M", false},
		{"SGR release", "\x1b[<0;10;5m", false},
		{"SGR wheel up", "\x1b[<64;10;5M", false},
		{"SGR drag", "\x1b[<32;10;5M", false},
		{"X10 press", "\x1b[M\x20\x30\x30", false},
	}
	for _, c := range cases {
		mouse := isMouseReport([]byte(c.bytes))
		if snaps := !mouse; snaps != c.snap {
			t.Errorf("%s (%q): snaps=%v, want %v", c.name, c.bytes, snaps, c.snap)
		}
	}
}
