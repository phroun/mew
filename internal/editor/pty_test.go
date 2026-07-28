package editor

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

// stubPTY is a host-provided session: the two halves of a pipe, so a test can
// feed output in and read what mew wrote back.
type stubPTY struct {
	mu      sync.Mutex
	out     chan []byte
	written strings.Builder
	cols    int
	rows    int
	closed  bool
}

func newStubPTY() *stubPTY { return &stubPTY{out: make(chan []byte, 16)} }

func (s *stubPTY) Read(p []byte) (int, error) {
	chunk, ok := <-s.out
	if !ok {
		return 0, io.EOF
	}
	return copy(p, chunk), nil
}
func (s *stubPTY) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.written.Write(p)
	return len(p), nil
}
func (s *stubPTY) Resize(c, r int) error { s.cols, s.rows = c, r; return nil }
func (s *stubPTY) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.out)
	}
	return nil
}
func (s *stubPTY) sent() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.written.String()
}

// The request carries a canonical URL for the buffer's directory and the
// command NAME, and nothing that could name a local binary.
func TestExecRequestShape(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	var got PTYRequest
	e.Config.PTYProvider = func(r PTYRequest) (PTYSession, error) {
		got = r
		return newStubPTY(), nil
	}
	w.Buffer.SetFilename("/home/user/project/main.go")

	if !e.execRequest("bash") {
		t.Fatal("exec should have succeeded")
	}
	if got.Command != "bash" {
		t.Errorf("Command = %q, want bash", got.Command)
	}
	if !strings.HasPrefix(got.CWD, "file://") || !strings.HasSuffix(got.CWD, "/home/user/project") {
		t.Errorf("CWD = %q, want the buffer's directory as a file:// URL", got.CWD)
	}
	if got.Cols <= 0 || got.Rows <= 0 {
		t.Errorf("Cols/Rows = %d/%d, want a usable size", got.Cols, got.Rows)
	}
}

// An unnamed buffer sends a BLANK cwd. That is a real answer, not a failure:
// the host decides what "nowhere in particular" means, which for a root mew is
// the user's home directory. mew must not guess on its behalf.
func TestExecUnnamedBufferSendsBlankCWD(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	var got PTYRequest
	e.Config.PTYProvider = func(r PTYRequest) (PTYSession, error) {
		got = r
		return newStubPTY(), nil
	}
	if !e.execRequest("zsh") {
		t.Fatal("exec should have succeeded")
	}
	if got.CWD != "" {
		t.Errorf("CWD = %q, want empty for an unnamed buffer", got.CWD)
	}
}

// Refusal is an ordinary outcome: reported, and the buffer is untouched.
func TestExecRefusalIsGraceful(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) {
		return nil, errors.New("not permitted here")
	}
	before := docContent(w)
	if e.execRequest("bash") {
		t.Error("a refused request must report failure")
	}
	if !hasWarning(e, "not permitted here") {
		t.Error("the host's reason should reach the user")
	}
	if docContent(w) != before {
		t.Error("a refused request must not touch the buffer")
	}
	if e.ptySessionFor(w.Buffer) != nil {
		t.Error("a refused request must leave no session bound")
	}
}

// With no provider at all, exec says so rather than failing silently.
func TestExecWithoutProvider(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	if e.execRequest("bash") {
		t.Error("no provider: exec must report failure")
	}
	if !hasWarning(e, "does not grant sessions") {
		t.Error("expected the no-provider warning")
	}
}

// pty_send writes to the bound session, and reports FALSE with no session so a
// chain falls through to ordinary editing.
func TestPTYSendRoutesToSessionElseFallsThrough(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	if e.ptySend("ls\n") {
		t.Error("no session: pty_send must fall through")
	}
	stub := newStubPTY()
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash") {
		t.Fatal("exec failed")
	}
	if !e.ptySend("ls\n") {
		t.Error("with a session, pty_send should succeed")
	}
	if stub.sent() != "ls\n" {
		t.Errorf("session received %q, want %q", stub.sent(), "ls\n")
	}
	_ = w
}

// One session per buffer: a second exec on the same buffer is refused rather
// than orphaning the first child.
func TestExecRefusesSecondSessionOnSameBuffer(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	if !e.execRequest("bash") {
		t.Fatal("first exec failed")
	}
	if e.execRequest("zsh") {
		t.Error("a second session on one buffer must be refused")
	}
	if !hasWarning(e, "already has a session") {
		t.Error("expected the already-running warning")
	}
}

// The stand-in output path drops terminal control sequences and keeps text.
// Provisional with ptyOutput, but pinned so a rewrite is a deliberate act.
func TestStripTerminalControls(t *testing.T) {
	esc := string(rune(27))
	for _, tc := range []struct{ in, want string }{
		{"plain", "plain"},
		{esc + "[0mred" + esc + "[m", "red"},
		{esc + "[1;32mgreen", "green"},
		{esc + "]0;a title" + string(rune(7)) + "after", "after"},
		{"line1\nline2", "line1\nline2"},
		{"tab\there", "tab\there"},
		{"carriage\rreturn", "carriagereturn"},
	} {
		if got := stripTerminalControls([]byte(tc.in)); got != tc.want {
			t.Errorf("stripTerminalControls(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
